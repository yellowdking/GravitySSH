package syncclient

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestRelayClientTLS13SPKIPinningAndRoundTrip(t *testing.T) {
	const gateKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var mu sync.Mutex
	slots := make(map[string]string)
	remoteAddrs := make(map[string]struct{})

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != ClientHeaderValue {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ts := r.Header.Get("X-GSSH-TS")
		if r.Header.Get("X-GSSH-Sig") != SignRequest(gateKey, r.Method, r.URL.Path, ts, body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		remoteAddrs[r.RemoteAddr] = struct{}{}
		switch r.URL.Path {
		case "/v1/pair/init":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
			}
			_ = json.Unmarshal(body, &req)
			slots[req.ChannelID] = req.SenderToken
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "expiresAt": time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)})
		case "/v1/pair/poll":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
			}
			_ = json.Unmarshal(body, &req)
			tok, ok := slots[req.ChannelID]
			if !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(req.SenderToken)) != 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "waiting"})
		case "/v1/pair/cancel":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
			}
			_ = json.Unmarshal(body, &req)
			if tok, ok := slots[req.ChannelID]; !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(req.SenderToken)) != 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(slots, req.ChannelID)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	spkiSum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	spkiHex := hex.EncodeToString(spkiSum[:])

	badClient, err := NewClientWithMode(srv.URL, gateKey, strings.Repeat("0", 64), RelayTLSModePinned)
	if err != nil {
		t.Fatalf("NewClientWithMode failed: %v", err)
	}
	if _, err := badClient.PairInit(context.Background(), strings.Repeat("a", 64), strings.Repeat("b", 64), 300); err == nil {
		t.Fatal("expected SPKI pin mismatch error")
	}

	client, err := NewClientWithMode(srv.URL, gateKey, spkiHex, RelayTLSModePinned)
	if err != nil {
		t.Fatalf("NewClientWithMode failed: %v", err)
	}
	code, _ := GenerateCode()
	canonical, _ := CanonicalizeCode(code)
	channelID := DeriveChannelID(canonical)
	senderToken, _ := GenerateHexToken32()

	if _, err := client.PairInit(context.Background(), channelID, senderToken, 300); err != nil {
		t.Fatalf("PairInit failed: %v", err)
	}
	poll, err := client.PairPoll(context.Background(), channelID, senderToken)
	if err != nil || poll.State != "waiting" || poll.NotFound {
		t.Fatalf("PairPoll failed: %+v err=%v", poll, err)
	}
	if err := client.PairCancel(context.Background(), channelID, senderToken); err != nil {
		t.Fatalf("PairCancel failed: %v", err)
	}
	pollAfter, err := client.PairPoll(context.Background(), channelID, senderToken)
	if err != nil || !pollAfter.NotFound || pollAfter.State != "gone" {
		t.Fatalf("expected gone after cancel, got %+v err=%v", pollAfter, err)
	}
	mu.Lock()
	connCount := len(remoteAddrs)
	mu.Unlock()
	if connCount != 1 {
		t.Fatalf("expected HTTP keep-alive to reuse 1 TLS connection across 4 requests, got %d", connCount)
	}
}

func TestRelayClientPairV2RoundTripAndRejection(t *testing.T) {
	const gateKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	type mockSlot struct {
		state       string
		senderToken string
		requestID   string
		reqPayload  EncryptedPayload
		appPayload  EncryptedPayload
		expiresAt   time.Time
	}
	var mu sync.Mutex
	slots := make(map[string]*mockSlot)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != ClientHeaderValue ||
			r.Header.Get("X-GSSH-Sig") != SignRequest(gateKey, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/pair/init":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
				TTLSeconds  int    `json:"ttlSeconds"`
			}
			_ = json.Unmarshal(body, &req)
			if _, exists := slots[req.ChannelID]; exists {
				w.WriteHeader(http.StatusConflict)
				return
			}
			exp := time.Now().UTC().Add(time.Duration(req.TTLSeconds) * time.Second)
			slots[req.ChannelID] = &mockSlot{state: "waiting", senderToken: req.SenderToken, expiresAt: exp}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "expiresAt": exp.Format(time.RFC3339)})
		case "/v1/pair/request":
			var req struct {
				ChannelID  string `json:"channelId"`
				RequestID  string `json:"requestId"`
				Salt       string `json:"salt"`
				Nonce      string `json:"nonce"`
				Ciphertext string `json:"ciphertext"`
			}
			_ = json.Unmarshal(body, &req)
			s, ok := slots[req.ChannelID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if s.state != "waiting" {
				w.WriteHeader(http.StatusConflict)
				return
			}
			s.state = "requested"
			s.requestID = req.RequestID
			s.reqPayload = EncryptedPayload{Salt: req.Salt, Nonce: req.Nonce, Ciphertext: req.Ciphertext}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "expiresAt": s.expiresAt.Format(time.RFC3339)})
		case "/v1/pair/poll":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
			}
			_ = json.Unmarshal(body, &req)
			s, ok := slots[req.ChannelID]
			if !ok || subtle.ConstantTimeCompare([]byte(s.senderToken), []byte(req.SenderToken)) != 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			out := map[string]any{"state": s.state}
			if s.requestID != "" {
				out["requestId"] = s.requestID
			}
			if s.state == "requested" {
				out["salt"] = s.reqPayload.Salt
				out["nonce"] = s.reqPayload.Nonce
				out["ciphertext"] = s.reqPayload.Ciphertext
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/v1/pair/approve":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
				RequestID   string `json:"requestId"`
				Salt        string `json:"salt"`
				Nonce       string `json:"nonce"`
				Ciphertext  string `json:"ciphertext"`
			}
			_ = json.Unmarshal(body, &req)
			s, ok := slots[req.ChannelID]
			if !ok || subtle.ConstantTimeCompare([]byte(s.senderToken), []byte(req.SenderToken)) != 1 || s.requestID != req.RequestID {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if s.state != "requested" {
				w.WriteHeader(http.StatusConflict)
				return
			}
			s.state = "approved"
			s.appPayload = EncryptedPayload{Salt: req.Salt, Nonce: req.Nonce, Ciphertext: req.Ciphertext}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/pair/reject":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
				RequestID   string `json:"requestId"`
			}
			_ = json.Unmarshal(body, &req)
			s, ok := slots[req.ChannelID]
			if !ok || subtle.ConstantTimeCompare([]byte(s.senderToken), []byte(req.SenderToken)) != 1 || s.requestID != req.RequestID {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			s.state = "rejected"
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/pair/status":
			var req struct {
				ChannelID string `json:"channelId"`
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(body, &req)
			s, ok := slots[req.ChannelID]
			if !ok || s.requestID != req.RequestID {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch s.state {
			case "waiting", "requested":
				_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending"})
			case "rejected":
				delete(slots, req.ChannelID)
				_ = json.NewEncoder(w).Encode(map[string]any{"state": "rejected"})
			case "approved":
				p := s.appPayload
				delete(slots, req.ChannelID)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"state":      "approved",
					"salt":       p.Salt,
					"nonce":      p.Nonce,
					"ciphertext": p.Ciphertext,
				})
			}
		case "/v1/pair/cancel":
			var req struct {
				ChannelID   string `json:"channelId"`
				SenderToken string `json:"senderToken"`
			}
			_ = json.Unmarshal(body, &req)
			s, ok := slots[req.ChannelID]
			if !ok || subtle.ConstantTimeCompare([]byte(s.senderToken), []byte(req.SenderToken)) != 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(slots, req.ChannelID)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	spkiSum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	spkiHex := hex.EncodeToString(spkiSum[:])
	client, err := NewClientWithMode(srv.URL, gateKey, spkiHex, RelayTLSModePinned)
	if err != nil {
		t.Fatalf("NewClientWithMode failed: %v", err)
	}
	ctx := context.Background()

	// Flow 1: Happy path mutual verification roundtrip
	sendCode, _ := GenerateCode()
	recvCode, _ := GenerateCode()
	canonicalSend, _ := CanonicalizeCode(sendCode)
	channelID := DeriveChannelID(canonicalSend)
	senderToken, _ := GenerateHexToken32()
	requestID, _ := GenerateHexToken32()

	if _, err := client.PairInit(ctx, channelID, senderToken, 300); err != nil {
		t.Fatalf("PairInit failed: %v", err)
	}
	poll1, err := client.PairPoll(ctx, channelID, senderToken)
	if err != nil || poll1.State != "waiting" || poll1.NotFound {
		t.Fatalf("expected waiting poll, got %+v err=%v", poll1, err)
	}

	reqInfo := PairRequestPayload{
		ReceiverCode:     recvCode,
		ReceiverDevice:   "Pixel-9",
		ReceiverPlatform: "android",
		RequestedAt:      "2026-11-27T03:00:00Z",
	}
	encReq, err := EncryptPairRequest(sendCode, reqInfo)
	if err != nil {
		t.Fatalf("EncryptPairRequest failed: %v", err)
	}
	if _, err := client.PairRequest(ctx, channelID, requestID, encReq); err != nil {
		t.Fatalf("PairRequest failed: %v", err)
	}
	// Second concurrent PairRequest must fail with conflict
	if _, err := client.PairRequest(ctx, channelID, requestID, encReq); err == nil {
		t.Fatal("expected second PairRequest to fail while slot is requested")
	}

	poll2, err := client.PairPoll(ctx, channelID, senderToken)
	if err != nil || poll2.State != "requested" || poll2.RequestID != requestID {
		t.Fatalf("expected requested poll, got %+v err=%v", poll2, err)
	}
	decReq, err := DecryptPairRequest(sendCode, poll2.EncryptedPayload)
	if err != nil || decReq != reqInfo {
		t.Fatalf("DecryptPairRequest mismatch: %+v err=%v", decReq, err)
	}

	// Receiver polls status while pending
	st1, err := client.PairStatus(ctx, channelID, requestID)
	if err != nil || st1.State != "pending" {
		t.Fatalf("expected pending status, got %+v err=%v", st1, err)
	}

	// Sender approves and uploads dual-encrypted bundle
	wantBundle := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     "2026-11-27T03:00:01Z",
		SourcePlatform: "windows",
		SourceDevice:   "WIN-SYNC-V2",
		Servers:        []models.ServerConfig{{ID: "s1", Name: "N1", Host: "10.0.0.1", Port: 22, Username: "root"}},
	}
	dualPayload, err := EncryptBundleDual(sendCode, decReq.ReceiverCode, requestID, wantBundle)
	if err != nil {
		t.Fatalf("EncryptBundleDual failed: %v", err)
	}
	if err := client.PairApprove(ctx, channelID, senderToken, requestID, dualPayload); err != nil {
		t.Fatalf("PairApprove failed: %v", err)
	}

	// Receiver fetches & burns approved payload
	st2, err := client.PairStatus(ctx, channelID, requestID)
	if err != nil || st2.State != "approved" || st2.EncryptedPayload.ChannelID != channelID {
		t.Fatalf("expected approved status, got %+v err=%v", st2, err)
	}
	gotBundle, err := DecryptBundleDual(sendCode, recvCode, requestID, st2.EncryptedPayload)
	if err != nil || gotBundle.SourceDevice != "WIN-SYNC-V2" || len(gotBundle.Servers) != 1 {
		t.Fatalf("DecryptBundleDual mismatch: %+v err=%v", gotBundle, err)
	}

	// Burn-after-read: receiver second PairStatus gets ErrPairNotFound; sender PairPoll gets State=gone, NotFound=true cleanly!
	if _, err := client.PairStatus(ctx, channelID, requestID); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("expected ErrPairNotFound on second PairStatus, got %v", err)
	}
	poll3, err := client.PairPoll(ctx, channelID, senderToken)
	if err != nil || !poll3.NotFound || poll3.State != "gone" {
		t.Fatalf("expected clean gone/NotFound after burn-after-read, got %+v err=%v", poll3, err)
	}

	// Flow 2: Rejection flow
	sendCode2, _ := GenerateCode()
	recvCode2, _ := GenerateCode()
	canSend2, _ := CanonicalizeCode(sendCode2)
	chan2 := DeriveChannelID(canSend2)
	tok2, _ := GenerateHexToken32()
	reqID2, _ := GenerateHexToken32()
	if _, err := client.PairInit(ctx, chan2, tok2, 300); err != nil {
		t.Fatalf("PairInit 2 failed: %v", err)
	}
	encReq2, _ := EncryptPairRequest(sendCode2, PairRequestPayload{ReceiverCode: recvCode2, ReceiverDevice: "Bad", ReceiverPlatform: "android", RequestedAt: "2026-11-27T03:00:00Z"})
	if _, err := client.PairRequest(ctx, chan2, reqID2, encReq2); err != nil {
		t.Fatalf("PairRequest 2 failed: %v", err)
	}
	if err := client.PairReject(ctx, chan2, tok2, reqID2); err != nil {
		t.Fatalf("PairReject failed: %v", err)
	}
	stRej, err := client.PairStatus(ctx, chan2, reqID2)
	if err != nil || stRej.State != "rejected" {
		t.Fatalf("expected rejected status, got %+v err=%v", stRej, err)
	}

	// Flow 3: Cancel flow
	sendCode3, _ := GenerateCode()
	canSend3, _ := CanonicalizeCode(sendCode3)
	chan3 := DeriveChannelID(canSend3)
	tok3, _ := GenerateHexToken32()
	if _, err := client.PairInit(ctx, chan3, tok3, 300); err != nil {
		t.Fatalf("PairInit 3 failed: %v", err)
	}
	if err := client.PairCancel(ctx, chan3, tok3); err != nil {
		t.Fatalf("PairCancel failed: %v", err)
	}
	pollCancel, err := client.PairPoll(ctx, chan3, tok3)
	if err != nil || !pollCancel.NotFound || pollCancel.State != "gone" {
		t.Fatalf("expected gone after PairCancel, got %+v err=%v", pollCancel, err)
	}
}

func TestLiveRelayRoundTrip(t *testing.T) {
	if os.Getenv("GSSH_LIVE_RELAY_TEST") != "1" {
		t.Skip("set GSSH_LIVE_RELAY_TEST=1 to run live VPS relay integration test")
	}
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient default failed: %v", err)
	}
	ctx := context.Background()

	sendCode, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode send failed: %v", err)
	}
	recvCode, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode recv failed: %v", err)
	}
	canonicalSend, err := CanonicalizeCode(sendCode)
	if err != nil {
		t.Fatalf("CanonicalizeCode failed: %v", err)
	}
	channelID := DeriveChannelID(canonicalSend)
	senderToken, err := GenerateHexToken32()
	if err != nil {
		t.Fatalf("GenerateHexToken32 senderToken failed: %v", err)
	}
	requestID, err := GenerateHexToken32()
	if err != nil {
		t.Fatalf("GenerateHexToken32 requestID failed: %v", err)
	}

	exp, err := client.PairInit(ctx, channelID, senderToken, 60)
	if err != nil {
		t.Fatalf("Live PairInit failed: %v", err)
	}
	t.Logf("Live PairInit OK: sendCode=%s expiresAt=%s", sendCode, exp.Format(time.RFC3339))

	reqInfo := PairRequestPayload{
		ReceiverCode:     recvCode,
		ReceiverDevice:   "WIN-LIVE-RECV",
		ReceiverPlatform: "windows",
		RequestedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	encReq, err := EncryptPairRequest(sendCode, reqInfo)
	if err != nil {
		t.Fatalf("EncryptPairRequest failed: %v", err)
	}
	if _, err := client.PairRequest(ctx, channelID, requestID, encReq); err != nil {
		t.Fatalf("Live PairRequest failed: %v", err)
	}

	poll, err := client.PairPoll(ctx, channelID, senderToken)
	if err != nil || poll.State != "requested" || poll.RequestID != requestID {
		t.Fatalf("Live PairPoll expected requested, got %+v err=%v", poll, err)
	}
	decReq, err := DecryptPairRequest(sendCode, poll.EncryptedPayload)
	if err != nil || decReq.ReceiverCode != recvCode {
		t.Fatalf("Live DecryptPairRequest mismatch: %+v err=%v", decReq, err)
	}

	want := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		SourcePlatform: "windows",
		SourceDevice:   "WIN-LIVE-TEST",
		Servers:        []models.ServerConfig{{ID: "live-1", Name: "LiveNode", Host: "192.0.2.99", Port: 22, Username: "root"}},
	}
	dualPayload, err := EncryptBundleDual(sendCode, decReq.ReceiverCode, requestID, want)
	if err != nil {
		t.Fatalf("EncryptBundleDual failed: %v", err)
	}
	if err := client.PairApprove(ctx, channelID, senderToken, requestID, dualPayload); err != nil {
		t.Fatalf("Live PairApprove failed: %v", err)
	}

	st, err := client.PairStatus(ctx, channelID, requestID)
	if err != nil || st.State != "approved" {
		t.Fatalf("Live PairStatus expected approved, got %+v err=%v", st, err)
	}
	dec, err := DecryptBundleDual(sendCode, recvCode, requestID, st.EncryptedPayload)
	if err != nil || dec.SourceDevice != "WIN-LIVE-TEST" || len(dec.Servers) != 1 {
		t.Fatalf("Live DecryptBundleDual mismatch: %+v err=%v", dec, err)
	}

	// Burn-after-read: second PairStatus must return ErrPairNotFound and PairPoll must report gone/NotFound.
	if _, err := client.PairStatus(ctx, channelID, requestID); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("expected ErrPairNotFound on second live PairStatus, got %v", err)
	}
	pollGone, err := client.PairPoll(ctx, channelID, senderToken)
	if err != nil || !pollGone.NotFound || pollGone.State != "gone" {
		t.Fatalf("expected gone/NotFound after live burn-after-read, got %+v err=%v", pollGone, err)
	}
}
