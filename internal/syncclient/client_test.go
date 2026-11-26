package syncclient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
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
	slots := make(map[string]EncryptedPayload)

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
		switch r.URL.Path {
		case "/v1/drop":
			var req EncryptedPayload
			_ = json.Unmarshal(body, &req)
			slots[req.ChannelID] = req
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "expiresAt": time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)})
		case "/v1/pickup":
			var req struct{ ChannelID string `json:"channelId"` }
			_ = json.Unmarshal(body, &req)
			p, ok := slots[req.ChannelID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(slots, req.ChannelID)
			_ = json.NewEncoder(w).Encode(map[string]string{"salt": p.Salt, "nonce": p.Nonce, "ciphertext": p.Ciphertext})
		case "/v1/cancel":
			var req struct{ ChannelID string `json:"channelId"` }
			_ = json.Unmarshal(body, &req)
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

	badClient, err := NewCustomClient(srv.URL, gateKey, strings.Repeat("0", 64), true)
	if err != nil {
		t.Fatalf("NewCustomClient failed: %v", err)
	}
	if _, err := badClient.Pickup(context.Background(), strings.Repeat("a", 64)); err == nil {
		t.Fatal("expected SPKI pin mismatch error")
	}

	client, err := NewCustomClient(srv.URL, gateKey, spkiHex, true)
	if err != nil {
		t.Fatalf("NewCustomClient failed: %v", err)
	}
	code, _ := GenerateCode()
	payload, err := EncryptBundle(code, models.SyncBundle{SchemaVersion: 1, SourcePlatform: "windows", SourceDevice: "TEST"})
	if err != nil {
		t.Fatalf("EncryptBundle failed: %v", err)
	}
	if _, err := client.Drop(context.Background(), payload, 300); err != nil {
		t.Fatalf("Drop failed: %v", err)
	}
	got, err := client.Pickup(context.Background(), payload.ChannelID)
	if err != nil {
		t.Fatalf("Pickup failed: %v", err)
	}
	dec, err := DecryptBundle(code, got)
	if err != nil || dec.SourceDevice != "TEST" {
		t.Fatalf("DecryptBundle failed: %+v err=%v", dec, err)
	}
	if _, err := client.Pickup(context.Background(), payload.ChannelID); err == nil {
		t.Fatal("expected second pickup to fail (burn-after-read)")
	}
	if err := client.Cancel(context.Background(), payload.ChannelID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
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
	code, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode failed: %v", err)
	}
	want := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		SourcePlatform: "windows",
		SourceDevice:   "WIN-LIVE-TEST",
		Servers:        []models.ServerConfig{{ID: "live-1", Name: "LiveNode", Host: "192.0.2.99", Port: 22, Username: "root"}},
	}
	payload, err := EncryptBundle(code, want)
	if err != nil {
		t.Fatalf("EncryptBundle failed: %v", err)
	}
	exp, err := client.Drop(context.Background(), payload, 60)
	if err != nil {
		t.Fatalf("Live Drop failed: %v", err)
	}
	t.Logf("Live Drop OK: code=%s expiresAt=%s", code, exp.Format(time.RFC3339))
	gotPayload, err := client.Pickup(context.Background(), payload.ChannelID)
	if err != nil {
		t.Fatalf("Live Pickup failed: %v", err)
	}
	dec, err := DecryptBundle(code, gotPayload)
	if err != nil || dec.SourceDevice != "WIN-LIVE-TEST" || len(dec.Servers) != 1 {
		t.Fatalf("Live DecryptBundle mismatch: %+v err=%v", dec, err)
	}
	// Burn-after-read: second pickup must return not found
	if _, err := client.Pickup(context.Background(), payload.ChannelID); err == nil {
		t.Fatal("expected second live pickup to fail (burn-after-read)")
	}
}
