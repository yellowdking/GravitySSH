//go:build !windows

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"

	"golang.org/x/crypto/hkdf"
)

type fallbackProtector struct {
	key []byte
}

// NewDefaultProtector provides a host-bound AES-GCM fallback for non-Windows test runs.
func NewDefaultProtector() KeyProtector {
	host, _ := os.Hostname()
	user := os.Getenv("USER")
	secret := []byte("GravitySSH::NonWinFallback::v1::9f3e7b1c4a82d605")
	salt := []byte(host + ":" + user)
	r := hkdf.New(sha256.New, secret, salt, []byte("GravitySSH-Fallback-KeyWrap"))
	key := make([]byte, 32)
	_, _ = io.ReadFull(r, key)
	return &fallbackProtector{key: key}
}

func (p *fallbackProtector) Protect(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, []byte("GSV1-WRAP")), nil
}

func (p *fallbackProtector) Unprotect(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("wrapped key too short")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	return gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], []byte("GSV1-WRAP"))
}

func (p *fallbackProtector) EngineName() string {
	return "Host+User HKDF KeyWrap + AES-256-GCM (Non-Windows Fallback)"
}

func (p *fallbackProtector) IsMachineBound() bool { return true }
func (p *fallbackProtector) IsUserBound() bool    { return true }
