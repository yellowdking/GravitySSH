package main

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
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
		exp, err := st.Drop(ch, []byte("salt"), []byte("nonce"), []byte("ct"), 30)
		if err != nil {
			t.Fatalf("drop %d: %v", i, err)
		}
		// ttl 30 clamped to 60
		if !exp.Equal(now.Add(60 * time.Second)) {
			t.Fatalf("expected ttl 60s, got %v", exp.Sub(now))
		}
	}
	// 65th slot -> errStoreFull
	if _, err := st.Drop(makeTestChannel("overflow"), []byte("salt"), []byte("nonce"), []byte("ct"), 0); err != errStoreFull {
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
	if st.Len() != 0 || st.TotalBytes() != 0 {
		t.Fatalf("expected empty store, len=%d bytes=%d", st.Len(), st.TotalBytes())
	}
}

func TestStoreMaxTotalBytesAndB64Escapes(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	st := newMemStore(func() time.Time { return now })

	chunk := make([]byte, 2<<20) // 2 MiB raw binary
	for i := 0; i < int(maxTotalBytes/(2<<20)); i++ {
		ch := makeTestChannel(fmt.Sprintf("big-%d", i))
		if _, err := st.Drop(ch, []byte{}, []byte{}, chunk, 120); err != nil {
			t.Fatalf("drop big %d failed: %v", i, err)
		}
	}
	if st.TotalBytes() != maxTotalBytes {
		t.Fatalf("expected totalBytes=%d, got %d", maxTotalBytes, st.TotalBytes())
	}
	if _, err := st.Drop(makeTestChannel("big-over"), []byte{1}, []byte{2}, []byte{3}, 120); err != errStoreFull {
		t.Fatalf("expected errStoreFull over maxTotalBytes, got %v", err)
	}

	// Reclaim via pickup -> new drop succeeds
	entry, ok := st.Pickup(makeTestChannel("big-0"))
	if !ok {
		t.Fatal("expected pickup of big-0")
	}
	zeroSlot(entry)
	if _, err := st.Drop(makeTestChannel("big-ok"), []byte{1}, []byte{2}, []byte{3}, 120); err != nil {
		t.Fatalf("expected drop after reclaim to succeed, got %v", err)
	}

	// Standard JSON string unmarshaling (\u002b and \/) + raw binary storage + roundtrip re-encode
	rawOrig := []byte{0xfb, 0xff, 0xfe, 0x3e, 0x3f, 0x01, 0x02, 0x03}
	wantB64 := base64.StdEncoding.EncodeToString(rawOrig)
	escaped := strings.ReplaceAll(strings.ReplaceAll(wantB64, "+", `\u002b`), "/", `\/`)
	var decoded b64Bytes
	if err := json.Unmarshal([]byte(`"`+escaped+`"`), &decoded); err != nil {
		t.Fatalf("unmarshal escaped b64: %v", err)
	}
	if !bytes.Equal(decoded, rawOrig) {
		t.Fatalf("expected raw binary %x, got %x", rawOrig, decoded)
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal b64Bytes: %v", err)
	}
	if string(reencoded) != `"`+wantB64+`"` {
		t.Fatalf("roundtrip mismatch: got %s want %q", reencoded, wantB64)
	}
}

func TestRateLimitBurstAndOversizedStealthStrike(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	guard := newIPGuard(nowFn)
	srv := newRelayServer(newMemStore(nowFn), guard, testAppKey, nowFn)

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

	// Oversized drop body (> 2 MiB) -> silent 404 + strike (never leaks 413)
	huge := bytes.Repeat([]byte("A"), maxBodyBytes+10)
	badIP := "198.51.100.88"
	for i := 0; i < strikeLimit; i++ {
		req := signReq(t, http.MethodPost, "/v1/drop", huge, testAppKey, now.Unix(), badIP)
		if i%2 == 1 {
			// Also test unknown/chunked ContentLength streaming path
			req.ContentLength = -1
			req.Body = io.NopCloser(bytes.NewReader(huge))
		}
		rHuge := httptest.NewRecorder()
		srv.ServeHTTP(rHuge, req)
		if rHuge.Code != http.StatusNotFound || rHuge.Body.Len() != 0 {
			t.Fatalf("oversized req %d: expected silent 404, got %d body=%q", i+1, rHuge.Code, rHuge.Body.String())
		}
	}
	if !guard.IsBanned(badIP) {
		t.Fatal("expected IP to be banned after repeated oversized body strikes")
	}
}

func TestBannedIPFloodEvictionBound(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	guard := newIPGuard(func() time.Time { return now })

	total := maxTrackedIPs + 1000
	for i := 0; i < total; i++ {
		ip := fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xff, (i>>8)&0xff, i&0xff)
		for s := 0; s < strikeLimit; s++ {
			guard.RecordStrike(ip)
		}
		now = now.Add(time.Millisecond)
		if len(guard.ips) > maxTrackedIPs {
			t.Fatalf("ip map exceeded maxTrackedIPs=%d at i=%d: len=%d", maxTrackedIPs, i, len(guard.ips))
		}
	}
	if len(guard.ips) > maxTrackedIPs {
		t.Fatalf("final ip map len=%d > maxTrackedIPs=%d", len(guard.ips), maxTrackedIPs)
	}
	// Newest banned IP should still be retained, oldest should have been evicted.
	oldestIP := "10.0.0.0"
	newestIP := fmt.Sprintf("10.%d.%d.%d", ((total-1)>>16)&0xff, ((total-1)>>8)&0xff, (total-1)&0xff)
	if guard.IsBanned(oldestIP) {
		t.Fatalf("expected oldest banned IP %s to be evicted", oldestIP)
	}
	if !guard.IsBanned(newestIP) {
		t.Fatalf("expected newest banned IP %s to remain banned", newestIP)
	}
}

func TestLimitListenerCloseUnblocksAcceptAndPerIPCap(t *testing.T) {
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln := newLimitListener(rawLn, 2, 1)

	// Dial 1st connection -> accepted (hits per-IP cap=1 for 127.0.0.1)
	c1, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	defer c1.Close()

	sc1, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept 1: %v", err)
	}

	// Dial 2nd connection from same IP (127.0.0.1) while sc1 is open -> listener rejects & closes it
	c2, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer c2.Close()

	// Start Accept in background; it should drop c2 (per-IP cap=1) and keep waiting until Close()
	acceptDone := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if conn != nil {
			_ = conn.Close()
		}
		acceptDone <- aerr
	}()

	// c2 should observe EOF/close promptly because per-IP cap dropped it
	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	var buf [1]byte
	if _, rerr := c2.Read(buf[:]); rerr == nil {
		t.Fatal("expected 2nd same-IP conn to be closed by per-IP cap")
	}

	// Now test deadlock fix: fill global sem completely (maxConns=1) and Close() while Accept blocks on sem
	_ = ln.Close()
	select {
	case aerr := <-acceptDone:
		if aerr == nil {
			t.Fatal("expected error from Accept after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not unblock on Close")
	}
	_ = sc1.Close()

	// Explicit full-semaphore Close() unblock test (sem capacity 1, held by open conn)
	rawLn2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen 2: %v", err)
	}
	ln2 := newLimitListener(rawLn2, 1, 1)
	d1, err := net.Dial("tcp", ln2.Addr().String())
	if err != nil {
		t.Fatalf("dial full: %v", err)
	}
	defer d1.Close()
	s1, err := ln2.Accept()
	if err != nil {
		t.Fatalf("accept full: %v", err)
	}
	defer s1.Close()

	blockedDone := make(chan error, 1)
	go func() {
		_, aerr := ln2.Accept()
		blockedDone <- aerr
	}()
	time.Sleep(30 * time.Millisecond)
	if err := ln2.Close(); err != nil {
		t.Fatalf("close full listener: %v", err)
	}
	select {
	case aerr := <-blockedDone:
		if aerr == nil {
			t.Fatal("expected net.ErrClosed from blocked Accept")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("limitListener.Close() deadlocked while Accept was waiting on full semaphore")
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
