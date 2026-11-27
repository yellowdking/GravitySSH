package syncclient

import (
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	// DefaultRelayURL is the built-in zero-knowledge GravitySSH relay endpoint.
	DefaultRelayURL = "https://43.155.231.14:54843"
	// DefaultRelayHost is used to determine whether built-in SPKI pinning applies.
	DefaultRelayHost = "43.155.231.14:54843"
	// ClientHeaderValue identifies this protocol client to the relay app gate.
	ClientHeaderValue = "GravitySSH/1"
	// DefaultTTLSeconds is the default 5-minute ephemeral drop lifetime.
	DefaultTTLSeconds = 300
	// ProtocolVersion is the emitted SyncBundle schemaVersion for cross-platform compatibility.
	ProtocolVersion = 1
	// MinBundleSchemaVersion and MaxBundleSchemaVersion define the accepted SyncBundle schema range.
	MinBundleSchemaVersion = 1
	MaxBundleSchemaVersion = 2
	// WireProtocolMinVersion / WireProtocolMaxVersion define the compatible relay HTTP wire protocol range.
	WireProtocolMinVersion = 1
	WireProtocolMaxVersion = 2
	// WireProtocolVersion is the preferred relay wire protocol advertised via X-GSSH-Proto.
	WireProtocolVersion = 2
	// WireProtocolHeader and server-advertised range headers for relay protocol negotiation.
	WireProtocolHeader    = "X-GSSH-Proto"
	WireProtocolMinHeader = "X-GSSH-Proto-Min"
	WireProtocolMaxHeader = "X-GSSH-Proto-Max"
	// maxSPKIPinsPerRelay bounds explicit comma-separated SPKI pins in #spki=.
	maxSPKIPinsPerRelay = 8
)

// RelayTLSMode controls TLS certificate verification for the sync relay.
type RelayTLSMode int

const (
	// RelayTLSModePinned enforces strict SPKI SHA-256 pinning (built-in default relay).
	RelayTLSModePinned RelayTLSMode = iota
	// RelayTLSModeSelfSignedIP enforces TLS 1.3 and validates non-empty, time-valid leaf cert
	// for user-deployed raw IP VPS relays (e.g., docker run bbplin/sshsync).
	RelayTLSModeSelfSignedIP
	// RelayTLSModeSystemCA enforces standard system root CA verification over TLS 1.3.
	RelayTLSModeSystemCA
)

// PinnedSPKISHA256, DefaultRelaySPKIPins, and AppGateKey are populated from the deployed relay config.
var (
	PinnedSPKISHA256     = "ca1a5f441de4962b6269449325ded3a284a0a8a9d8fd3d995ffcb218c8eb7575"
	DefaultRelaySPKIPins = []string{PinnedSPKISHA256}
	AppGateKey           = "fb6f39e339dffe1141537e5e8154491cce0044c7bc2ecb26b17828571b9d5aae"
)

// RelayConfig holds normalized connection, TLS pinning, and optional App Gate override settings.
type RelayConfig struct {
	URL        string
	TLSMode    RelayTLSMode
	SPKIPins   []string
	AppGateKey string
}

// EffectiveGateKey returns the custom #gate= key when set, or the built-in AppGateKey.
func (c RelayConfig) EffectiveGateKey() string {
	if strings.TrimSpace(c.AppGateKey) != "" {
		return c.AppGateKey
	}
	return AppGateKey
}

func defaultRelaySPKIPins() []string {
	if len(DefaultRelaySPKIPins) > 0 {
		if pins, err := ParseSPKIPinList(strings.Join(DefaultRelaySPKIPins, ",")); err == nil && len(pins) > 0 {
			return pins
		}
	}
	if pin := strings.ToLower(strings.TrimSpace(PinnedSPKISHA256)); pin != "" {
		return []string{pin}
	}
	return nil
}

// ParseSPKIPinList parses a comma-separated list of 64-char hex SPKI SHA-256 pins
// (optionally prefixed with "sha256:"), deduplicating while preserving order.
func ParseSPKIPinList(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("内置中继 SPKI 指纹未配置")
	}
	parts := strings.Split(trimmed, ",")
	if len(parts) > maxSPKIPinsPerRelay {
		return nil, errors.New("中继 SPKI 指纹数量超出上限（最多 8 个）")
	}
	pins := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, item := range parts {
		pin := strings.ToLower(strings.TrimSpace(item))
		pin = strings.TrimPrefix(pin, "sha256:")
		if len(pin) != 64 {
			return nil, errors.New("中继 SPKI 指纹格式无效（应为 64 位 SHA-256 十六进制字符串）")
		}
		if _, err := hex.DecodeString(pin); err != nil {
			return nil, errors.New("中继 SPKI 指纹格式无效（应为 64 位 SHA-256 十六进制字符串）")
		}
		if _, dup := seen[pin]; !dup {
			seen[pin] = struct{}{}
			pins = append(pins, pin)
		}
	}
	if len(pins) == 0 {
		return nil, errors.New("中继 SPKI 指纹格式无效（应为 64 位 SHA-256 十六进制字符串）")
	}
	return pins, nil
}

func parseRelayFragment(frag string) (pins []string, gateKey string, err error) {
	trimmed := strings.TrimSpace(frag)
	if trimmed == "" {
		return nil, "", nil
	}
	for _, part := range strings.Split(trimmed, "&") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		switch {
		case strings.EqualFold(key, "spki"):
			parsedPins, pErr := ParseSPKIPinList(v)
			if pErr != nil {
				return nil, "", pErr
			}
			pins = parsedPins
		case strings.EqualFold(key, "gate"):
			g := strings.ToLower(strings.TrimSpace(v))
			if len(g) != 64 {
				return nil, "", errors.New("自定义中继 App Gate 密钥格式无效（应为 64 位十六进制字符串）")
			}
			if _, dErr := hex.DecodeString(g); dErr != nil {
				return nil, "", errors.New("自定义中继 App Gate 密钥格式无效（应为 64 位十六进制字符串）")
			}
			gateKey = g
		}
	}
	return pins, gateKey, nil
}

// IsRawIPHost reports whether rawURL or host[:port] targets a literal IPv4/IPv6 address.
func IsRawIPHost(rawURL string) bool {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return false
	}
	host := s
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return false
		}
		host = u.Hostname()
	} else if h, _, err := net.SplitHostPort(s); err == nil {
		host = h
	} else {
		host = strings.Trim(s, "[]")
	}
	return net.ParseIP(host) != nil
}

// NormalizeRelayConfig validates and normalizes a relay URL or bare host:port / IP:port,
// extracting optional `#spki=<pin1>,<pin2>` and `#gate=<64-hex>` fragments.
func NormalizeRelayConfig(raw string) (RelayConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return RelayConfig{
			URL:      DefaultRelayURL,
			TLSMode:  RelayTLSModePinned,
			SPKIPins: defaultRelaySPKIPins(),
		}, nil
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") {
		return RelayConfig{}, errors.New("中继服务器仅支持 HTTPS（TLS 1.3），不支持明文 http://")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return RelayConfig{}, errors.New("中继服务器地址格式无效")
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.Hostname() == "" {
		return RelayConfig{}, errors.New("中继服务器地址必须使用有效的 HTTPS 地址或 IP:端口")
	}
	if u.User != nil {
		return RelayConfig{}, errors.New("中继服务器地址不支持包含用户名或密码")
	}
	if strings.ContainsAny(u.Host, " \t\r\n") {
		return RelayConfig{}, errors.New("中继服务器地址包含无效空格")
	}
	if strings.HasSuffix(u.Host, ":") {
		return RelayConfig{}, errors.New("中继服务器端口号无效（范围应为 1-65535）")
	}
	if portStr := u.Port(); portStr != "" {
		portNum, err := strconv.Atoi(portStr)
		if err != nil || portNum < 1 || portNum > 65535 {
			return RelayConfig{}, errors.New("中继服务器端口号无效（范围应为 1-65535）")
		}
	}
	explicitPins, customGate, err := parseRelayFragment(u.Fragment)
	if err != nil {
		return RelayConfig{}, err
	}

	u.Scheme = "https"
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""

	if len(explicitPins) > 0 {
		return RelayConfig{
			URL:        u.String(),
			TLSMode:    RelayTLSModePinned,
			SPKIPins:   explicitPins,
			AppGateKey: customGate,
		}, nil
	}
	switch {
	case strings.EqualFold(u.Host, DefaultRelayHost):
		return RelayConfig{
			URL:        u.String(),
			TLSMode:    RelayTLSModePinned,
			SPKIPins:   defaultRelaySPKIPins(),
			AppGateKey: customGate,
		}, nil
	case IsRawIPHost(u.Host):
		return RelayConfig{
			URL:        u.String(),
			TLSMode:    RelayTLSModeSelfSignedIP,
			AppGateKey: customGate,
		}, nil
	default:
		return RelayConfig{
			URL:        u.String(),
			TLSMode:    RelayTLSModeSystemCA,
			AppGateKey: customGate,
		}, nil
	}
}

// NormalizeRelayURL validates and normalizes a relay URL or bare host:port / IP:port.
// Plain http:// is rejected; bare addresses automatically receive https://.
func NormalizeRelayURL(raw string) (string, RelayTLSMode, error) {
	cfg, err := NormalizeRelayConfig(raw)
	if err != nil {
		return "", 0, err
	}
	return cfg.URL, cfg.TLSMode, nil
}

// NormalizeRelayURLWithPin validates and normalizes a relay URL and extracts any
// explicit SPKI SHA-256 pin(s) from a `#spki=<64-hex>[,<64-hex>...]` URL fragment.
func NormalizeRelayURLWithPin(raw string) (string, RelayTLSMode, string, error) {
	cfg, err := NormalizeRelayConfig(raw)
	if err != nil {
		return "", 0, "", err
	}
	return cfg.URL, cfg.TLSMode, strings.Join(cfg.SPKIPins, ","), nil
}
