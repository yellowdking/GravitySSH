package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreLimitsExpiryAndZeroing(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	st := newMemStore(func() time.Time { return now })

	// Fill all 64 slots
	for i := 0; i < maxSlots; i++ {
		ch := makeTestChannel(fmt.Sprintf("slot-%d", i))
		exp, err := st.Drop(ch, []byte("c2FsdA=="), []byte("bm9uY2U="), []byte("Y3Q="), 30)
		if err != nil {
			t.Fatalf("drop %d: %v", i, err)
		}
		// ttl 30 clamped to 60
		if !exp.Equal(now.Add(60 * time.Second)) {
			t.Fatalf("expected ttl 60s, got %v", exp.Sub(now))
		}
	}
	// 65th slot -> errStoreFull
	if _, err := st.Drop(makeTestChannel("overflow"), []byte("c2FsdA=="), []byte("bm9uY2U="), []byte("Y3Q="), 0); err != errStoreFull {
		t.Fatalf("expected errStoreFull, got %v", err)
	}

	// Pickup returns slot and removes it; zeroSlot wipes bytes
	ch0 := makeTestChannel("slot-0")
	entry, ok := st.Pickup(ch0)
	if !ok {
		t.Fatal("expected pickup hit")
	}
	ctRef := entry.ciphertext
	zeroSlot(entry)
	for _, b := range ctRef {
		if b != 0 {
			t.Fatalf("expected zeroed memory, found %d", b)
		}
	}

	// Advance past expiry -> Sweep removes all remaining 63 slots
	now = now.Add(61 * time.Second)
	if swept := st.Sweep(); swept != maxSlots-1 {
		t.Fatalf("expected %d swept, got %d", maxSlots-1, swept)
	}
	if st.Len() != 0 {
		t.Fatalf("expected empty store, len=%d", st.Len())
	}
}

func TestRateLimitBurstAndMaxBody(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	srv := newRelayServer(newMemStore(nowFn), newIPGuard(nowFn), testAppKey, nowFn)

	body := []byte(fmt.Sprintf(`{"channelId":"%s"}`, makeTestChannel("rl")))
	ip := "198.51.100.77"
	for i := 0; i < 5; i++ {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, signReq(t, http.MethodPost, "/v1/cancel", body, testAppKey, now.Unix(), ip))
		if r.Code != http.StatusOK {
			t.Fatalf("burst req %d expected 200, got %d", i+1, r.Code)
		}
	}
	// 6th immediate request -> 429 rate limited
	r6 := httptest.NewRecorder()
	srv.ServeHTTP(r6, signReq(t, http.MethodPost, "/v1/cancel", body, testAppKey, now.Unix(), ip))
	if r6.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th burst req, got %d", r6.Code)
	}
	// Advance 6s -> 1 token refilled -> 200
	now = now.Add(6 * time.Second)
	r7 := httptest.NewRecorder()
	srv.ServeHTTP(r7, signReq(t, http.MethodPost, "/v1/cancel", body, testAppKey, now.Unix(), ip))
	if r7.Code != http.StatusOK {
		t.Fatalf("expected 200 after token refill, got %d", r7.Code)
	}

	// Oversized drop body (> 2 MiB) -> 413
	huge := bytes.Repeat([]byte("A"), maxBodyBytes+10)
	rHuge := httptest.NewRecorder()
	srv.ServeHTTP(rHuge, signReq(t, http.MethodPost, "/v1/drop", huge, testAppKey, now.Unix(), "198.51.100.88"))
	if rHuge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on >2MiB body, got %d", rHuge.Code)
	}
}

func TestEnsureAssetsInitAndReload(t *testing.T) {
	dir := t.TempDir()
	a1, err := ensureAssets(dir, "107.172.98.169")
	if err != nil {
		t.Fatalf("ensureAssets init: %v", err)
	}
	if !isLowerHex64(a1.SPKIHex) || !isLowerHex64(a1.AppGateKey) {
		t.Fatalf("invalid generated hex: spki=%q key=%q", a1.SPKIHex, a1.AppGateKey)
	}
	spkiFile, err := os.ReadFile(filepath.Join(dir, "certs", "spki-sha256.txt"))
	if err != nil || strings.TrimSpace(string(spkiFile)) != a1.SPKIHex {
		t.Fatalf("spki file mismatch: %v %q", err, string(spkiFile))
	}

	// Verify SAN IP and 10-year validity
	certPEM, err := os.ReadFile(a1.CertPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	blk, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "107.172.98.169" {
		t.Fatalf("unexpected SAN IPs: %+v", cert.IPAddresses)
	}
	if cert.NotAfter.Sub(cert.NotBefore) < 9*365*24*time.Hour {
		t.Fatalf("cert validity too short: %v", cert.NotAfter.Sub(cert.NotBefore))
	}

	// Second call reloads same key and SPKI
	a2, err := ensureAssets(dir, "107.172.98.169")
	if err != nil {
		t.Fatalf("ensureAssets reload: %v", err)
	}
	if a2.SPKIHex != a1.SPKIHex || a2.AppGateKey != a1.AppGateKey {
		t.Fatalf("reload changed assets")
	}
}
