package syncclient

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
)

const (
	// CrockfordAlphabet is the 30-character unambiguous Base32 alphabet for gssh-sync-v1.
	CrockfordAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
	SyncCodeLength    = 8
	ChannelPrefix     = "gssh-sync-channel-v1:"
)

// GenerateCode generates an unbiased 8-char Crockford Base32 code formatted as XXXX-XXXX.
func GenerateCode() (string, error) {
	max := big.NewInt(int64(len(CrockfordAlphabet)))
	buf := make([]byte, SyncCodeLength)
	for i := 0; i < SyncCodeLength; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate sync code: %w", err)
		}
		buf[i] = CrockfordAlphabet[n.Int64()]
	}
	return FormatDisplayCode(string(buf)), nil
}

// GenerateHexToken32 returns 32 cryptographically random bytes encoded as 64 lowercase hex chars.
func GenerateHexToken32() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	defer clear(buf)
	return hex.EncodeToString(buf), nil
}

// FormatDisplayCode formats an 8-char canonical code as XXXX-XXXX.
func FormatDisplayCode(canonical string) string {
	if len(canonical) != SyncCodeLength {
		return canonical
	}
	return canonical[:4] + "-" + canonical[4:]
}

// FormatQRText formats a sync code into the canonical QR URI.
func FormatQRText(code string) (string, error) {
	canonical, err := CanonicalizeCode(code)
	if err != nil {
		return "", err
	}
	return "gssh-sync://v1?code=" + FormatDisplayCode(canonical), nil
}

// CanonicalizeCode extracts (if QR URI), uppercases, strips hyphens/spaces, and validates a sync code.
func CanonicalizeCode(input string) (string, error) {
	s := strings.TrimSpace(input)
	if strings.HasPrefix(strings.ToLower(s), "gssh-sync://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", errors.New("同步二维码链接格式无效")
		}
		s = u.Query().Get("code")
	}
	s = strings.ToUpper(s)
	var b strings.Builder
	b.Grow(SyncCodeLength)
	for _, r := range s {
		if r == '-' || r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			continue
		}
		if !strings.ContainsRune(CrockfordAlphabet, r) {
			return "", errors.New("同步码包含无效字符，请检查是否为 8 位有效同步码")
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) != SyncCodeLength {
		return "", errors.New("同步码长度必须为 8 位（格式 XXXX-XXXX）")
	}
	return out, nil
}

// DeriveChannelID computes hex(SHA256("gssh-sync-channel-v1:" + canonicalCode)).
func DeriveChannelID(canonicalCode string) string {
	sum := sha256.Sum256([]byte(ChannelPrefix + canonicalCode))
	return hex.EncodeToString(sum[:])
}

// DeriveDualSecret returns "canonicalSend:canonicalRecv" for gssh-sync-v2 dual-key derivation.
func DeriveDualSecret(sendCode, recvCode string) (string, error) {
	canonicalSend, err := CanonicalizeCode(sendCode)
	if err != nil {
		return "", err
	}
	canonicalRecv, err := CanonicalizeCode(recvCode)
	if err != nil {
		return "", err
	}
	return canonicalSend + ":" + canonicalRecv, nil
}

// DeriveDualAAD returns "channelID:requestID:canonicalRecv" used as AES-GCM AAD in gssh-sync-v2.
func DeriveDualAAD(channelID, requestID, canonicalRecv string) string {
	return channelID + ":" + requestID + ":" + canonicalRecv
}
