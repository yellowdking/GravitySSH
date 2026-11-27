package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
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

	tok := []byte("0123456789abcdef0123456789abcdef")
	reqID0 := makeTestHex64("slot-req-0")

	// Fill all 64 slots
	for i := 0; i < maxSlots; i++ {
		ch := makeTestChannel(fmt.Sprintf("slot-%d", i))
		ttl := 30
		wantTTL := 60 * time.Second
		if i == 1 {
			ttl = 900
			wantTTL = 600 * time.Second
		}
		exp, err := st.PairInit(ch, tok, ttl)
		if err != nil {
			t.Fatalf("pair init %d: %v", i, err)
		}
		if !exp.Equal(now.Add(wantTTL)) {
			t.Fatalf("slot %d expected ttl %v, got %v", i, wantTTL, exp.Sub(now))
		}
	}
	// 65th slot -> errStoreFull
	if _, err := st.PairInit(makeTestChannel("overflow"), tok, 0); err != errStoreFull {
		t.Fatalf("expected errStoreFull, got %v", err)
	}

	// Approve slot-0 and read via PairStatus -> returns slot and removes it; zeroSlot wipes bytes
	ch0 := makeTestChannel("slot-0")
	if _, err := st.PairRequest(ch0, reqID0, []byte("rs"), []byte("rn"), []byte("rct")); err != nil {
		t.Fatalf("pair request 0: %v", err)
	}
	if err := st.PairApprove(ch0, tok, reqID0, []byte("salt"), []byte("nonce"), []byte("secret-ct")); err != nil {
		t.Fatalf("pair approve 0: %v", err)
	}
	state, entry, code := st.PairStatus(ch0, reqID0)
	if code != http.StatusOK || state != "approved" || entry == nil {
		t.Fatalf("expected approved status hit, got state=%q code=%d", state, code)
	}
	ctRef := entry.ciphertext
	zeroSlot(entry)
	for _, b := range ctRef {
		if b != 0 {
			t.Fatalf("expected zeroed memory, found %d", b)
		}
	}

	// Advance 61s -> Sweep removes the 63 slots with 60s TTL (slot-1 has 600s TTL and remains)
	now = now.Add(61 * time.Second)
	if swept := st.Sweep(); swept != maxSlots-2 {
		t.Fatalf("expected %d swept, got %d", maxSlots-2, swept)
	}
	if st.Len() != 1 {
		t.Fatalf("expected 1 remaining slot with 600s TTL, len=%d", st.Len())
	}
	// Advance past 600s -> Sweep removes slot-1
	now = now.Add(540 * time.Second)
	if swept := st.Sweep(); swept != 1 {
		t.Fatalf("expected 1 swept for 600s TTL slot, got %d", swept)
	}
	if st.Len() != 0 || st.TotalBytes() != 0 {
		t.Fatalf("expected empty store, len=%d bytes=%d", st.Len(), st.TotalBytes())
	}
}

func TestStoreMaxTotalBytesAndB64Escapes(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	st := newMemStore(func() time.Time { return now })

	tok := []byte("0123456789abcdef0123456789abcdef")
	chunk := make([]byte, 2<<20) // 2 MiB raw binary
	for i := 0; i < int(maxTotalBytes/(2<<20)); i++ {
		ch := makeTestChannel(fmt.Sprintf("big-%d", i))
		reqID := makeTestHex64(fmt.Sprintf("big-req-%d", i))
		if _, err := st.PairInit(ch, tok, 120); err != nil {
			t.Fatalf("pair init big %d failed: %v", i, err)
		}
		if _, err := st.PairRequest(ch, reqID, []byte{1}, []byte{2}, []byte{3}); err != nil {
			t.Fatalf("pair request big %d failed: %v", i, err)
		}
		if err := st.PairApprove(ch, tok, reqID, []byte{}, []byte{}, chunk); err != nil {
			t.Fatalf("pair approve big %d failed: %v", i, err)
		}
	}
	if st.TotalBytes() != maxTotalBytes {
		t.Fatalf("expected totalBytes=%d, got %d", maxTotalBytes, st.TotalBytes())
	}
	chOver := makeTestChannel("big-over")
	reqOver := makeTestHex64("big-over-req")
	if _, err := st.PairInit(chOver, tok, 120); err != nil {
		t.Fatalf("pair init big-over failed: %v", err)
	}
	if _, err := st.PairRequest(chOver, reqOver, []byte{1}, []byte{2}, []byte{3}); err != errStoreFull {
		t.Fatalf("expected errStoreFull over maxTotalBytes, got %v", err)
	}

	// Reclaim via PairStatus -> new PairRequest succeeds
	state, entry, code := st.PairStatus(makeTestChannel("big-0"), makeTestHex64("big-req-0"))
	if code != http.StatusOK || state != "approved" || entry == nil {
		t.Fatalf("expected approved status of big-0, got state=%q code=%d", state, code)
	}
	zeroSlot(entry)
	if _, err := st.PairRequest(chOver, reqOver, []byte{1}, []byte{2}, []byte{3}); err != nil {
		t.Fatalf("expected pair request after reclaim to succeed, got %v", err)
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

	tok := makeTestHex64("rl-tok")
	ip := "198.51.100.77"
	for i := 0; i < 5; i++ {
		body := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, makeTestChannel(fmt.Sprintf("rl-%d", i)), tok))
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, signReq(t, http.MethodPost, "/v1/pair/init", body, testAppKey, now.Unix(), ip))
		if r.Code != http.StatusOK {
			t.Fatalf("burst req %d expected 200, got %d", i+1, r.Code)
		}
	}
	// 6th immediate request -> 429 rate limited
	body6 := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, makeTestChannel("rl-5"), tok))
	r6 := httptest.NewRecorder()
	srv.ServeHTTP(r6, signReq(t, http.MethodPost, "/v1/pair/init", body6, testAppKey, now.Unix(), ip))
	if r6.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th burst req, got %d", r6.Code)
	}
	// Advance 6s -> 1 token refilled -> 200
	now = now.Add(6 * time.Second)
	r7 := httptest.NewRecorder()
	srv.ServeHTTP(r7, signReq(t, http.MethodPost, "/v1/pair/init", body6, testAppKey, now.Unix(), ip))
	if r7.Code != http.StatusOK {
		t.Fatalf("expected 200 after token refill, got %d", r7.Code)
	}

	// Oversized heavy approve body (> 2 MiB) -> silent 404 + strike (never leaks 413)
	huge := bytes.Repeat([]byte("A"), maxBodyBytes+10)
	badIP := "198.51.100.88"
	for i := 0; i < strikeLimit; i++ {
		req := signReq(t, http.MethodPost, "/v1/pair/approve", huge, testAppKey, now.Unix(), badIP)
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

func makeTestHex64(tag string) string {
	s := sha256.Sum256([]byte(tag))
	return hex.EncodeToString(s[:])
}

func TestPairFullFlowAndBurnAfterRead(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	store := newMemStore(nowFn)
	guard := newIPGuard(nowFn)
	srv := newRelayServer(store, guard, testAppKey, nowFn)

	ch := makeTestHex64("pair-chan-1")
	senderTok := makeTestHex64("sender-tok-1")
	reqID := makeTestHex64("req-id-1")
	senderIP := "198.51.100.30"
	recvIP := "198.51.100.31"

	// 1. PairInit
	initBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s","ttlSeconds":300}`, ch, senderTok))
	rInit := httptest.NewRecorder()
	srv.ServeHTTP(rInit, signReq(t, http.MethodPost, "/v1/pair/init", initBody, testAppKey, now.Unix(), senderIP))
	if rInit.Code != http.StatusOK || !strings.Contains(rInit.Body.String(), `"ok":true`) {
		t.Fatalf("pair/init failed: %d %s", rInit.Code, rInit.Body.String())
	}

	// Duplicate init -> 409 exists
	rDupInit := httptest.NewRecorder()
	srv.ServeHTTP(rDupInit, signReq(t, http.MethodPost, "/v1/pair/init", initBody, testAppKey, now.Unix(), senderIP))
	if rDupInit.Code != http.StatusConflict || !strings.Contains(rDupInit.Body.String(), `"exists"`) {
		t.Fatalf("duplicate pair/init expected 409 exists, got %d %s", rDupInit.Code, rDupInit.Body.String())
	}

	// 2. PairPoll while waiting
	pollBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, ch, senderTok))
	rPollWait := httptest.NewRecorder()
	srv.ServeHTTP(rPollWait, signReq(t, http.MethodPost, "/v1/pair/poll", pollBody, testAppKey, now.Unix(), senderIP))
	if rPollWait.Code != http.StatusOK || rPollWait.Body.String() != `{"state":"waiting"}` {
		t.Fatalf("pair/poll waiting got %d %s", rPollWait.Code, rPollWait.Body.String())
	}

	// 3. PairRequest
	reqSalt := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	reqNonce := base64.StdEncoding.EncodeToString([]byte{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22})
	reqCT := base64.StdEncoding.EncodeToString([]byte("encrypted-pair-request-info"))
	reqBody := []byte(fmt.Sprintf(
		`{"channelId":"%s","requestId":"%s","salt":"%s","nonce":"%s","ciphertext":"%s"}`,
		ch, reqID, reqSalt, reqNonce, reqCT,
	))
	rReq := httptest.NewRecorder()
	srv.ServeHTTP(rReq, signReq(t, http.MethodPost, "/v1/pair/request", reqBody, testAppKey, now.Unix(), recvIP))
	if rReq.Code != http.StatusOK || !strings.Contains(rReq.Body.String(), `"ok":true`) {
		t.Fatalf("pair/request failed: %d %s", rReq.Code, rReq.Body.String())
	}
	if store.TotalBytes() != int64(16+12+len("encrypted-pair-request-info")) {
		t.Fatalf("unexpected totalBytes after pair/request: %d", store.TotalBytes())
	}

	// Concurrent second request -> 409 busy
	reqID2 := makeTestHex64("req-id-2")
	reqBody2 := []byte(fmt.Sprintf(
		`{"channelId":"%s","requestId":"%s","salt":"%s","nonce":"%s","ciphertext":"%s"}`,
		ch, reqID2, reqSalt, reqNonce, reqCT,
	))
	rBusy := httptest.NewRecorder()
	srv.ServeHTTP(rBusy, signReq(t, http.MethodPost, "/v1/pair/request", reqBody2, testAppKey, now.Unix(), "198.51.100.33"))
	if rBusy.Code != http.StatusConflict || !strings.Contains(rBusy.Body.String(), `"busy"`) {
		t.Fatalf("concurrent pair/request expected 409 busy, got %d %s", rBusy.Code, rBusy.Body.String())
	}

	// 4. PairStatus while requested -> pending
	statusBody := []byte(fmt.Sprintf(`{"channelId":"%s","requestId":"%s"}`, ch, reqID))
	rStatusPend := httptest.NewRecorder()
	srv.ServeHTTP(rStatusPend, signReq(t, http.MethodPost, "/v1/pair/status", statusBody, testAppKey, now.Unix(), recvIP))
	if rStatusPend.Code != http.StatusOK || rStatusPend.Body.String() != `{"state":"pending"}` {
		t.Fatalf("pair/status pending got %d %s", rStatusPend.Code, rStatusPend.Body.String())
	}

	// 5. PairPoll while requested -> returns requestId + encrypted request
	rPollReq := httptest.NewRecorder()
	srv.ServeHTTP(rPollReq, signReq(t, http.MethodPost, "/v1/pair/poll", pollBody, testAppKey, now.Unix(), senderIP))
	if rPollReq.Code != http.StatusOK {
		t.Fatalf("pair/poll requested got %d %s", rPollReq.Code, rPollReq.Body.String())
	}
	var pollResp struct {
		State      string `json:"state"`
		RequestID  string `json:"requestId"`
		Salt       string `json:"salt"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(rPollReq.Body.Bytes(), &pollResp); err != nil {
		t.Fatalf("unmarshal poll resp: %v", err)
	}
	if pollResp.State != "requested" || pollResp.RequestID != reqID ||
		pollResp.Salt != reqSalt || pollResp.Nonce != reqNonce || pollResp.Ciphertext != reqCT {
		t.Fatalf("unexpected poll resp: %+v", pollResp)
	}

	// 6. PairApprove (test heavy body > 4096 bytes allowed on /v1/pair/approve)
	appSaltRaw := bytes.Repeat([]byte{0xaa}, 16)
	appNonceRaw := bytes.Repeat([]byte{0xbb}, 12)
	appCTRaw := bytes.Repeat([]byte{0xcc}, 8192)
	appSalt := base64.StdEncoding.EncodeToString(appSaltRaw)
	appNonce := base64.StdEncoding.EncodeToString(appNonceRaw)
	appCT := base64.StdEncoding.EncodeToString(appCTRaw)
	appBody := []byte(fmt.Sprintf(
		`{"channelId":"%s","senderToken":"%s","requestId":"%s","salt":"%s","nonce":"%s","ciphertext":"%s"}`,
		ch, senderTok, reqID, appSalt, appNonce, appCT,
	))
	rApp := httptest.NewRecorder()
	srv.ServeHTTP(rApp, signReq(t, http.MethodPost, "/v1/pair/approve", appBody, testAppKey, now.Unix(), senderIP))
	if rApp.Code != http.StatusOK || !strings.Contains(rApp.Body.String(), `"ok":true`) {
		t.Fatalf("pair/approve failed: %d %s", rApp.Code, rApp.Body.String())
	}
	wantBytes := int64(len(appSaltRaw) + len(appNonceRaw) + len(appCTRaw))
	if store.TotalBytes() != wantBytes {
		t.Fatalf("expected totalBytes=%d after approve, got %d", wantBytes, store.TotalBytes())
	}

	// 7. PairStatus when approved -> returns payload and burns slot
	now = now.Add(6 * time.Second)
	rStatusApp := httptest.NewRecorder()
	srv.ServeHTTP(rStatusApp, signReq(t, http.MethodPost, "/v1/pair/status", statusBody, testAppKey, now.Unix(), recvIP))
	if rStatusApp.Code != http.StatusOK {
		t.Fatalf("pair/status approved failed: %d %s", rStatusApp.Code, rStatusApp.Body.String())
	}
	var statusResp struct {
		State      string `json:"state"`
		Salt       string `json:"salt"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(rStatusApp.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("unmarshal status resp: %v", err)
	}
	if statusResp.State != "approved" || statusResp.Salt != appSalt || statusResp.Nonce != appNonce || statusResp.Ciphertext != appCT {
		t.Fatalf("unexpected approved status resp: %+v", statusResp)
	}
	if store.Len() != 0 || store.TotalBytes() != 0 {
		t.Fatalf("expected clean store after burn-after-read, len=%d bytes=%d", store.Len(), store.TotalBytes())
	}

	// 8. Second PairStatus & PairPoll after burn -> silent 404
	rStatusGone := httptest.NewRecorder()
	srv.ServeHTTP(rStatusGone, signReq(t, http.MethodPost, "/v1/pair/status", statusBody, testAppKey, now.Unix(), recvIP))
	if rStatusGone.Code != http.StatusNotFound || rStatusGone.Body.Len() != 0 {
		t.Fatalf("expected silent 404 on 2nd pair/status, got %d body=%q", rStatusGone.Code, rStatusGone.Body.String())
	}
	rPollGone := httptest.NewRecorder()
	srv.ServeHTTP(rPollGone, signReq(t, http.MethodPost, "/v1/pair/poll", pollBody, testAppKey, now.Unix(), senderIP))
	if rPollGone.Code != http.StatusNotFound || rPollGone.Body.Len() != 0 {
		t.Fatalf("expected silent 404 on pair/poll after burn, got %d body=%q", rPollGone.Code, rPollGone.Body.String())
	}
}

func TestPairRejectAndExpiryClamp(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	store := newMemStore(nowFn)
	guard := newIPGuard(nowFn)
	srv := newRelayServer(store, guard, testAppKey, nowFn)

	ch := makeTestHex64("pair-rej-chan")
	senderTok := makeTestHex64("pair-rej-tok")
	reqID := makeTestHex64("pair-rej-req")
	senderIP := "198.51.100.40"
	recvIP := "198.51.100.41"

	initBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s","ttlSeconds":300}`, ch, senderTok))
	rInit := httptest.NewRecorder()
	srv.ServeHTTP(rInit, signReq(t, http.MethodPost, "/v1/pair/init", initBody, testAppKey, now.Unix(), senderIP))
	if rInit.Code != http.StatusOK {
		t.Fatalf("init failed: %d", rInit.Code)
	}

	reqBody := []byte(fmt.Sprintf(
		`{"channelId":"%s","requestId":"%s","salt":"AQID","nonce":"BAUG","ciphertext":"BwgJ"}`,
		ch, reqID,
	))
	rReq := httptest.NewRecorder()
	srv.ServeHTTP(rReq, signReq(t, http.MethodPost, "/v1/pair/request", reqBody, testAppKey, now.Unix(), recvIP))
	if rReq.Code != http.StatusOK {
		t.Fatalf("request failed: %d", rReq.Code)
	}
	if store.TotalBytes() == 0 {
		t.Fatal("expected nonzero totalBytes after pair/request")
	}

	rejBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s","requestId":"%s"}`, ch, senderTok, reqID))
	rRej := httptest.NewRecorder()
	srv.ServeHTTP(rRej, signReq(t, http.MethodPost, "/v1/pair/reject", rejBody, testAppKey, now.Unix(), senderIP))
	if rRej.Code != http.StatusOK || !strings.Contains(rRej.Body.String(), `"ok":true`) {
		t.Fatalf("reject failed: %d %s", rRej.Code, rRej.Body.String())
	}
	if store.TotalBytes() != 0 {
		t.Fatalf("expected totalBytes=0 immediately after reject, got %d", store.TotalBytes())
	}

	// Receiver sees rejected once, then slot is deleted
	statusBody := []byte(fmt.Sprintf(`{"channelId":"%s","requestId":"%s"}`, ch, reqID))
	rStatus := httptest.NewRecorder()
	srv.ServeHTTP(rStatus, signReq(t, http.MethodPost, "/v1/pair/status", statusBody, testAppKey, now.Unix(), recvIP))
	if rStatus.Code != http.StatusOK || rStatus.Body.String() != `{"state":"rejected"}` {
		t.Fatalf("status rejected got %d %s", rStatus.Code, rStatus.Body.String())
	}
	if store.Len() != 0 || store.TotalBytes() != 0 {
		t.Fatalf("expected empty store after rejected status, len=%d bytes=%d", store.Len(), store.TotalBytes())
	}

	// Also test expiry clamp: TTL=300s -> after reject at t=0, expires at t+30s
	ch2 := makeTestHex64("pair-rej-clamp")
	if _, err := store.PairInit(ch2, []byte("tok2"), 300); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PairRequest(ch2, reqID, []byte{1}, []byte{2}, []byte{3}); err != nil {
		t.Fatal(err)
	}
	if err := store.PairReject(ch2, []byte("tok2"), reqID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if swept := store.Sweep(); swept != 1 {
		t.Fatalf("expected rejected slot to expire after 30s clamp, swept=%d", swept)
	}
	if store.Len() != 0 || store.TotalBytes() != 0 {
		t.Fatalf("expected empty store after sweep, len=%d bytes=%d", store.Len(), store.TotalBytes())
	}
}

func TestPairAuthStrikesSmallBodyLimitAndCancel(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	store := newMemStore(nowFn)
	guard := newIPGuard(nowFn)
	srv := newRelayServer(store, guard, testAppKey, nowFn)

	ch := makeTestHex64("pair-sec-chan")
	senderTok := makeTestHex64("pair-sec-tok")
	wrongTok := makeTestHex64("pair-wrong-tok")
	reqID := makeTestHex64("pair-sec-req")
	wrongReqID := makeTestHex64("pair-wrong-req")
	ownerIP := "198.51.100.50"

	initBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s","ttlSeconds":120}`, ch, senderTok))
	rInit := httptest.NewRecorder()
	srv.ServeHTTP(rInit, signReq(t, http.MethodPost, "/v1/pair/init", initBody, testAppKey, now.Unix(), ownerIP))
	if rInit.Code != http.StatusOK {
		t.Fatalf("init failed: %d", rInit.Code)
	}
	reqBody := []byte(fmt.Sprintf(
		`{"channelId":"%s","requestId":"%s","salt":"AQID","nonce":"BAUG","ciphertext":"BwgJ"}`,
		ch, reqID,
	))
	rReq := httptest.NewRecorder()
	srv.ServeHTTP(rReq, signReq(t, http.MethodPost, "/v1/pair/request", reqBody, testAppKey, now.Unix(), "198.51.100.51"))
	if rReq.Code != http.StatusOK {
		t.Fatalf("request failed: %d", rReq.Code)
	}

	// Small body cap (> 4096 bytes) enforced on /v1/pair/request
	oversizedSmall := bytes.Repeat([]byte("a"), maxSmallBodyBytes+10)
	rSmall := httptest.NewRecorder()
	srv.ServeHTTP(rSmall, signReq(t, http.MethodPost, "/v1/pair/request", oversizedSmall, testAppKey, now.Unix(), "198.51.100.52"))
	if rSmall.Code != http.StatusNotFound || rSmall.Body.Len() != 0 {
		t.Fatalf("expected silent 404 on >4096 pair/request, got %d", rSmall.Code)
	}

	// Wrong senderToken / wrong requestId -> silent 404 + strike -> ban after strikeLimit
	badIP := "198.51.100.99"
	badPoll := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, ch, wrongTok))
	badStatus := []byte(fmt.Sprintf(`{"channelId":"%s","requestId":"%s"}`, ch, wrongReqID))
	badCancel := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, ch, wrongTok))
	for i := 0; i < strikeLimit; i++ {
		var path string
		var body []byte
		switch i % 3 {
		case 0:
			path, body = "/v1/pair/poll", badPoll
		case 1:
			path, body = "/v1/pair/status", badStatus
		default:
			path, body = "/v1/pair/cancel", badCancel
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, signReq(t, http.MethodPost, path, body, testAppKey, now.Unix(), badIP))
		if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 {
			t.Fatalf("strike %d on %s expected silent 404, got %d body=%q", i+1, path, rec.Code, rec.Body.String())
		}
		now = now.Add(10 * time.Second)
	}
	if !guard.IsBanned(badIP) {
		t.Fatalf("expected %s to be banned after %d bad token/requestId strikes", badIP, strikeLimit)
	}

	// Valid PairCancel cleans slot and releases bytes
	canBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, ch, senderTok))
	rCan := httptest.NewRecorder()
	srv.ServeHTTP(rCan, signReq(t, http.MethodPost, "/v1/pair/cancel", canBody, testAppKey, now.Unix(), ownerIP))
	if rCan.Code != http.StatusOK || !strings.Contains(rCan.Body.String(), `"ok":true`) {
		t.Fatalf("pair/cancel failed: %d %s", rCan.Code, rCan.Body.String())
	}
	if store.Len() != 0 || store.TotalBytes() != 0 {
		t.Fatalf("expected empty store after pair/cancel, len=%d bytes=%d", store.Len(), store.TotalBytes())
	}
}

func TestPairStoreMemoryLimitsAndZeroing(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	st := newMemStore(func() time.Time { return now })

	// Fill store close to maxTotalBytes using a pair slot, leaving 100 bytes free
	bigCh := makeTestHex64("fill-store")
	bigTok := []byte("fedcba9876543210fedcba9876543210")
	bigReqID := makeTestHex64("fill-store-req")
	if _, err := st.PairInit(bigCh, bigTok, 120); err != nil {
		t.Fatalf("pair init fill: %v", err)
	}
	if _, err := st.PairRequest(bigCh, bigReqID, []byte{1}, []byte{2}, []byte{3}); err != nil {
		t.Fatalf("pair request fill: %v", err)
	}
	if err := st.PairApprove(bigCh, bigTok, bigReqID, nil, nil, make([]byte, maxTotalBytes-100)); err != nil {
		t.Fatalf("pair approve fill: %v", err)
	}

	pairCh := makeTestHex64("pair-mem-limit")
	tok := []byte("0123456789abcdef0123456789abcdef")
	reqID := makeTestHex64("req-mem-limit")
	if _, err := st.PairInit(pairCh, tok, 120); err != nil {
		t.Fatalf("pair init: %v", err)
	}
	if _, err := st.PairRequest(pairCh, reqID, make([]byte, 16), make([]byte, 12), make([]byte, 32)); err != nil {
		t.Fatalf("pair request (60B): %v", err)
	}
	// Replacing 60B with 120B exceeds maxTotalBytes by 20B -> errStoreFull
	if err := st.PairApprove(pairCh, tok, reqID, make([]byte, 16), make([]byte, 12), make([]byte, 92)); err != errStoreFull {
		t.Fatalf("expected errStoreFull on PairApprove delta overflow, got %v", err)
	}

	// Free bigCh -> PairApprove succeeds and zeroes old req* slices
	if !st.PairCancel(bigCh, bigTok) {
		t.Fatal("expected PairCancel(bigCh) to succeed")
	}
	sRef := st.slots[pairCh]
	reqCTRef := sRef.reqCiphertext
	if err := st.PairApprove(pairCh, tok, reqID, []byte("s1"), []byte("n1"), []byte("final-ct")); err != nil {
		t.Fatalf("pair approve after reclaim: %v", err)
	}
	for _, b := range reqCTRef {
		if b != 0 {
			t.Fatalf("expected old reqCiphertext to be zeroed on approve, found %d", b)
		}
	}
	if st.TotalBytes() != int64(len("s1")+len("n1")+len("final-ct")) {
		t.Fatalf("unexpected totalBytes after approve: %d", st.TotalBytes())
	}

	state, entry, code := st.PairStatus(pairCh, reqID)
	if code != http.StatusOK || state != "approved" || entry == nil {
		t.Fatalf("pair status expected approved, got state=%q code=%d", state, code)
	}
	tokRef := entry.senderToken
	ctRef := entry.ciphertext
	zeroSlot(entry)
	for _, b := range tokRef {
		if b != 0 {
			t.Fatalf("expected senderToken zeroed, found %d", b)
		}
	}
	for _, b := range ctRef {
		if b != 0 {
			t.Fatalf("expected ciphertext zeroed, found %d", b)
		}
	}
	if st.Len() != 0 || st.TotalBytes() != 0 {
		t.Fatalf("expected empty store, len=%d bytes=%d", st.Len(), st.TotalBytes())
	}
}
