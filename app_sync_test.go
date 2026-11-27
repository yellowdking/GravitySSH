package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"github.com/yellowdking/GravitySSH/internal/syncclient"
)

func TestPollSyncSendCachesDecryptedPairRequest(t *testing.T) {
	app, _ := newTestAppWithVault(t)

	const sendCode = "K7M9-P4X2"
	canonicalSend, _ := syncclient.CanonicalizeCode(sendCode)
	channelID := syncclient.DeriveChannelID(canonicalSend)
	senderToken := strings.Repeat("b", 64)

	reqInfo1 := syncclient.PairRequestPayload{
		ReceiverCode:     "M8N2-Q5R7",
		ReceiverDevice:   "Pixel-9",
		ReceiverPlatform: "android",
		RequestedAt:      "2026-11-27T12:00:00Z",
	}
	enc1, err := syncclient.EncryptPairRequest(sendCode, reqInfo1)
	if err != nil {
		t.Fatalf("EncryptPairRequest 1 failed: %v", err)
	}
	reqInfo2 := syncclient.PairRequestPayload{
		ReceiverCode:     "P3Q4-R5S6",
		ReceiverDevice:   "MacBook-Pro",
		ReceiverPlatform: "macos",
		RequestedAt:      "2026-11-27T12:01:00Z",
	}
	enc2, err := syncclient.EncryptPairRequest(sendCode, reqInfo2)
	if err != nil {
		t.Fatalf("EncryptPairRequest 2 failed: %v", err)
	}

	var (
		mu       sync.Mutex
		curReqID = "req-1"
		curEnc   = enc1
		pollHits atomic.Int32
	)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != syncclient.ClientHeaderValue ||
			r.Header.Get("X-GSSH-Sig") != syncclient.SignRequest(syncclient.AppGateKey, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/v1/pair/poll" {
			pollHits.Add(1)
			mu.Lock()
			id, enc := curReqID, curEnc
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":      "requested",
				"requestId":  id,
				"salt":       enc.Salt,
				"nonce":      enc.Nonce,
				"ciphertext": enc.Ciphertext,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	var decryptCalls atomic.Int32
	origDecrypt := decryptPairRequestFn
	decryptPairRequestFn = func(code string, payload syncclient.EncryptedPayload) (syncclient.PairRequestPayload, error) {
		decryptCalls.Add(1)
		return origDecrypt(code, payload)
	}
	t.Cleanup(func() { decryptPairRequestFn = origDecrypt })

	app.syncCache.putSend(&pendingSyncSendSession{
		sessionID:   "sess-cache",
		channelID:   channelID,
		senderToken: senderToken,
		sendCode:    canonicalSend,
		relayURL:    srv.URL,
		relayTarget: srv.URL,
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   time.Now().Add(5 * time.Minute),
	})

	// Poll 4 times for the same requestId -> PBKDF2 decryption MUST run only once.
	for i := 0; i < 4; i++ {
		res, err := app.PollSyncSend("sess-cache")
		if err != nil {
			t.Fatalf("PollSyncSend #%d failed: %v", i, err)
		}
		if res.Status != "requested" || res.RequestID != "req-1" || res.ReceiverCode != "M8N2-Q5R7" || res.ReceiverDevice != "Pixel-9" {
			t.Fatalf("unexpected poll #%d result: %+v", i, res)
		}
	}
	if got := decryptCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 PBKDF2 decrypt call across 4 polls for same requestId, got %d", got)
	}
	if got := pollHits.Load(); got != 4 {
		t.Fatalf("expected 4 relay polls, got %d", got)
	}

	// Switch to a different requestId -> cache misses once, then hits on repeat poll.
	mu.Lock()
	curReqID = "req-2"
	curEnc = enc2
	mu.Unlock()

	for i := 0; i < 2; i++ {
		res, err := app.PollSyncSend("sess-cache")
		if err != nil {
			t.Fatalf("PollSyncSend req-2 #%d failed: %v", i, err)
		}
		if res.Status != "requested" || res.RequestID != "req-2" || res.ReceiverCode != "P3Q4-R5S6" || res.ReceiverDevice != "MacBook-Pro" {
			t.Fatalf("unexpected req-2 poll #%d result: %+v", i, res)
		}
	}
	if got := decryptCalls.Load(); got != 2 {
		t.Fatalf("expected 2 total decrypt calls after switching to req-2 and polling twice, got %d", got)
	}
}

func TestApplySyncReceiveCloneZeroAndKeepOnError(t *testing.T) {
	app, store := newTestAppWithVault(t)
	if err := store.SetPassword("", "vault-pw"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}

	pemStr, parsed, err := sshkey.GenerateKeyPair("ed25519", "sync@test", "")
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	bundle := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		SourcePlatform: "android",
		SourceDevice:   "Pixel",
		Servers: []models.ServerConfig{
			{ID: "srv-sync-1", Name: "SyncNode", Host: "10.9.0.1", Port: 22, Username: "root", Password: "top-secret-password"},
		},
		SSHKeys: []models.SSHKeyRecord{
			{
				ID:            "key-sync-1",
				Name:          "SyncKey",
				KeyType:       parsed.KeyType,
				Fingerprint:   parsed.Fingerprint,
				PublicKey:     parsed.PublicKey,
				PrivateKeyPEM: pemStr,
			},
		},
	}
	previewID := app.syncCache.put(bundle)

	// Invalid mode fails ImportSyncBundle; ApplySyncReceive zeroes its local clone
	// while keeping the cached preview completely intact for retry.
	if _, err := app.ApplySyncReceive(previewID, "unsupported-mode"); err == nil {
		t.Fatal("expected invalid mode error")
	}
	peeked, ok := app.syncCache.peekClone(previewID)
	if !ok || len(peeked.Servers) != 1 || peeked.Servers[0].Password != "top-secret-password" ||
		len(peeked.SSHKeys) != 1 || peeked.SSHKeys[0].PrivateKeyPEM != pemStr {
		t.Fatalf("cached preview was corrupted or zeroed after error: ok=%v bundle=%+v", ok, peeked)
	}
	zeroSyncBundle(&peeked)

	// Valid apply succeeds and deletes preview from cache.
	sum, err := app.ApplySyncReceive(previewID, "merge")
	if err != nil {
		t.Fatalf("ApplySyncReceive retry failed: %v", err)
	}
	if sum.ServersAdded != 1 || sum.KeysAdded != 1 {
		t.Fatalf("unexpected import summary: %+v", sum)
	}
	if _, ok := app.syncCache.peekClone(previewID); ok {
		t.Fatal("expected preview to be removed from cache after successful apply")
	}
	srv, err := store.GetServerInternal("srv-sync-1")
	if err != nil || srv.Password != "top-secret-password" {
		t.Fatalf("imported server mismatch: %+v err=%v", srv, err)
	}
}

func TestSyncClientsLRUEvictionAndGateIsolation(t *testing.T) {
	app, _ := newTestAppWithVault(t)

	urls := make([]string, maxCachedSyncClients)
	for i := 0; i < maxCachedSyncClients; i++ {
		urls[i] = fmt.Sprintf("https://198.51.100.%d:54843", i+1)
		if _, _, err := app.syncClient(urls[i]); err != nil {
			t.Fatalf("syncClient(%s) failed: %v", urls[i], err)
		}
		// Ensure monotonic timestamps even on coarse Windows clocks.
		app.syncClientMu.Lock()
		for k, entry := range app.syncClients {
			if strings.HasPrefix(k, urls[i]+"|") {
				entry.lastUsed = time.Unix(1000+int64(i), 0)
			}
		}
		app.syncClientMu.Unlock()
	}

	// Touch urls[0] so it becomes most recently used; urls[1] is now the LRU victim.
	cli0Before, _, err := app.syncClient(urls[0])
	if err != nil {
		t.Fatalf("re-fetch urls[0] failed: %v", err)
	}
	app.syncClientMu.Lock()
	for k, entry := range app.syncClients {
		if strings.HasPrefix(k, urls[0]+"|") {
			entry.lastUsed = time.Unix(2000, 0)
		}
	}
	app.syncClientMu.Unlock()

	// Insert one more relay URL to trigger LRU eviction.
	overflowURL := "https://198.51.100.200:54843"
	if _, _, err := app.syncClient(overflowURL); err != nil {
		t.Fatalf("syncClient overflow failed: %v", err)
	}

	app.syncClientMu.Lock()
	if len(app.syncClients) != maxCachedSyncClients {
		app.syncClientMu.Unlock()
		t.Fatalf("expected syncClients len=%d, got %d", maxCachedSyncClients, len(app.syncClients))
	}
	var has0, has1, hasOverflow bool
	for k, entry := range app.syncClients {
		if strings.HasPrefix(k, urls[0]+"|") && entry.client == cli0Before {
			has0 = true
		}
		if strings.HasPrefix(k, urls[1]+"|") {
			has1 = true
		}
		if strings.HasPrefix(k, overflowURL+"|") {
			hasOverflow = true
		}
	}
	app.syncClientMu.Unlock()
	if !has0 || has1 || !hasOverflow {
		t.Fatalf("unexpected LRU eviction outcome: has0(MRU)=%v has1(LRU-victim)=%v hasOverflow=%v", has0, has1, hasOverflow)
	}

	// Same URL with two different #gate= keys must cache separately and never share clients.
	gateA := strings.Repeat("a", 64)
	gateB := strings.Repeat("b", 64)
	cliA, cleanA, err := app.syncClient("https://198.51.100.201:54843#gate=" + gateA)
	if err != nil || cleanA != "https://198.51.100.201:54843" {
		t.Fatalf("syncClient gateA failed: clean=%q err=%v", cleanA, err)
	}
	cliB, cleanB, err := app.syncClient("https://198.51.100.201:54843#gate=" + gateB)
	if err != nil || cleanB != "https://198.51.100.201:54843" {
		t.Fatalf("syncClient gateB failed: clean=%q err=%v", cleanB, err)
	}
	if cliA == cliB {
		t.Fatal("expected distinct cached clients for different #gate= keys on same URL")
	}
}

func TestSyncClockSkewGraceAcrossPolling(t *testing.T) {
	app, _ := newTestAppWithVault(t)

	var hits atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != syncclient.ClientHeaderValue ||
			r.Header.Get("X-GSSH-Sig") != syncclient.SignRequest(syncclient.AppGateKey, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		hits.Add(1)
		switch r.URL.Path {
		case "/v1/pair/poll":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "waiting"})
		case "/v1/pair/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	// Within 135s clock-skew grace: local expiresAt appears 115s in the past, yet polling must still hit relay.
	withinGrace := time.Now().Add(-115 * time.Second)
	app.syncCache.putSend(&pendingSyncSendSession{
		sessionID:   "send-skew",
		channelID:   strings.Repeat("1", 64),
		senderToken: strings.Repeat("2", 64),
		sendCode:    "ABCD2345",
		relayURL:    srv.URL,
		relayTarget: srv.URL,
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   withinGrace,
	})
	app.syncCache.putReceive(&pendingSyncReceiveSession{
		waitID:      "recv-skew",
		channelID:   strings.Repeat("1", 64),
		sendCode:    "ABCD2345",
		recvCode:    "WXYZ6789",
		requestID:   strings.Repeat("3", 64),
		relayURL:    srv.URL,
		relayTarget: srv.URL,
		expiresAt:   withinGrace,
	})

	sRes, err := app.PollSyncSend("send-skew")
	if err != nil || sRes.Status != "waiting" {
		t.Fatalf("expected waiting within skew grace, got %+v err=%v", sRes, err)
	}
	rRes, err := app.PollSyncReceive("recv-skew")
	if err != nil || rRes.Status != "pending" {
		t.Fatalf("expected pending within skew grace, got %+v err=%v", rRes, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected 2 relay hits within skew grace, got %d", hits.Load())
	}

	// Past 135s skew grace: both must expire locally without contacting relay.
	pastGrace := time.Now().Add(-(syncClockSkewGrace + 5*time.Second))
	app.syncCache.putSend(&pendingSyncSendSession{
		sessionID:   "send-expired",
		channelID:   strings.Repeat("4", 64),
		senderToken: strings.Repeat("5", 64),
		sendCode:    "EFGH2345",
		relayURL:    srv.URL,
		relayTarget: srv.URL,
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   pastGrace,
	})
	app.syncCache.putReceive(&pendingSyncReceiveSession{
		waitID:      "recv-expired",
		channelID:   strings.Repeat("4", 64),
		sendCode:    "EFGH2345",
		recvCode:    "JKLM6789",
		requestID:   strings.Repeat("6", 64),
		relayURL:    srv.URL,
		relayTarget: srv.URL,
		expiresAt:   pastGrace,
	})

	sExp, err := app.PollSyncSend("send-expired")
	if err != nil || sExp.Status != "expired" {
		t.Fatalf("expected expired past skew grace, got %+v err=%v", sExp, err)
	}
	rExp, err := app.PollSyncReceive("recv-expired")
	if err != nil || rExp.Status != "expired" {
		t.Fatalf("expected expired past skew grace, got %+v err=%v", rExp, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected no additional relay hits for sessions past skew grace, got %d", hits.Load())
	}
}

func TestLockVaultClearsSyncAndCancelsRelayAndImportKeyHelper(t *testing.T) {
	app, store := newTestAppWithVault(t)

	// LockVault must reject when no vault password is configured.
	if err := app.LockVault(); err == nil {
		t.Fatal("expected LockVault to fail when no password is set")
	}

	// ImportSSHKeyFromFile nil-ctx guard + importSSHKeyFromPath validation & import.
	if _, err := app.ImportSSHKeyFromFile("k", "", ""); err == nil {
		t.Fatal("expected ImportSSHKeyFromFile with nil ctx to fail cleanly")
	}
	tmpDir := t.TempDir()
	if _, err := app.importSSHKeyFromPath(store, tmpDir, "", "", ""); err == nil {
		t.Fatal("expected directory to be rejected")
	}
	badFile := filepath.Join(tmpDir, "bad.pem")
	if err := os.WriteFile(badFile, []byte("not-a-valid-pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.importSSHKeyFromPath(store, badFile, "", "", ""); err == nil {
		t.Fatal("expected invalid PEM to be rejected")
	}
	bigFile := filepath.Join(tmpDir, "huge.pem")
	if err := os.WriteFile(bigFile, []byte(strings.Repeat("X", maxSSHKeyFileSize+32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.importSSHKeyFromPath(store, bigFile, "", "", ""); err == nil {
		t.Fatal("expected oversized key file to be rejected")
	}

	pemStr, parsed, err := sshkey.GenerateKeyPair("ed25519", "file@import", "pass-123")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(tmpDir, "prod_bastion.pem")
	if err := os.WriteFile(keyPath, []byte(pemStr), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := app.importSSHKeyFromPath(store, keyPath, "", "pass-123", "from-file")
	if err != nil || meta == nil {
		t.Fatalf("importSSHKeyFromPath failed: meta=%+v err=%v", meta, err)
	}
	if meta.Name != "prod_bastion" || meta.Fingerprint != parsed.Fingerprint || !meta.HasPassphrase {
		t.Fatalf("unexpected imported key meta: %+v", meta)
	}

	// Set password, register active sync send (using custom #gate= & #spki=) + preview + receive, then LockVault.
	if err := store.SetPassword("", "lock-pass-123"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}

	const customGate = "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"
	cancelCh := make(chan string, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != syncclient.ClientHeaderValue ||
			r.Header.Get("X-GSSH-Sig") != syncclient.SignRequest(customGate, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/v1/pair/cancel" {
			var req struct {
				ChannelID string `json:"channelId"`
			}
			_ = json.Unmarshal(body, &req)
			cancelCh <- req.ChannelID
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	sum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	spkiPin := hex.EncodeToString(sum[:])
	relayWithFrag := srv.URL + "#spki=" + spkiPin + "&gate=" + customGate

	wantChan := strings.Repeat("c", 64)
	app.syncCache.putSend(&pendingSyncSendSession{
		sessionID:   "send-lock",
		channelID:   wantChan,
		senderToken: strings.Repeat("d", 64),
		sendCode:    "ABCD2345",
		relayURL:    srv.URL,
		relayTarget: relayWithFrag,
		bundle:      sampleTestBundle(),
		hasBundle:   true,
		expiresAt:   time.Now().Add(5 * time.Minute),
	})
	app.syncCache.putReceive(&pendingSyncReceiveSession{
		waitID:      "recv-lock",
		channelID:   wantChan,
		sendCode:    "ABCD2345",
		recvCode:    "WXYZ6789",
		requestID:   strings.Repeat("e", 64),
		relayURL:    srv.URL,
		relayTarget: relayWithFrag,
		expiresAt:   time.Now().Add(5 * time.Minute),
	})
	previewID := app.syncCache.put(sampleTestBundle())

	if err := app.LockVault(); err != nil {
		t.Fatalf("LockVault failed: %v", err)
	}
	if st, err := app.GetVaultLockState(); err != nil || !st.Locked || !st.HasPassword {
		t.Fatalf("expected locked vault state after LockVault, got %+v err=%v", st, err)
	}
	if len(store.ListSSHKeys()) != 0 {
		t.Fatal("expected in-memory SSH keys cleared on LockVault")
	}
	if _, ok := app.syncCache.getSend("send-lock"); ok {
		t.Fatal("expected pending send session cleared on LockVault")
	}
	if _, ok := app.syncCache.getReceive("recv-lock"); ok {
		t.Fatal("expected pending receive session cleared on LockVault")
	}
	if _, ok := app.syncCache.peekClone(previewID); ok {
		t.Fatal("expected pending preview cleared on LockVault")
	}
	select {
	case gotChan := <-cancelCh:
		if gotChan != wantChan {
			t.Fatalf("expected PairCancel for %s, got %s", wantChan, gotChan)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected LockVault to issue best-effort PairCancel to custom #spki+#gate relay")
	}

	// Re-unlocking restores imported key cleanly.
	if err := app.UnlockVault("lock-pass-123"); err != nil {
		t.Fatalf("UnlockVault after LockVault failed: %v", err)
	}
	keys, err := app.ListSSHKeys()
	if err != nil || len(keys) != 1 || keys[0].Fingerprint != parsed.Fingerprint {
		t.Fatalf("expected key restored after unlock, got %+v err=%v", keys, err)
	}
}
