package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSilent404AndAutoBan(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	srv := newRelayServer(newMemStore(nowFn), newIPGuard(nowFn), testAppKey, nowFn)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:1111"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 {
		t.Fatalf("expected silent 404, got %d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Server") != "" || rec.Header().Get("X-Powered-By") != "" {
		t.Fatalf("unexpected server headers: %+v", rec.Header())
	}

	body := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, makeTestChannel("skew"), makeTestHex64("skew-tok")))
	recSkew := httptest.NewRecorder()
	srv.ServeHTTP(recSkew, signReq(t, http.MethodPost, "/v1/pair/init", body, testAppKey, now.Unix()-125, "203.0.113.6"))
	if recSkew.Code != http.StatusNotFound || recSkew.Body.Len() != 0 {
		t.Fatalf("expected 404 on clock skew, got %d", recSkew.Code)
	}

	badIP := "203.0.113.99"
	for i := 0; i < strikeLimit; i++ {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, signReq(t, http.MethodPost, "/v1/pair/init", body, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", now.Unix(), badIP))
		if r.Code != http.StatusNotFound {
			t.Fatalf("strike %d expected 404, got %d", i+1, r.Code)
		}
	}

	rValid := httptest.NewRecorder()
	srv.ServeHTTP(rValid, signReq(t, http.MethodPost, "/v1/pair/init", body, testAppKey, now.Unix(), badIP))
	if rValid.Code != http.StatusNotFound {
		t.Fatalf("expected banned IP to get 404, got %d", rValid.Code)
	}

	now = now.Add(banDuration + time.Second)
	rAfter := httptest.NewRecorder()
	srv.ServeHTTP(rAfter, signReq(t, http.MethodPost, "/v1/pair/init", body, testAppKey, now.Unix(), badIP))
	if rAfter.Code != http.StatusOK {
		t.Fatalf("expected 200 after ban expiry, got %d", rAfter.Code)
	}
}

func TestPingAuthAndSilent404(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	srv := newRelayServer(newMemStore(nowFn), newIPGuard(nowFn), testAppKey, nowFn)

	// Unauthenticated POST /v1/ping -> silent 404
	uReq := httptest.NewRequest(http.MethodPost, "/v1/ping", nil)
	uReq.RemoteAddr = "203.0.113.20:5000"
	uRec := httptest.NewRecorder()
	srv.ServeHTTP(uRec, uReq)
	if uRec.Code != http.StatusNotFound || uRec.Body.Len() != 0 || uRec.Header().Get("Content-Length") != "0" {
		t.Fatalf("unauth ping expected silent 404, got %d body=%q", uRec.Code, uRec.Body.String())
	}

	// GET /v1/ping -> silent 404
	gReq := signReq(t, http.MethodGet, "/v1/ping", nil, testAppKey, now.Unix(), "203.0.113.21")
	gRec := httptest.NewRecorder()
	srv.ServeHTTP(gRec, gReq)
	if gRec.Code != http.StatusNotFound || gRec.Body.Len() != 0 {
		t.Fatalf("GET ping expected silent 404, got %d", gRec.Code)
	}

	// Authenticated POST /v1/ping -> 200 JSON
	want := `{"ok":true,"service":"sshsync","protocol":"gssh-sync-v2"}`
	aRec := httptest.NewRecorder()
	srv.ServeHTTP(aRec, signReq(t, http.MethodPost, "/v1/ping", []byte("{}"), testAppKey, now.Unix(), "203.0.113.22"))
	if aRec.Code != http.StatusOK || aRec.Body.String() != want {
		t.Fatalf("auth ping got %d %q, want 200 %q", aRec.Code, aRec.Body.String(), want)
	}

	// Authenticated POST /v1/ping with empty body also succeeds
	eRec := httptest.NewRecorder()
	srv.ServeHTTP(eRec, signReq(t, http.MethodPost, "/v1/ping", nil, testAppKey, now.Unix(), "203.0.113.22"))
	if eRec.Code != http.StatusOK || eRec.Body.String() != want {
		t.Fatalf("empty body ping got %d %q", eRec.Code, eRec.Body.String())
	}
}

func TestAutoTLSAndDefaultGateKey(t *testing.T) {
	k, err := resolveGateKey("")
	if err != nil || k != defaultAppGateKey {
		t.Fatalf("expected default gate key, got %q err=%v", k, err)
	}
	k2, err := resolveGateKey(testAppKey)
	if err != nil || k2 != testAppKey {
		t.Fatalf("expected override key, got %q err=%v", k2, err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, spki1, persisted, err := loadRuntimeTLS(certFile, keyFile, "sshsync")
	if err != nil || !persisted || !isLowerHex64(spki1) {
		t.Fatalf("auto-gen persist failed: spki=%q persisted=%v err=%v", spki1, persisted, err)
	}
	_, spki2, persisted2, err := loadRuntimeTLS(certFile, keyFile, "sshsync")
	if err != nil || !persisted2 || spki2 != spki1 {
		t.Fatalf("reload mismatch: spki1=%q spki2=%q err=%v", spki1, spki2, err)
	}

	// Non-writable parent (a regular file) -> clean in-memory fallback
	blocker := filepath.Join(dir, "ro-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, spkiMem, persistedMem, err := loadRuntimeTLS(
		filepath.Join(blocker, "cert.pem"),
		filepath.Join(blocker, "key.pem"),
		"sshsync",
	)
	if err != nil || persistedMem || !isLowerHex64(spkiMem) {
		t.Fatalf("in-memory fallback failed: spki=%q persisted=%v err=%v", spkiMem, persistedMem, err)
	}
}
