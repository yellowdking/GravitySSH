package syncclient

import (
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
	// ProtocolVersion is the SyncBundle schemaVersion supported by gssh-sync-v1.
	ProtocolVersion = 1
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

// PinnedSPKISHA256 and AppGateKey are populated from the deployed relay config.
var (
	PinnedSPKISHA256 = "ca1a5f441de4962b6269449325ded3a284a0a8a9d8fd3d995ffcb218c8eb7575"
	AppGateKey       = "fb6f39e339dffe1141537e5e8154491cce0044c7bc2ecb26b17828571b9d5aae"
)

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

// NormalizeRelayURL validates and normalizes a relay URL or bare host:port / IP:port.
// Plain http:// is rejected; bare addresses automatically receive https://.
func NormalizeRelayURL(raw string) (string, RelayTLSMode, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultRelayURL, RelayTLSModePinned, nil
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") {
		return "", 0, errors.New("中继服务器仅支持 HTTPS（TLS 1.3），不支持明文 http://")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", 0, errors.New("中继服务器地址格式无效")
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.Hostname() == "" {
		return "", 0, errors.New("中继服务器地址必须使用有效的 HTTPS 地址或 IP:端口")
	}
	if strings.ContainsAny(u.Host, " \t\r\n") {
		return "", 0, errors.New("中继服务器地址包含无效空格")
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", 0, errors.New("中继服务器端口号无效（范围应为 1-65535）")
	}
	if portStr := u.Port(); portStr != "" {
		portNum, err := strconv.Atoi(portStr)
		if err != nil || portNum < 1 || portNum > 65535 {
			return "", 0, errors.New("中继服务器端口号无效（范围应为 1-65535）")
		}
	}
	u.Scheme = "https"
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""

	switch {
	case strings.EqualFold(u.Host, DefaultRelayHost):
		return u.String(), RelayTLSModePinned, nil
	case IsRawIPHost(u.Host):
		return u.String(), RelayTLSModeSelfSignedIP, nil
	default:
		return u.String(), RelayTLSModeSystemCA, nil
	}
}
