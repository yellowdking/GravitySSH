package sshkey

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh"
)

var (
	ErrPassphraseRequired = errors.New("SSH 私钥已加密，请输入私钥密码 (passphrase)")
	ErrInvalidKey         = errors.New("无法解析 SSH 私钥格式")
)

// ParsedKeyInfo holds non-sensitive metadata extracted from a valid private key.
type ParsedKeyInfo struct {
	KeyType     string
	Fingerprint string
	PublicKey   string
	Signer      ssh.Signer
}

// SanitizeComment removes control characters (including NUL, CR, LF, tabs, and ANSI controls)
// from an SSH key comment so it cannot inject extra lines or control sequences into
// remote authorized_keys files.
func SanitizeComment(comment string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return -1
		}
		return r
	}, comment)
	return strings.TrimSpace(cleaned)
}

// ParsePrivateKey validates and parses a private key PEM (with optional passphrase).
func ParsePrivateKey(pemContent string, passphrase string) (*ParsedKeyInfo, error) {
	trimmed := strings.TrimSpace(pemContent)
	if trimmed == "" {
		return nil, ErrInvalidKey
	}

	raw := []byte(trimmed)
	defer clear(raw)

	var (
		signer ssh.Signer
		err    error
	)

	if passphrase != "" {
		passBytes := []byte(passphrase)
		defer clear(passBytes)
		signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, passBytes)
	} else {
		signer, err = ssh.ParsePrivateKey(raw)
		if err != nil {
			var passErr *ssh.PassphraseMissingError
			if errors.As(err, &passErr) {
				return nil, ErrPassphraseRequired
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}

	pub := signer.PublicKey()
	return &ParsedKeyInfo{
		KeyType:     pub.Type(),
		Fingerprint: ssh.FingerprintSHA256(pub),
		PublicKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
		Signer:      signer,
	}, nil
}

// GenerateKeyPair generates a new Ed25519 or RSA (4096-bit) key pair in OpenSSH format.
// Generated raw private key bytes are zeroed before returning, so info.Signer is nil;
// callers needing an active ssh.Signer should parse privatePEM via ParsePrivateKey.
func GenerateKeyPair(algorithm string, comment string, passphrase string) (privatePEM string, info *ParsedKeyInfo, err error) {
	comment = SanitizeComment(comment)

	var (
		priv   any
		pubKey crypto.PublicKey
	)
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case "", "ed25519":
		pub, privKey, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return "", nil, fmt.Errorf("generate ed25519 key: %w", genErr)
		}
		defer clear(privKey)
		priv = privKey
		pubKey = pub
	case "rsa", "rsa4096":
		privKey, genErr := rsa.GenerateKey(rand.Reader, 4096)
		if genErr != nil {
			return "", nil, fmt.Errorf("generate rsa key: %w", genErr)
		}
		priv = privKey
		pubKey = &privKey.PublicKey
	case "rsa2048":
		privKey, genErr := rsa.GenerateKey(rand.Reader, 2048)
		if genErr != nil {
			return "", nil, fmt.Errorf("generate rsa2048 key: %w", genErr)
		}
		priv = privKey
		pubKey = &privKey.PublicKey
	default:
		return "", nil, fmt.Errorf("unsupported key algorithm: %s", algorithm)
	}

	var block *pem.Block
	if passphrase != "" {
		passBytes := []byte(passphrase)
		defer clear(passBytes)
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, passBytes)
	} else {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return "", nil, fmt.Errorf("marshal openssh private key: %w", err)
	}
	defer clear(block.Bytes)

	pemBytes := pem.EncodeToMemory(block)
	defer clear(pemBytes)
	pemStr := string(pemBytes)

	pub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", nil, fmt.Errorf("create ssh public key: %w", err)
	}
	pubStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		pubStr = pubStr + " " + comment
	}
	parsed := &ParsedKeyInfo{
		KeyType:     pub.Type(),
		Fingerprint: ssh.FingerprintSHA256(pub),
		PublicKey:   pubStr,
	}
	return pemStr, parsed, nil
}
