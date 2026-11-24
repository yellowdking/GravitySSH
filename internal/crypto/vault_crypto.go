package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	ModeDPAPI         byte = 0x01
	ModePasswordDPAPI byte = 0x02

	masterKeySize = 32
	saltSize      = 32
	pwSaltSize    = 16
	nonceSize     = 12
)

var (
	vaultMagicV1 = []byte{'G', 'S', 'V', '1'}
	vaultMagicV2 = []byte{'G', 'S', 'V', '2'}

	ErrInvalidFormat    = errors.New("invalid vault file format")
	ErrDecryptFailed    = errors.New("failed to decrypt vault (unauthorized machine/user or corrupted data)")
	ErrPasswordRequired = errors.New("vault is protected by a user password")
	ErrWrongPassword    = errors.New("incorrect vault password or corrupted data")
)

// KeyProtector seals and unseals the vault's master encryption key using OS-level protection (Windows DPAPI).
type KeyProtector interface {
	Protect(plaintext []byte) ([]byte, error)
	Unprotect(ciphertext []byte) ([]byte, error)
	EngineName() string
	IsMachineBound() bool
	IsUserBound() bool
}

// EnvelopeCrypto encrypts and decrypts vault payloads using GSV2 envelope encryption
// while maintaining 100% backward compatibility with legacy GSV1 files.
type EnvelopeCrypto struct {
	protector KeyProtector
}

func NewEnvelopeCrypto(protector KeyProtector) *EnvelopeCrypto {
	if protector == nil {
		protector = NewDefaultProtector()
	}
	return &EnvelopeCrypto{protector: protector}
}

func (e *EnvelopeCrypto) Protector() KeyProtector {
	return e.protector
}

// Seal encrypts plaintext bytes into GSV2 Mode 0x01 (DPAPI-only, zero-prompt).
func (e *EnvelopeCrypto) Seal(plaintext []byte) ([]byte, error) {
	return e.SealWithPassword(plaintext, "")
}

// Open decrypts a GSV1 or GSV2 (Mode 0x01) binary envelope back into plaintext bytes.
func (e *EnvelopeCrypto) Open(blob []byte) ([]byte, error) {
	return e.OpenWithPassword(blob, "")
}

// IsPasswordProtected inspects the vault envelope header to determine if a user password is required.
func (e *EnvelopeCrypto) IsPasswordProtected(blob []byte) (bool, error) {
	if len(blob) < 4 {
		return false, ErrInvalidFormat
	}
	if bytes.Equal(blob[:4], vaultMagicV1) {
		return false, nil
	}
	if bytes.Equal(blob[:4], vaultMagicV2) {
		if len(blob) < 6 {
			return false, ErrInvalidFormat
		}
		switch blob[4] {
		case ModeDPAPI:
			return false, nil
		case ModePasswordDPAPI:
			return true, nil
		default:
			return false, ErrInvalidFormat
		}
	}
	return false, ErrInvalidFormat
}

// OpenWithPassword decrypts a GSV1 or GSV2 (Mode 0x01 / Mode 0x02) binary envelope.
func (e *EnvelopeCrypto) OpenWithPassword(blob []byte, password string) ([]byte, error) {
	if len(blob) < 4 {
		return nil, ErrInvalidFormat
	}
	if bytes.Equal(blob[:4], vaultMagicV1) {
		return e.openV1(blob)
	}
	if bytes.Equal(blob[:4], vaultMagicV2) {
		return e.openV2(blob, password)
	}
	return nil, ErrInvalidFormat
}

func (e *EnvelopeCrypto) sealV1(plaintext []byte) ([]byte, error) {
	masterKey := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, masterKey); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	defer zeroBytes(masterKey)

	wrappedKey, err := e.protector.Protect(masterKey)
	if err != nil {
		return nil, fmt.Errorf("protect master key: %w", err)
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	dataKey, err := deriveDataKeyV1(masterKey, salt)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(dataKey)

	gcm, err := newAESGCM(dataKey)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	headerLen := 4 + 4 + len(wrappedKey) + saltSize + nonceSize
	out := make([]byte, 0, headerLen+len(plaintext)+gcm.Overhead())
	out = append(out, vaultMagicV1...)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(wrappedKey)))
	out = append(out, lenBuf[:]...)
	out = append(out, wrappedKey...)
	out = append(out, salt...)
	out = append(out, nonce...)

	ciphertext := gcm.Seal(nil, nonce, plaintext, out[:headerLen])
	return append(out, ciphertext...), nil
}

func (e *EnvelopeCrypto) openV1(blob []byte) ([]byte, error) {
	minHeader := 4 + 4 + saltSize + nonceSize + 16
	if len(blob) < minHeader {
		return nil, ErrInvalidFormat
	}
	wrappedKeyLen := int(binary.BigEndian.Uint32(blob[4:8]))
	headerLen := 4 + 4 + wrappedKeyLen + saltSize + nonceSize
	if wrappedKeyLen <= 0 || headerLen+16 > len(blob) {
		return nil, ErrInvalidFormat
	}

	offset := 8
	wrappedKey := blob[offset : offset+wrappedKeyLen]
	offset += wrappedKeyLen
	salt := blob[offset : offset+saltSize]
	offset += saltSize
	nonce := blob[offset : offset+nonceSize]
	offset += nonceSize
	ciphertext := blob[offset:]

	masterKey, err := e.protector.Unprotect(wrappedKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	defer zeroBytes(masterKey)
	if len(masterKey) != masterKeySize {
		return nil, ErrDecryptFailed
	}

	dataKey, err := deriveDataKeyV1(masterKey, salt)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(dataKey)

	gcm, err := newAESGCM(dataKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, blob[:headerLen])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	return plaintext, nil
}

func newAESGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func deriveDataKeyV1(masterKey, salt []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, masterKey, salt, []byte("GravitySSH-Vault-AES256GCM-DataKey-v1"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
