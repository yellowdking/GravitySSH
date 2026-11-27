package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	if rec.Header().Get("Connection") != "close" {
		t.Fatalf("expected Connection: close on silent 404, got %q", rec.Header().Get("Connection"))
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

func writeTestCertPair(t *testing.T, dir, name string, notBefore, notAfter time.Time) (string, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "sshsync-test"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPath := filepath.Join(dir, name+".crt")
	keyPath := filepath.Join(dir, name+".key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestPartialCertKeyFlagsAndCertValidity(t *testing.T) {
	dir := t.TempDir()

	// 1. resolveCertKeyPaths fails fast if only one of --cert or --key is set
	if _, _, err := resolveCertKeyPaths("/tmp/only.crt", "", dir); err == nil {
		t.Fatal("expected error when only --cert is set")
	}
	if _, _, err := resolveCertKeyPaths("", "/tmp/only.key", dir); err == nil {
		t.Fatal("expected error when only --key is set")
	}
	defCert, defKey, err := resolveCertKeyPaths("", "", dir)
	if err != nil || defCert != filepath.Join(dir, "certs", "server.crt") || defKey != filepath.Join(dir, "certs", "server.key") {
		t.Fatalf("unexpected default cert/key paths: %q %q err=%v", defCert, defKey, err)
	}
	expCert, expKey, err := resolveCertKeyPaths(" /a/c.pem ", " /a/k.pem ", dir)
	if err != nil || expCert != "/a/c.pem" || expKey != "/a/k.pem" {
		t.Fatalf("unexpected explicit cert/key paths: %q %q err=%v", expCert, expKey, err)
	}

	// 2. loadRuntimeTLSAt fails fast on partial flag or incomplete disk pair (no silent ephemeral fallback)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	if _, _, _, err := loadRuntimeTLSAt(filepath.Join(dir, "only.crt"), "", "sshsync", now); err == nil {
		t.Fatal("expected loadRuntimeTLSAt error when keyFile is empty")
	}
	if _, _, _, err := loadRuntimeTLSAt("", filepath.Join(dir, "only.key"), "sshsync", now); err == nil {
		t.Fatal("expected loadRuntimeTLSAt error when certFile is empty")
	}
	orphanCert := filepath.Join(dir, "orphan.crt")
	if err := os.WriteFile(orphanCert, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadRuntimeTLSAt(orphanCert, filepath.Join(dir, "missing.key"), "sshsync", now); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete keypair error, got %v", err)
	}

	// 3. Expired certificate rejected
	expiredCert, expiredKey := writeTestCertPair(t, dir, "expired", now.Add(-48*time.Hour), now.Add(-time.Minute))
	if _, _, _, err := loadRuntimeTLSAt(expiredCert, expiredKey, "sshsync", now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired cert error, got %v", err)
	}

	// 4. Not-yet-valid certificate rejected
	futureCert, futureKey := writeTestCertPair(t, dir, "future", now.Add(time.Hour), now.Add(48*time.Hour))
	if _, _, _, err := loadRuntimeTLSAt(futureCert, futureKey, "sshsync", now); err == nil || !strings.Contains(err.Error(), "not yet valid") {
		t.Fatalf("expected not-yet-valid cert error, got %v", err)
	}

	// 5. Valid certificate (including <30d remaining) accepted
	validCert, validKey := writeTestCertPair(t, dir, "valid-soon", now.Add(-time.Hour), now.Add(10*24*time.Hour))
	_, spki, persisted, err := loadRuntimeTLSAt(validCert, validKey, "sshsync", now)
	if err != nil || !persisted || !isLowerHex64(spki) {
		t.Fatalf("expected valid cert to load cleanly, got spki=%q persisted=%v err=%v", spki, persisted, err)
	}
}

func TestHealthcheckEndpointAndTargetAddr(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{":54843", "127.0.0.1:54843"},
		{"0.0.0.0:54843", "127.0.0.1:54843"},
		{"[::]:54843", "[::1]:54843"},
		{"127.0.0.1:8443", "127.0.0.1:8443"},
	}
	for _, tc := range cases {
		got, err := healthcheckTargetAddr(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("healthcheckTargetAddr(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := healthcheckTargetAddr("no-port"); err == nil {
		t.Fatal("expected error for listen address without port")
	}

	// End-to-end TLS 1.3 healthcheck against live relayServer
	certPEM, keyPEM, _, err := generateSelfSignedCertBytes("127.0.0.1")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tlsLn := tls.NewListener(rawLn, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	})
	guard := newIPGuard(nil)
	srv := &http.Server{
		Handler: newRelayServer(newMemStore(nil), guard, testAppKey, nil),
	}
	go func() { _ = srv.Serve(tlsLn) }()
	defer srv.Close()

	if err := runHealthcheck(rawLn.Addr().String(), testAppKey, 3*time.Second); err != nil {
		t.Fatalf("expected healthcheck to succeed, got %v", err)
	}
	if strikes := guard.Strikes("127.0.0.1"); strikes != 0 {
		t.Fatalf("expected 0 strikes after valid healthcheck, got %d", strikes)
	}
	if err := runHealthcheck(rawLn.Addr().String(), "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 3*time.Second); err == nil {
		t.Fatal("expected healthcheck with wrong gate key to fail")
	}
}
