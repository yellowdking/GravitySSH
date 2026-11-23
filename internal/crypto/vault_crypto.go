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

var (
	vaultMagic = []byte{'G', 'S', 'V', '1'}

	ErrInvalidFormat = errors.New("invalid vault file format")
	ErrDecryptFailed = errors.New("failed to decrypt vault (unauthorized machine/user or corrupted data)")
)

const (
	masterKeySize = 32
	saltSize      = 32
	nonceSize     = 12
)

type KeyProtector interface {
	Protect(plaintext []byte) ([]byte, error)
	Unprotect(ciphertext []byte) ([]byte, error)
	EngineName() string
	IsMachineBound() bool
	IsUserBound() bool
}

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

func (e *EnvelopeCrypto) Seal(plaintext []byte) ([]byte, error) {
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

	dataKey, err := deriveDataKey(masterKey, salt)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(dataKey)

	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	headerLen := 4 + 4 + len(wrappedKey) + saltSize + nonceSize
	out := make([]byte, 0, headerLen+len(plaintext)+gcm.Overhead())
	out = append(out, vaultMagic...)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(wrappedKey)))
	out = append(out, lenBuf[:]...)
	out = append(out, wrappedKey...)
	out = append(out, salt...)
	out = append(out, nonce...)

	aad := out[:headerLen]
	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)
	out = append(out, ciphertext...)
	return out, nil
}

func (e *EnvelopeCrypto) Open(blob []byte) ([]byte, error) {
	minHeader := 4 + 4 + saltSize + nonceSize + 16
	if len(blob) < minHeader {
		return nil, ErrInvalidFormat
	}
	if !bytes.Equal(blob[:4], vaultMagic) {
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

	dataKey, err := deriveDataKey(masterKey, salt)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(dataKey)

	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	aad := blob[:headerLen]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	return plaintext, nil
}

func deriveDataKey(masterKey, salt []byte) ([]byte, error) {
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
