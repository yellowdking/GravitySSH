package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

// SealWithPassword encrypts plaintext into GSV2 format:
//   - Empty password -> Mode 0x01 (DPAPI-only, zero-prompt on same PC/user)
//   - Non-empty password -> Mode 0x02 (Argon2id user password + DPAPI dual-layer)
//
// Binary layout:
//
//	Magic(4:"GSV2") | Mode(1) | Flags(1) | WrappedKeyLen(4) | WrappedKey |
//	PwSaltLen(1) | PwSalt | Salt(32) | Nonce(12) | Ciphertext+GCMTag
func (e *EnvelopeCrypto) SealWithPassword(plaintext []byte, password string) ([]byte, error) {
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

	mode := ModeDPAPI
	var pwSalt []byte
	keyMaterial := masterKey

	if password != "" {
		mode = ModePasswordDPAPI
		pwSalt = make([]byte, pwSaltSize)
		if _, err := io.ReadFull(rand.Reader, pwSalt); err != nil {
			return nil, fmt.Errorf("generate password salt: %w", err)
		}
		combined := combineMasterAndPassword(masterKey, password, pwSalt)
		defer zeroBytes(combined)
		keyMaterial = combined
	}

	dataKey, err := deriveDataKeyV2(keyMaterial, salt, mode)
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

func (e *EnvelopeCrypto) openV2(blob []byte, password string) ([]byte, error) {
	// Minimum size: Magic(4)+Mode(1)+Flags(1)+WKeyLen(4)+PwSaltLen(1)+Salt(32)+Nonce(12)+Tag(16)
	minSize := 4 + 1 + 1 + 4 + 1 + saltSize + nonceSize + 16
	if len(blob) < minSize {
		return nil, ErrInvalidFormat
	}

	mode := blob[4]
	if mode != ModeDPAPI && mode != ModePasswordDPAPI {
		return nil, ErrInvalidFormat
	}

	wrappedKeyLen := int(binary.BigEndian.Uint32(blob[6:10]))
	if wrappedKeyLen <= 0 || 10+wrappedKeyLen+1 > len(blob) {
		return nil, ErrInvalidFormat
	}

	offset := 10
	wrappedKey := blob[offset : offset+wrappedKeyLen]
	offset += wrappedKeyLen

	pwSaltLen := int(blob[offset])
	offset++
	headerLen := offset + pwSaltLen + saltSize + nonceSize
	if headerLen+16 > len(blob) {
		return nil, ErrInvalidFormat
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
			return nil, ErrInvalidFormat
		}
		if password == "" {
			return nil, ErrPasswordRequired
		}
	}

	masterKey, err := e.protector.Unprotect(wrappedKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	defer zeroBytes(masterKey)
	if len(masterKey) != masterKeySize {
		return nil, ErrDecryptFailed
	}

	keyMaterial := masterKey
	if mode == ModePasswordDPAPI {
		combined := combineMasterAndPassword(masterKey, password, pwSalt)
		defer zeroBytes(combined)
		keyMaterial = combined
	}

	dataKey, err := deriveDataKeyV2(keyMaterial, salt, mode)
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
		if mode == ModePasswordDPAPI {
			return nil, fmt.Errorf("%w: %v", ErrWrongPassword, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	return plaintext, nil
}

func combineMasterAndPassword(masterKey []byte, password string, pwSalt []byte) []byte {
	pwKey := argon2.IDKey([]byte(password), pwSalt, 2, 64*1024, 2, masterKeySize)
	defer zeroBytes(pwKey)
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
