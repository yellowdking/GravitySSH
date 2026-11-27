package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

// DerivePasswordKey derives the 32-byte Argon2id password key for GSV2 Mode 0x02.
// If pwSalt is nil/empty, a fresh 16-byte random salt is generated and returned.
func DerivePasswordKey(password string, pwSalt []byte) (pwKey, saltOut []byte, err error) {
	if password == "" {
		return nil, nil, errors.New("password must not be empty")
	}
	if len(pwSalt) == 0 {
		saltOut = make([]byte, pwSaltSize)
		if _, err := io.ReadFull(rand.Reader, saltOut); err != nil {
			return nil, nil, fmt.Errorf("generate password salt: %w", err)
		}
	} else {
		if len(pwSalt) != pwSaltSize {
			return nil, nil, ErrInvalidFormat
		}
		saltOut = append([]byte(nil), pwSalt...)
	}
	pwBytes := []byte(password)
	defer ZeroBytes(pwBytes)
	pwKey = argon2.IDKey(pwBytes, saltOut, 2, 64*1024, 2, masterKeySize)
	return pwKey, saltOut, nil
}

// SealWithPassword encrypts plaintext into GSV2 format:
//   - Empty password -> Mode 0x01 (DPAPI-only, zero-prompt on same PC/user)
//   - Non-empty password -> Mode 0x02 (Argon2id user password + DPAPI dual-layer)
//
// Binary layout:
//
//	Magic(4:"GSV2") | Mode(1) | Flags(1) | WrappedKeyLen(4) | WrappedKey |
//	PwSaltLen(1) | PwSalt | Salt(32) | Nonce(12) | Ciphertext+GCMTag
func (e *EnvelopeCrypto) SealWithPassword(plaintext []byte, password string) ([]byte, error) {
	if password == "" {
		return e.SealV2WithCachedKey(plaintext, nil, nil)
	}
	pwKey, pwSalt, err := DerivePasswordKey(password, nil)
	if err != nil {
		return nil, err
	}
	defer ZeroBytes(pwKey)
	return e.SealV2WithCachedKey(plaintext, pwSalt, pwKey)
}

// SealV2WithCachedKey encrypts plaintext into GSV2 format using a pre-derived
// (pwSalt, pwKey) pair for Mode 0x02, or Mode 0x01 when both are empty.
// Every call still generates a fresh random masterKey, HKDF salt, and AES-GCM nonce.
func (e *EnvelopeCrypto) SealV2WithCachedKey(plaintext, pwSalt, pwKey []byte) ([]byte, error) {
	mode := ModeDPAPI
	if len(pwSalt) > 0 || len(pwKey) > 0 {
		if len(pwSalt) != pwSaltSize || len(pwKey) != masterKeySize {
			return nil, ErrInvalidFormat
		}
		mode = ModePasswordDPAPI
	}

	masterKey := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, masterKey); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	defer ZeroBytes(masterKey)

	wrappedKey, err := e.protector.Protect(masterKey)
	if err != nil {
		return nil, fmt.Errorf("protect master key: %w", err)
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	keyMaterial := masterKey
	if mode == ModePasswordDPAPI {
		combined := xorKeyMaterial(masterKey, pwKey)
		defer ZeroBytes(combined)
		keyMaterial = combined
	}

	dataKey, err := deriveDataKeyV2(keyMaterial, salt, mode)
	if err != nil {
		return nil, err
	}
	defer ZeroBytes(dataKey)

	gcm, err := newAESGCM(dataKey)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	headerLen := 4 + 1 + 1 + 4 + len(wrappedKey) + 1 + len(pwSalt) + saltSize + nonceSize
	out := make([]byte, 0, headerLen+len(plaintext)+gcm.Overhead())
	out = append(out, vaultMagicV2...)
	out = append(out, mode, 0x00)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(wrappedKey)))
	out = append(out, lenBuf[:]...)
	out = append(out, wrappedKey...)
	out = append(out, byte(len(pwSalt)))
	out = append(out, pwSalt...)
	out = append(out, salt...)
	out = append(out, nonce...)

	ciphertext := gcm.Seal(nil, nonce, plaintext, out[:headerLen])
	return append(out, ciphertext...), nil
}

func (e *EnvelopeCrypto) openV2WithKey(blob []byte, password string) (plaintext, pwSaltOut, pwKeyOut []byte, err error) {
	// Minimum size: Magic(4)+Mode(1)+Flags(1)+WKeyLen(4)+PwSaltLen(1)+Salt(32)+Nonce(12)+Tag(16)
	minSize := 4 + 1 + 1 + 4 + 1 + saltSize + nonceSize + 16
	if len(blob) < minSize {
		return nil, nil, nil, ErrInvalidFormat
	}

	mode := blob[4]
	if mode != ModeDPAPI && mode != ModePasswordDPAPI {
		return nil, nil, nil, ErrInvalidFormat
	}

	wrappedKeyLen := int(binary.BigEndian.Uint32(blob[6:10]))
	if wrappedKeyLen <= 0 || 10+wrappedKeyLen+1 > len(blob) {
		return nil, nil, nil, ErrInvalidFormat
	}

	offset := 10
	wrappedKey := blob[offset : offset+wrappedKeyLen]
	offset += wrappedKeyLen

	pwSaltLen := int(blob[offset])
	offset++
	headerLen := offset + pwSaltLen + saltSize + nonceSize
	if headerLen+16 > len(blob) {
		return nil, nil, nil, ErrInvalidFormat
	}

	pwSalt := blob[offset : offset+pwSaltLen]
	offset += pwSaltLen
	salt := blob[offset : offset+saltSize]
	offset += saltSize
	nonce := blob[offset : offset+nonceSize]
	offset += nonceSize
	ciphertext := blob[offset:]

	if mode == ModePasswordDPAPI {
		if pwSaltLen != pwSaltSize {
			return nil, nil, nil, ErrInvalidFormat
		}
		if password == "" {
			return nil, nil, nil, ErrPasswordRequired
		}
	}

	masterKey, err := e.protector.Unprotect(wrappedKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	defer ZeroBytes(masterKey)
	if len(masterKey) != masterKeySize {
		return nil, nil, nil, ErrDecryptFailed
	}

	keyMaterial := masterKey
	var pwKey []byte
	keepPWKey := false
	defer func() {
		if !keepPWKey {
			ZeroBytes(pwKey)
		}
	}()

	if mode == ModePasswordDPAPI {
		pwBytes := []byte(password)
		pwKey = argon2.IDKey(pwBytes, pwSalt, 2, 64*1024, 2, masterKeySize)
		ZeroBytes(pwBytes)
		combined := xorKeyMaterial(masterKey, pwKey)
		defer ZeroBytes(combined)
		keyMaterial = combined
	}

	dataKey, err := deriveDataKeyV2(keyMaterial, salt, mode)
	if err != nil {
		return nil, nil, nil, err
	}
	defer ZeroBytes(dataKey)

	gcm, err := newAESGCM(dataKey)
	if err != nil {
		return nil, nil, nil, err
	}

	plaintext, err = gcm.Open(nil, nonce, ciphertext, blob[:headerLen])
	if err != nil {
		if mode == ModePasswordDPAPI {
			return nil, nil, nil, fmt.Errorf("%w: %v", ErrWrongPassword, err)
		}
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	if mode == ModePasswordDPAPI {
		keepPWKey = true
		pwSaltOut = append([]byte(nil), pwSalt...)
		pwKeyOut = pwKey
	}
	return plaintext, pwSaltOut, pwKeyOut, nil
}

func xorKeyMaterial(masterKey, pwKey []byte) []byte {
	combined := make([]byte, masterKeySize)
	for i := range combined {
		combined[i] = masterKey[i] ^ pwKey[i]
	}
	return combined
}

func deriveDataKeyV2(keyMaterial, salt []byte, mode byte) ([]byte, error) {
	info := []byte("GravitySSH-Vault-AES256GCM-DataKey-v2")
	if mode == ModePasswordDPAPI {
		info = []byte("GravitySSH-Vault-AES256GCM-DataKey-v2-PW")
	}
	r := hkdf.New(sha256.New, keyMaterial, salt, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}
