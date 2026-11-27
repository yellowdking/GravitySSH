package syncclient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestMultiPinSPKIAndDefaultPinRotation(t *testing.T) {
	const gateKey = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	cert1 := makeSelfSignedCertForIP(t, "127.0.0.1")
	cert2 := makeSelfSignedCertForIP(t, "127.0.0.1")
	cert3 := makeSelfSignedCertForIP(t, "127.0.0.1")

	pin1 := spkiSHA256Hex(cert1.Leaf)
	pin2 := spkiSHA256Hex(cert2.Leaf)

	// 1. Fragment parsing with comma-separated multi-pin, sha256: prefix, uppercase & dedupe.
	cfg, err := NormalizeRelayConfig("127.0.0.1:54843#spki=SHA256:" + strings.ToUpper(pin1) + "," + pin2 + "," + pin1)
	if err != nil {
		t.Fatalf("NormalizeRelayConfig multi-pin failed: %v", err)
	}
	if cfg.TLSMode != RelayTLSModePinned || len(cfg.SPKIPins) != 2 || cfg.SPKIPins[0] != pin1 || cfg.SPKIPins[1] != pin2 {
		t.Fatalf("unexpected parsed multi-pin config: %+v", cfg)
	}

	// Reject empty/malformed/overflow pin lists.
	for _, bad := range []string{
		"https://127.0.0.1:54843#spki=",
		"https://127.0.0.1:54843#spki=" + pin1 + ",",
		"https://127.0.0.1:54843#spki=" + pin1 + ",not-hex",
		"https://127.0.0.1:54843#spki=" + strings.Repeat(pin1+",", maxSPKIPinsPerRelay) + pin2,
	} {
		if _, err := NormalizeRelayConfig(bad); err == nil {
			t.Fatalf("expected error for malformed multi-pin URL %q", bad)
		}
	}

	// 2. Live TLS 1.3 rotation: cert1 -> cert2 succeeds with zero downtime, cert3 fails.
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
	cli, err := NewClient(srvURL + "#spki=" + pin1 + "," + pin2 + "&gate=" + gateKey)
	if err != nil {
		t.Fatalf("NewClient multi-pin failed: %v", err)
	}
	if _, err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("Ping against cert1 failed: %v", err)
	}

	// Rotate relay certificate to backup pin2 -> must succeed without client change!
	cli.CloseIdleConnections()
	activeCert.Store(&cert2)
	if _, err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("Ping against rotated cert2 (backup pin) failed: %v", err)
	}

	// Rotate to unpinned cert3 -> must fail SPKI verification.
	cli.CloseIdleConnections()
	activeCert.Store(&cert3)
	if _, err := cli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "SPKI") {
		t.Fatalf("expected SPKI mismatch against cert3, got: %v", err)
	}

	// 3. DefaultRelaySPKIPins multi-pin configuration flows through NormalizeRelayConfig("").
	origPins := append([]string(nil), DefaultRelaySPKIPins...)
	DefaultRelaySPKIPins = []string{pin1, pin2}
	t.Cleanup(func() { DefaultRelaySPKIPins = origPins })
	defCfg, err := NormalizeRelayConfig("")
	if err != nil || defCfg.TLSMode != RelayTLSModePinned || len(defCfg.SPKIPins) != 2 || defCfg.SPKIPins[1] != pin2 {
		t.Fatalf("DefaultRelaySPKIPins not reflected in NormalizeRelayConfig: %+v err=%v", defCfg, err)
	}
}

func TestCustomRelayGateFragmentAndDefaultFallback(t *testing.T) {
	const customGate = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

	// Fragment validation for #gate=
	cfg, err := NormalizeRelayConfig("https://127.0.0.1:54843#gate=" + strings.ToUpper(customGate))
	if err != nil || cfg.AppGateKey != customGate || cfg.EffectiveGateKey() != customGate || cfg.TLSMode != RelayTLSModeSelfSignedIP {
		t.Fatalf("unexpected #gate= config: %+v err=%v", cfg, err)
	}
	defCfg, err := NormalizeRelayConfig("https://127.0.0.1:54843")
	if err != nil || defCfg.AppGateKey != "" || defCfg.EffectiveGateKey() != AppGateKey {
		t.Fatalf("expected default AppGateKey fallback when #gate= omitted: %+v err=%v", defCfg, err)
	}
	for _, bad := range []string{
		"https://127.0.0.1:54843#gate=short",
		"https://127.0.0.1:54843#gate=" + strings.Repeat("z", 64),
	} {
		if _, err := NormalizeRelayConfig(bad); err == nil {
			t.Fatalf("expected malformed #gate= URL %q to be rejected", bad)
		}
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != ClientHeaderValue ||
			r.Header.Get("X-GSSH-Sig") != SignRequest(customGate, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	pinSum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	pin := hex.EncodeToString(pinSum[:])

	// Without #gate=, default built-in AppGateKey fails with 404.
	defaultCli, err := NewClient(srv.URL + "#spki=" + pin)
	if err != nil {
		t.Fatalf("NewClient default gate failed: %v", err)
	}
	if _, err := defaultCli.Ping(context.Background()); err == nil {
		t.Fatal("expected built-in AppGateKey to fail against custom-gate relay")
	}

	// With #gate= alongside #spki=, client authenticates cleanly.
	customCli, err := NewClient(srv.URL + "#gate=" + customGate + "&spki=" + pin)
	if err != nil {
		t.Fatalf("NewClient custom gate failed: %v", err)
	}
	if _, err := customCli.Ping(context.Background()); err != nil {
		t.Fatalf("Ping with #gate=+#spki= failed: %v", err)
	}
}

func TestProtocolVersionRangeNegotiation(t *testing.T) {
	const gateKey = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	var (
		mode     atomic.Int32 // 0=compat(1..2), 1=legacy(no headers), 2=426, 3=disjoint(3..4)
		gotProto atomic.Value
	)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GSSH-Client") != ClientHeaderValue ||
			r.Header.Get("X-GSSH-Sig") != SignRequest(gateKey, r.Method, r.URL.Path, r.Header.Get("X-GSSH-TS"), body) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotProto.Store(r.Header.Get(WireProtocolHeader))
		switch mode.Load() {
		case 0:
			w.Header().Set(WireProtocolMinHeader, "1")
			w.Header().Set(WireProtocolMaxHeader, "2")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case 2:
			w.Header().Set(WireProtocolMinHeader, "3")
			w.Header().Set(WireProtocolMaxHeader, "4")
			w.WriteHeader(http.StatusUpgradeRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unsupported_protocol", "minVersion": 3, "maxVersion": 4})
		case 3:
			w.Header().Set(WireProtocolMinHeader, "3")
			w.Header().Set(WireProtocolMaxHeader, "4")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	cli, err := NewClientWithMode(srv.URL, gateKey, "", RelayTLSModeSelfSignedIP)
	if err != nil {
		t.Fatalf("NewClientWithMode failed: %v", err)
	}

	// Compatible range 1..2 succeeds and sends X-GSSH-Proto: 2.
	mode.Store(0)
	if _, err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("compatible range Ping failed: %v", err)
	}
	if p, _ := gotProto.Load().(string); p != "2" {
		t.Fatalf("expected X-GSSH-Proto=2, got %q", p)
	}

	// Legacy relay omitting X-GSSH-Proto-* headers remains 100% compatible.
	mode.Store(1)
	if _, err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("legacy relay Ping failed: %v", err)
	}

	// 426 Upgrade Required reports clear incompatibility error with server range.
	mode.Store(2)
	if _, err := cli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "3-4") {
		t.Fatalf("expected 426 protocol version mismatch error with range 3-4, got: %v", err)
	}

	// Disjoint 200 range headers (3..4) rejected cleanly.
	mode.Store(3)
	if _, err := cli.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "不兼容") {
		t.Fatalf("expected disjoint range error, got: %v", err)
	}

	// SyncBundle schemaVersion accepts 1 and 2, rejects 3.
	sendCode, recvCode := "K7M9-P4X2", "M8N2-Q5R7"
	reqID := strings.Repeat("a", 64)
	for _, ver := range []int{1, 2} {
		b := models.SyncBundle{SchemaVersion: ver, SourceDevice: "v-test"}
		enc, err := EncryptBundleDual(sendCode, recvCode, reqID, b)
		if err != nil {
			t.Fatalf("EncryptBundleDual v%d failed: %v", ver, err)
		}
		dec, err := DecryptBundleDual(sendCode, recvCode, reqID, enc)
		if err != nil || dec.SchemaVersion != ver {
			t.Fatalf("DecryptBundleDual v%d failed: got=%+v err=%v", ver, dec, err)
		}
	}
	badEnc, err := EncryptBundleDual(sendCode, recvCode, reqID, models.SyncBundle{SchemaVersion: 3, SourceDevice: "future"})
	if err != nil {
		t.Fatalf("EncryptBundleDual v3 failed: %v", err)
	}
	if _, err := DecryptBundleDual(sendCode, recvCode, reqID, badEnc); err == nil {
		t.Fatal("expected DecryptBundleDual to reject unsupported SchemaVersion=3")
	}
}

func TestPairRequest64KiBGunzipCap(t *testing.T) {
	sendCode := "K7M9-P4X2"
	recvCode := "M8N2-Q5R7"

	// Highly compressible >64 KiB PairRequestPayload (gzip compresses ~100 KiB of 'A' into <500 bytes!).
	bombReq := PairRequestPayload{
		ReceiverCode:     recvCode,
		ReceiverDevice:   strings.Repeat("A", MaxPairRequestPlainBytes+4096),
		ReceiverPlatform: "android",
		RequestedAt:      "2026-11-27T12:00:00Z",
	}
	encBomb, err := EncryptPairRequest(sendCode, bombReq)
	if err != nil {
		t.Fatalf("EncryptPairRequest bomb failed: %v", err)
	}
	if _, err := DecryptPairRequest(sendCode, encBomb); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("expected 64 KiB gunzip cap rejection on PairRequest, got: %v", err)
	}

	// SyncBundle >64 KiB (well within 16 MiB bundle cap) must still decrypt cleanly!
	reqID := strings.Repeat("b", 64)
	largeBundle := models.SyncBundle{
		SchemaVersion: 1,
		SourceDevice:  "LargeVault",
		Servers: []models.ServerConfig{
			{ID: "s1", Name: "LargeNotes", Host: "10.0.0.1", Notes: strings.Repeat("N", MaxPairRequestPlainBytes+16*1024)},
		},
	}
	encBundle, err := EncryptBundleDual(sendCode, recvCode, reqID, largeBundle)
	if err != nil {
		t.Fatalf("EncryptBundleDual large failed: %v", err)
	}
	decBundle, err := DecryptBundleDual(sendCode, recvCode, reqID, encBundle)
	if err != nil || len(decBundle.Servers) != 1 || len(decBundle.Servers[0].Notes) != len(largeBundle.Servers[0].Notes) {
		t.Fatalf("expected >64 KiB SyncBundle to remain allowed, got err=%v", err)
	}
}
