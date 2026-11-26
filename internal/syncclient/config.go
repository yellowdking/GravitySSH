package syncclient

import (
	"errors"
	"net/url"
	"strings"
)

const (
	// DefaultRelayURL is the built-in zero-knowledge GravitySSH relay endpoint.
	DefaultRelayURL = "https://107.172.98.169:54843"
	// DefaultRelayHost is used to determine whether built-in SPKI pinning applies.
	DefaultRelayHost = "107.172.98.169:54843"
	// ClientHeaderValue identifies this protocol client to the relay app gate.
	ClientHeaderValue = "GravitySSH/1"
	// DefaultTTLSeconds is the default 5-minute ephemeral drop lifetime.
	DefaultTTLSeconds = 300
	// ProtocolVersion is the SyncBundle schemaVersion supported by gssh-sync-v1.
	ProtocolVersion = 1
)

// PinnedSPKISHA256 and AppGateKey are populated from the deployed relay config.
var (
	PinnedSPKISHA256 = "8d02a67e23e9f5af5922a8e700a6309dfa4ce335baca7125452fe22f4e5e29ba"
	AppGateKey       = "fb6f39e339dffe1141537e5e8154491cce0044c7bc2ecb26b17828571b9d5aae"
)

// NormalizeRelayURL validates and normalizes a relay URL, returning whether
// the URL targets the default pinned relay host.
func NormalizeRelayURL(raw string) (string, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultRelayURL, true, nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", false, errors.New("中继服务器地址格式无效")
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return "", false, errors.New("中继服务器地址必须使用有效的 HTTPS URL")
	}
	u.Scheme = "https"
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	isDefault := strings.EqualFold(u.Host, DefaultRelayHost)
	return u.String(), isDefault, nil
}
