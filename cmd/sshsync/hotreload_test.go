package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testReloadSignal struct{}

func (testReloadSignal) String() string { return "test-sighup" }
func (testReloadSignal) Signal()        {}

func copyFileBytes(t *testing.T, src, dst string, mode os.FileMode) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, mode); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func dialPeerSPKI(t *testing.T, addr string) string {
	t.Helper()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr, &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // Loopback test server with self-signed certs; SPKI verified explicitly below.
	})
	if err != nil {
		t.Fatalf("tls dial %s: %v", addr, err)
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		t.Fatal("server presented no peer certificates")
	}
	spki, err := spkiHexFromDER(certs[0].Raw)
	if err != nil {
		t.Fatalf("extract peer SPKI: %v", err)
	}
	return spki
}

func TestCertStoreGetCertificateAndLiveHotReload(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	c1Path, k1Path := writeTestCertPair(t, dir, "rot-1", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	c2Path, k2Path := writeTestCertPair(t, dir, "rot-2", now.Add(-time.Hour), now.Add(90*24*time.Hour))

	activeCert := filepath.Join(dir, "server.crt")
	activeKey := filepath.Join(dir, "server.key")
	copyFileBytes(t, c1Path, activeCert, 0o644)
	copyFileBytes(t, k1Path, activeKey, 0o600)

	initCert, spki1, persisted, err := loadRuntimeTLS(activeCert, activeKey, "127.0.0.1")
	if err != nil || !persisted {
		t.Fatalf("loadRuntimeTLS initial failed: persisted=%v err=%v", persisted, err)
	}
	certs := newCertStore(activeCert, activeKey, initCert)

	gotInit, err := certs.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil || gotInit == nil {
		t.Fatalf("GetCertificate initial failed: %v", err)
	}
	gotInitSPKI, err := spkiFromTLSCert(*gotInit)
	if err != nil || gotInitSPKI != spki1 {
		t.Fatalf("initial SPKI mismatch: got %q want %q err=%v", gotInitSPKI, spki1, err)
	}

	// Live TLS 1.3 server with ONLY GetCertificate (empty Certificates slice so raw-IP empty-SNI handshakes invoke GetCertificate).
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tlsLn := tls.NewListener(rawLn, &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: certs.GetCertificate,
	})
	srv := &http.Server{
		Handler: newRelayServer(newMemStore(nil), newIPGuard(nil), testAppKey, nil),
	}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = srv.Serve(tlsLn)
	}()
	defer func() {
		_ = srv.Close()
		<-serveDone
	}()

	if liveSPKI1 := dialPeerSPKI(t, rawLn.Addr().String()); liveSPKI1 != spki1 {
		t.Fatalf("live handshake before reload got SPKI %q, want %q", liveSPKI1, spki1)
	}

	// Rotate cert/key on disk and hot-reload.
	copyFileBytes(t, c2Path, activeCert, 0o644)
	copyFileBytes(t, k2Path, activeKey, 0o600)

	spki2, err := certs.Reload()
	if err != nil {
		t.Fatalf("certs.Reload failed: %v", err)
	}
	if !isLowerHex64(spki2) || spki2 == spki1 {
		t.Fatalf("expected distinct valid SPKI after rotation: spki1=%q spki2=%q", spki1, spki2)
	}

	// Next TLS 1.3 handshake must immediately serve rotated cert2 with zero downtime.
	if liveSPKI2 := dialPeerSPKI(t, rawLn.Addr().String()); liveSPKI2 != spki2 {
		t.Fatalf("live handshake after reload got SPKI %q, want %q", liveSPKI2, spki2)
	}
	if err := runHealthcheck(rawLn.Addr().String(), testAppKey, 3*time.Second); err != nil {
		t.Fatalf("healthcheck after hot-reload failed: %v", err)
	}

	// Concurrent GetCertificate readers + Reload writers must be race-free and never return nil/invalid certs.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c, gerr := certs.GetCertificate(&tls.ClientHelloInfo{})
				if gerr != nil || c == nil || len(c.Certificate) == 0 {
					t.Errorf("concurrent GetCertificate failed: cert=%v err=%v", c, gerr)
					return
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, rerr := certs.Reload(); rerr != nil {
					t.Errorf("concurrent Reload failed: %v", rerr)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestCertStoreReloadFailurePreservesCurrentCert(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

	c1Path, k1Path := writeTestCertPair(t, dir, "valid1", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	c2Path, k2Path := writeTestCertPair(t, dir, "valid2", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	expC, expK := writeTestCertPair(t, dir, "expired", now.Add(-48*time.Hour), now.Add(-time.Minute))
	futC, futK := writeTestCertPair(t, dir, "future", now.Add(time.Hour), now.Add(48*time.Hour))

	activeCert := filepath.Join(dir, "active.crt")
	activeKey := filepath.Join(dir, "active.key")
	copyFileBytes(t, c1Path, activeCert, 0o644)
	copyFileBytes(t, k1Path, activeKey, 0o600)

	initCert, spki1, _, err := loadRuntimeTLSAt(activeCert, activeKey, "127.0.0.1", now)
	if err != nil {
		t.Fatalf("load initial cert: %v", err)
	}
	certs := newCertStore(activeCert, activeKey, initCert)

	assertActiveSPKI := func(ctx string, want string) {
		t.Helper()
		c, err := certs.GetCertificate(nil)
		if err != nil || c == nil {
			t.Fatalf("%s: GetCertificate failed: %v", ctx, err)
		}
		got, err := spkiFromTLSCert(*c)
		if err != nil || got != want {
			t.Fatalf("%s: active SPKI=%q, want %q (err=%v)", ctx, got, want, err)
		}
	}

	// 1. Expired cert on disk -> reload rejected, cert1 preserved.
	copyFileBytes(t, expC, activeCert, 0o644)
	copyFileBytes(t, expK, activeKey, 0o600)
	if _, err := certs.ReloadAt(now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired cert reload error, got %v", err)
	}
	assertActiveSPKI("after expired reload", spki1)

	// 2. Not-yet-valid cert on disk -> reload rejected, cert1 preserved.
	copyFileBytes(t, futC, activeCert, 0o644)
	copyFileBytes(t, futK, activeKey, 0o600)
	if _, err := certs.ReloadAt(now); err == nil || !strings.Contains(err.Error(), "not yet valid") {
		t.Fatalf("expected not-yet-valid cert reload error, got %v", err)
	}
	assertActiveSPKI("after not-yet-valid reload", spki1)

	// 3. Corrupt PEM bytes on disk -> reload rejected, cert1 preserved.
	if err := os.WriteFile(activeCert, []byte("-----BEGIN CERTIFICATE-----\ncorrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activeKey, []byte("not-a-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := certs.ReloadAt(now); err == nil {
		t.Fatal("expected error reloading corrupt cert/key")
	}
	assertActiveSPKI("after corrupt reload", spki1)

	// 4. Mismatched cert2 + key1 on disk -> reload rejected, cert1 preserved.
	copyFileBytes(t, c2Path, activeCert, 0o644)
	copyFileBytes(t, k1Path, activeKey, 0o600)
	if _, err := certs.ReloadAt(now); err == nil {
		t.Fatal("expected error reloading mismatched cert/key pair")
	}
	assertActiveSPKI("after mismatched reload", spki1)

	// 5. Missing files on disk -> reload rejected, cert1 preserved.
	_ = os.Remove(activeCert)
	_ = os.Remove(activeKey)
	if _, err := certs.ReloadAt(now); err == nil {
		t.Fatal("expected error reloading missing cert/key")
	}
	assertActiveSPKI("after missing reload", spki1)

	// 6. Unconfigured certStore paths -> reload rejected, cert1 preserved.
	noPathStore := newCertStore("", "", initCert)
	if _, err := noPathStore.ReloadAt(now); err == nil {
		t.Fatal("expected error reloading certStore without paths")
	}
	if c, err := noPathStore.GetCertificate(nil); err != nil || c == nil {
		t.Fatalf("unconfigured certStore lost initial cert: %v", err)
	}

	// 7. Restoring valid cert2/key2 recovers cleanly and swaps to spki2.
	copyFileBytes(t, c2Path, activeCert, 0o644)
	copyFileBytes(t, k2Path, activeKey, 0o600)
	spki2, err := certs.ReloadAt(now)
	if err != nil {
		t.Fatalf("expected recovery reload to succeed, got %v", err)
	}
	if spki2 == spki1 {
		t.Fatalf("expected new SPKI after recovery, both were %q", spki1)
	}
	assertActiveSPKI("after valid recovery reload", spki2)
}

func TestCertReloaderSignalAndGracefulShutdown(t *testing.T) {
	// Smoke check OS-specific signal helper registration and cleanup.
	osCh, stopOS := startReloadSignal()
	stopOS()
	stopOS() // idempotent
	_ = osCh

	// Nil reloadCh (Windows stub path) closes done channel immediately with zero goroutines.
	stopNil := make(chan struct{})
	nilDone := startCertReloader(nil, nil, stopNil, nil)
	select {
	case <-nilDone:
	case <-time.After(time.Second):
		t.Fatal("expected nil-channel startCertReloader to return closed done channel")
	}

	dir := t.TempDir()
	now := time.Now().UTC()
	c1Path, k1Path := writeTestCertPair(t, dir, "sig-1", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	c2Path, k2Path := writeTestCertPair(t, dir, "sig-2", now.Add(-time.Hour), now.Add(90*24*time.Hour))

	activeCert := filepath.Join(dir, "server.crt")
	activeKey := filepath.Join(dir, "server.key")
	copyFileBytes(t, c1Path, activeCert, 0o644)
	copyFileBytes(t, k1Path, activeKey, 0o600)

	initCert, spki1, _, err := loadRuntimeTLS(activeCert, activeKey, "127.0.0.1")
	if err != nil {
		t.Fatalf("loadRuntimeTLS: %v", err)
	}
	certs := newCertStore(activeCert, activeKey, initCert)

	type reloadEvt struct {
		spki string
		err  error
	}
	results := make(chan reloadEvt, 4)
	reloadCh := make(chan os.Signal, 1)
	stopCh := make(chan struct{})

	done := startCertReloader(certs, reloadCh, stopCh, func(spki string, err error) {
		results <- reloadEvt{spki: spki, err: err}
	})

	// 1. Rotate on disk and trigger reload signal -> swaps to spki2.
	copyFileBytes(t, c2Path, activeCert, 0o644)
	copyFileBytes(t, k2Path, activeKey, 0o600)
	reloadCh <- testReloadSignal{}

	select {
	case ev := <-results:
		if ev.err != nil || !isLowerHex64(ev.spki) || ev.spki == spki1 {
			t.Fatalf("expected successful rotated reload, got spki=%q err=%v", ev.spki, ev.err)
		}
		got, err := spkiFromTLSCert(*certs.Current())
		if err != nil || got != ev.spki {
			t.Fatalf("certStore current SPKI=%q want %q err=%v", got, ev.spki, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reload result")
	}
	spki2, _ := spkiFromTLSCert(*certs.Current())

	// 2. Corrupt file on disk and trigger reload signal -> fails safely, preserves spki2.
	if err := os.WriteFile(activeCert, []byte("corrupt-pem"), 0o644); err != nil {
		t.Fatal(err)
	}
	reloadCh <- testReloadSignal{}

	select {
	case ev := <-results:
		if ev.err == nil {
			t.Fatal("expected error event on corrupt cert reload")
		}
		got, err := spkiFromTLSCert(*certs.Current())
		if err != nil || got != spki2 {
			t.Fatalf("expected previous cert %q preserved, got %q err=%v", spki2, got, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for failed reload result")
	}

	// 3. Closing stopCh exits reloader goroutine cleanly with zero leak.
	close(stopCh)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for cert reloader goroutine to stop")
	}
}

func TestProtocolVersionNegotiation(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	guard := newIPGuard(nowFn)
	srv := newRelayServer(newMemStore(nowFn), guard, testAppKey, nowFn)

	wantMin := fmt.Sprintf("%d", MinProtoVersion)
	wantMax := fmt.Sprintf("%d", MaxProtoVersion)
	want426 := fmt.Sprintf(`{"error":"unsupported_protocol","minVersion":%d,"maxVersion":%d}`, MinProtoVersion, MaxProtoVersion)

	assertProtoHeaders := func(t *testing.T, rec *httptest.ResponseRecorder, label string) {
		t.Helper()
		if got := rec.Header().Get(protoMinHeaderName); got != wantMin {
			t.Fatalf("%s: %s=%q, want %q", label, protoMinHeaderName, got, wantMin)
		}
		if got := rec.Header().Get(protoMaxHeaderName); got != wantMax {
			t.Fatalf("%s: %s=%q, want %q", label, protoMaxHeaderName, got, wantMax)
		}
	}

	assertStealth404NoProto := func(t *testing.T, rec *httptest.ResponseRecorder, label string) {
		t.Helper()
		if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "0" {
			t.Fatalf("%s: expected stealth 404, got code=%d body=%q", label, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Connection") != "close" {
			t.Fatalf("%s: expected Connection: close, got %q", label, rec.Header().Get("Connection"))
		}
		if rec.Header().Get(protoMinHeaderName) != "" || rec.Header().Get(protoMaxHeaderName) != "" {
			t.Fatalf("%s: stealth 404 leaked proto headers: min=%q max=%q",
				label, rec.Header().Get(protoMinHeaderName), rec.Header().Get(protoMaxHeaderName))
		}
	}

	// 1. Authenticated /v1/ping with omitted X-GSSH-Proto (backwards-compat), "1", and "2".
	for _, proto := range []string{"", "1", "2"} {
		req := signReq(t, http.MethodPost, "/v1/ping", []byte("{}"), testAppKey, now.Unix(), "203.0.113.30")
		if proto != "" {
			req.Header.Set(protoHeaderName, proto)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("ping proto=%q expected 200, got %d body=%q", proto, rec.Code, rec.Body.String())
		}
		assertProtoHeaders(t, rec, "ping proto="+proto)
	}

	// 2. Authenticated /v1/pair/* endpoints also emit X-GSSH-Proto-Min / Max and honor valid X-GSSH-Proto.
	tok := makeTestHex64("proto-tok")
	ch := makeTestChannel("proto-chan")
	initBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, ch, tok))
	initReq := signReq(t, http.MethodPost, "/v1/pair/init", initBody, testAppKey, now.Unix(), "203.0.113.31")
	initReq.Header.Set(protoHeaderName, "2")
	initRec := httptest.NewRecorder()
	srv.ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("pair/init proto=2 expected 200, got %d %s", initRec.Code, initRec.Body.String())
	}
	assertProtoHeaders(t, initRec, "pair/init proto=2")

	pollBody := []byte(fmt.Sprintf(`{"channelId":"%s","senderToken":"%s"}`, ch, tok))
	pollReq := signReq(t, http.MethodPost, "/v1/pair/poll", pollBody, testAppKey, now.Unix(), "203.0.113.31")
	// Omit X-GSSH-Proto on poll (v1.4.x / v1.1.x legacy client compatibility)
	pollRec := httptest.NewRecorder()
	srv.ServeHTTP(pollRec, pollReq)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("pair/poll omitted proto expected 200, got %d %s", pollRec.Code, pollRec.Body.String())
	}
	assertProtoHeaders(t, pollRec, "pair/poll omitted proto")

	// 3. Authenticated unsupported / malformed X-GSSH-Proto values -> 426 Upgrade Required (no strikes).
	badProtos := []string{"99", "0", "-1", "3", "abc", "1.5", "+1", "01"}
	protoTestIP := "203.0.113.32"
	for _, bad := range badProtos {
		req := signReq(t, http.MethodPost, "/v1/ping", []byte("{}"), testAppKey, now.Unix(), protoTestIP)
		req.Header.Set(protoHeaderName, bad)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUpgradeRequired {
			t.Fatalf("ping proto=%q expected 426, got %d body=%q", bad, rec.Code, rec.Body.String())
		}
		if rec.Body.String() != want426 {
			t.Fatalf("ping proto=%q body=%q, want %q", bad, rec.Body.String(), want426)
		}
		var parsed struct {
			Error      string `json:"error"`
			MinVersion int    `json:"minVersion"`
			MaxVersion int    `json:"maxVersion"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("426 JSON unmarshal failed: %v", err)
		}
		if parsed.Error != "unsupported_protocol" || parsed.MinVersion != MinProtoVersion || parsed.MaxVersion != MaxProtoVersion {
			t.Fatalf("unexpected 426 JSON: %+v", parsed)
		}
		assertProtoHeaders(t, rec, "ping 426 proto="+bad)
	}

	// Duplicate X-GSSH-Proto headers -> 426 Upgrade Required.
	dupReq := signReq(t, http.MethodPost, "/v1/ping", []byte("{}"), testAppKey, now.Unix(), protoTestIP)
	dupReq.Header.Add(protoHeaderName, "2")
	dupReq.Header.Add(protoHeaderName, "99")
	dupRec := httptest.NewRecorder()
	srv.ServeHTTP(dupRec, dupReq)
	if dupRec.Code != http.StatusUpgradeRequired || dupRec.Body.String() != want426 {
		t.Fatalf("duplicate X-GSSH-Proto expected 426 %s, got %d %q", want426, dupRec.Code, dupRec.Body.String())
	}
	assertProtoHeaders(t, dupRec, "duplicate X-GSSH-Proto")

	// Authenticated /v1/pair/init with unsupported X-GSSH-Proto -> 426 Upgrade Required.
	pairBadReq := signReq(t, http.MethodPost, "/v1/pair/init", initBody, testAppKey, now.Unix(), "203.0.113.33")
	pairBadReq.Header.Set(protoHeaderName, "99")
	pairBadRec := httptest.NewRecorder()
	srv.ServeHTTP(pairBadRec, pairBadReq)
	if pairBadRec.Code != http.StatusUpgradeRequired || pairBadRec.Body.String() != want426 {
		t.Fatalf("pair/init proto=99 expected 426 %s, got %d %q", want426, pairBadRec.Code, pairBadRec.Body.String())
	}
	assertProtoHeaders(t, pairBadRec, "pair/init proto=99")

	if strikes := guard.Strikes(protoTestIP); strikes != 0 {
		t.Fatalf("authenticated 426 responses must not record strikes, got %d", strikes)
	}

	// 4. Unauthenticated requests with unsupported X-GSSH-Proto MUST still get stealth 404 BEFORE protocol check!
	// (a) Missing auth headers entirely
	unauthNoHdr := httptest.NewRequest(http.MethodPost, "/v1/ping", strings.NewReader("{}"))
	unauthNoHdr.RemoteAddr = "203.0.113.40:5000"
	unauthNoHdr.Header.Set(protoHeaderName, "99")
	recNoHdr := httptest.NewRecorder()
	srv.ServeHTTP(recNoHdr, unauthNoHdr)
	assertStealth404NoProto(t, recNoHdr, "unauth missing headers proto=99")

	// (b) Wrong App Gate HMAC key with X-GSSH-Proto: 99 and abc
	wrongKey := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	for _, bad := range []string{"99", "abc", "2"} {
		badAuthReq := signReq(t, http.MethodPost, "/v1/ping", []byte("{}"), wrongKey, now.Unix(), "203.0.113.41")
		badAuthReq.Header.Set(protoHeaderName, bad)
		badAuthRec := httptest.NewRecorder()
		srv.ServeHTTP(badAuthRec, badAuthReq)
		assertStealth404NoProto(t, badAuthRec, "wrong HMAC key proto="+bad)
	}

	// (c) Clock-skewed request with X-GSSH-Proto: 99
	skewReq := signReq(t, http.MethodPost, "/v1/pair/init", initBody, testAppKey, now.Unix()-130, "203.0.113.42")
	skewReq.Header.Set(protoHeaderName, "99")
	skewRec := httptest.NewRecorder()
	srv.ServeHTTP(skewRec, skewReq)
	assertStealth404NoProto(t, skewRec, "clock skew proto=99")

	// (d) Unknown endpoint with valid HMAC and X-GSSH-Proto: 99
	unkReq := signReq(t, http.MethodPost, "/v1/unknown", []byte("{}"), testAppKey, now.Unix(), "203.0.113.43")
	unkReq.Header.Set(protoHeaderName, "99")
	unkRec := httptest.NewRecorder()
	srv.ServeHTTP(unkRec, unkReq)
	assertStealth404NoProto(t, unkRec, "unknown endpoint proto=99")
}
