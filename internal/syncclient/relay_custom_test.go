package syncclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "protocol": "gssh-sync-v1"})
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
	if _, err := cli.Drop(context.Background(), hugePayload, 300); err == nil || !strings.Contains(err.Error(), "2 MiB") {
		t.Fatalf("expected 2 MiB payload pre-check error, got %v", err)
	}
}
