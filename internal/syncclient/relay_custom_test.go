package syncclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeRelayURL(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantURL  string
		wantMode RelayTLSMode
		wantErr  bool
	}{
		{"empty_default", "", DefaultRelayURL, RelayTLSModePinned, false},
		{"default_bare", DefaultRelayHost, DefaultRelayURL, RelayTLSModePinned, false},
		{"bare_ipv4_port", "1.2.3.4:54843", "https://1.2.3.4:54843", RelayTLSModeSelfSignedIP, false},
		{"https_ipv4_slash", "https://1.2.3.4:54843/", "https://1.2.3.4:54843", RelayTLSModeSelfSignedIP, false},
		{"bare_ipv6_port", "[::1]:54843", "https://[::1]:54843", RelayTLSModeSelfSignedIP, false},
		{"bare_domain_port", "relay.example.com:54843", "https://relay.example.com:54843", RelayTLSModeSystemCA, false},
		{"https_domain", "https://relay.example.com/sync/", "https://relay.example.com/sync", RelayTLSModeSystemCA, false},
		{"reject_http", "http://1.2.3.4:54843", "", 0, true},
		{"reject_http_upper", "HTTP://relay.example.com:54843", "", 0, true},
		{"reject_ftp", "ftp://1.2.3.4:54843", "", 0, true},
		{"reject_spaces", "1.2.3.4:54843 extra", "", 0, true},
		{"reject_port_zero", "1.2.3.4:0", "", 0, true},
		{"reject_port_overflow", "1.2.3.4:65536", "", 0, true},
		{"reject_port_empty_colon", "https://1.2.3.4:", "", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotMode, err := NormalizeRelayURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got url=%q mode=%v", tc.raw, gotURL, gotMode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if gotURL != tc.wantURL || gotMode != tc.wantMode {
				t.Fatalf("NormalizeRelayURL(%q) = (%q, %v), want (%q, %v)", tc.raw, gotURL, gotMode, tc.wantURL, tc.wantMode)
			}
		})
	}
}

func TestCustomSelfSignedIPTLS13AndPing(t *testing.T) {
	const gateKey = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	var pingHits int

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
		if r.URL.Path == "/v1/ping" {
			pingHits++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "protocol": "gssh-sync-v2"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	// Strip https:// to exercise bare IP:port normalization -> RelayTLSModeSelfSignedIP.
	bareIPPort := strings.TrimPrefix(srv.URL, "https://")
	cleanURL, mode, err := NormalizeRelayURL(bareIPPort)
	if err != nil || mode != RelayTLSModeSelfSignedIP {
		t.Fatalf("expected RelayTLSModeSelfSignedIP for %q, got (%q, %v, %v)", bareIPPort, cleanURL, mode, err)
	}

	client, err := NewClientWithMode(cleanURL, gateKey, "", mode)
	if err != nil {
		t.Fatalf("NewClientWithMode failed: %v", err)
	}
	rtt, err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping over custom self-signed IP TLS 1.3 failed: %v", err)
	}
	if rtt < 0 || pingHits != 1 {
		t.Fatalf("unexpected ping result: rtt=%d hits=%d", rtt, pingHits)
	}

	// Wrong App Gate key must fail ping.
	badAuth, err := NewClientWithMode(cleanURL, strings.Repeat("0", 64), "", mode)
	if err != nil {
		t.Fatalf("NewClientWithMode badAuth failed: %v", err)
	}
	if _, err := badAuth.Ping(context.Background()); err == nil {
		t.Fatal("expected ping failure with wrong App Gate key")
	}

	// TLS 1.2-only server must be rejected even on custom self-signed IP.
	tls12Srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	tls12Srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
	tls12Srv.StartTLS()
	defer tls12Srv.Close()

	tls12Client, err := NewClientWithMode(tls12Srv.URL, gateKey, "", RelayTLSModeSelfSignedIP)
	if err != nil {
		t.Fatalf("NewClientWithMode tls12 failed: %v", err)
	}
	if _, err := tls12Client.Ping(context.Background()); err == nil {
		t.Fatal("expected TLS 1.2-only server to be rejected by TLS 1.3 enforcement")
	}
}

func TestSyncClientSafetyGuards(t *testing.T) {
	const gateKey = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	// Unknown TLS mode must error.
	if _, err := NewClientWithMode("https://127.0.0.1:443", gateKey, "", RelayTLSMode(99)); err == nil {
		t.Fatal("expected error for unknown RelayTLSMode")
	}

	// Oversized response (>4 MiB) must be rejected instead of silently truncated.
	bigSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBodyBytes+16)))
	}))
	bigSrv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	bigSrv.StartTLS()
	defer bigSrv.Close()

	cli, err := NewClientWithMode(bigSrv.URL, gateKey, "", RelayTLSModeSelfSignedIP)
	if err != nil {
		t.Fatalf("NewClientWithMode failed: %v", err)
	}
	if _, err := cli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("expected 4 MiB overflow error, got %v", err)
	}

	// Pre-check 2 MiB payload limit before sending over network.
	hugePayload := EncryptedPayload{
		ChannelID:  strings.Repeat("a", 64),
		Salt:       "abcd",
		Nonce:      "efgh",
		Ciphertext: strings.Repeat("A", maxDropPayloadBytes+10),
	}
	if err := cli.PairApprove(context.Background(), strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), hugePayload); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("expected 2 MiB payload pre-check error, got %v", err)
	}
}

func TestSyncClientBlocksRedirects(t *testing.T) {
	const gateKey = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	var targetHits int
	downgradeTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer downgradeTarget.Close()

	redirRelay := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, downgradeTarget.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	redirRelay.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	redirRelay.StartTLS()
	defer redirRelay.Close()

	cli, err := NewClientWithMode(redirRelay.URL, gateKey, "", RelayTLSModeSelfSignedIP)
	if err != nil {
		t.Fatalf("NewClientWithMode failed: %v", err)
	}
	if _, err := cli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("expected HTTP 307 error without following redirect, got %v", err)
	}
	if targetHits != 0 {
		t.Fatalf("sync client followed 307 redirect to %s (%d hits)", downgradeTarget.URL, targetHits)
	}
}

func makeSelfSignedCertForIP(t *testing.T, ipStr string) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: ipStr},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP(ipStr)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        leaf,
	}
}

func makeSelfSignedCertForDNS(t *testing.T, dnsName string) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{dnsName},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        leaf,
	}
}

func TestCustomRelayExplicitSPKIPinSANAndTOFUHardening(t *testing.T) {
	const gateKey = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	cert1 := makeSelfSignedCertForIP(t, "127.0.0.1")
	cert2 := makeSelfSignedCertForIP(t, "127.0.0.1")
	wrongIPCert := makeSelfSignedCertForIP(t, "198.51.100.77")

	sum1 := sha256.Sum256(cert1.Leaf.RawSubjectPublicKeyInfo)
	pin1 := hex.EncodeToString(sum1[:])

	// 1. NormalizeRelayURLWithPin parses valid #spki= and rejects malformed #spki=.
	cleanURL, mode, gotPin, err := NormalizeRelayURLWithPin("127.0.0.1:54843#spki=" + strings.ToUpper(pin1))
	if err != nil || cleanURL != "https://127.0.0.1:54843" || mode != RelayTLSModePinned || gotPin != pin1 {
		t.Fatalf("unexpected NormalizeRelayURLWithPin: url=%q mode=%v pin=%q err=%v", cleanURL, mode, gotPin, err)
	}
	if _, _, _, err := NormalizeRelayURLWithPin("https://127.0.0.1:54843#spki=not-hex"); err == nil {
		t.Fatal("expected malformed #spki= fragment to be rejected")
	}

	// 2. Custom raw-IP TLS 1.3 server with swappable certificate (avoid httptest.StartTLS
	//    which injects a default Certificates[0] that bypasses GetCertificate for raw IPs).
	var activeCert atomic.Pointer[tls.Certificate]
	activeCert.Store(&cert1)

	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsLn := tls.NewListener(rawLn, &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return activeCert.Load(), nil
		},
	})
	httpSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if r.Header.Get("X-GSSH-Client") != ClientHeaderValue ||
				r.Header.Get("X-GSSH-Sig") != SignRequest(gateKey, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}),
	}
	go func() { _ = httpSrv.Serve(tlsLn) }()
	defer httpSrv.Close()
	srvURL := "https://" + rawLn.Addr().String()

	// Explicit #spki= matches cert1 and rejects mismatching pin.
	cURL, cMode, cPin, err := NormalizeRelayURLWithPin(srvURL + "#spki=" + pin1)
	if err != nil || cMode != RelayTLSModePinned {
		t.Fatalf("NormalizeRelayURLWithPin failed: %v", err)
	}
	pinnedCli, err := NewClientWithMode(cURL, gateKey, cPin, cMode)
	if err != nil {
		t.Fatalf("NewClientWithMode pinned failed: %v", err)
	}
	if _, err := pinnedCli.Ping(context.Background()); err != nil {
		t.Fatalf("explicit #spki= pin Ping failed: %v", err)
	}

	// 3. TOFU on RelayTLSModeSelfSignedIP pins cert1 on first handshake, then rejects MITM swap to cert2.
	tofuCli, err := NewClientWithMode(srvURL, gateKey, "", RelayTLSModeSelfSignedIP)
	if err != nil {
		t.Fatalf("NewClientWithMode TOFU failed: %v", err)
	}
	if _, err := tofuCli.Ping(context.Background()); err != nil {
		t.Fatalf("initial TOFU Ping failed: %v", err)
	}
	// Close idle connections so the next request performs a fresh TLS handshake against the swapped cert2.
	tofuCli.CloseIdleConnections()
	activeCert.Store(&cert2)
	if _, err := tofuCli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "TOFU") {
		t.Fatalf("expected TOFU SPKI mismatch error after cert swap, got: %v", err)
	}

	// 4. Raw-IP cert with mismatched IP SANs must be rejected even on first handshake.
	activeCert.Store(&wrongIPCert)
	wrongSanCli, err := NewClientWithMode(srvURL, gateKey, "", RelayTLSModeSelfSignedIP)
	if err != nil {
		t.Fatalf("NewClientWithMode wrongSan failed: %v", err)
	}
	if _, err := wrongSanCli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "SAN") {
		t.Fatalf("expected SAN IP mismatch error, got: %v", err)
	}

	// 5. Default self-hosted bbplin/sshsync cert (DNSNames:["sshsync"], empty IPAddresses)
	//    must work out-of-the-box under RelayTLSModeSelfSignedIP AND enforce TOFU on swap.
	sshsyncCertA := makeSelfSignedCertForDNS(t, "sshsync")
	sshsyncCertB := makeSelfSignedCertForDNS(t, "sshsync")
	activeCert.Store(&sshsyncCertA)
	defaultSelfHostedCli, err := NewClientWithMode(srvURL, gateKey, "", RelayTLSModeSelfSignedIP)
	if err != nil {
		t.Fatalf("NewClientWithMode defaultSelfHosted failed: %v", err)
	}
	if _, err := defaultSelfHostedCli.Ping(context.Background()); err != nil {
		t.Fatalf("expected default bbplin/sshsync cert (DNSNames:[sshsync], no IP SAN) to succeed: %v", err)
	}
	defaultSelfHostedCli.CloseIdleConnections()
	activeCert.Store(&sshsyncCertB)
	if _, err := defaultSelfHostedCli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "TOFU") {
		t.Fatalf("expected TOFU mismatch when default bbplin/sshsync cert rotates, got: %v", err)
	}
}
