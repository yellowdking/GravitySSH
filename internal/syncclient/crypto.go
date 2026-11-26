package syncclient

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
	"golang.org/x/crypto/pbkdf2"
)

const (
	PBKDF2Iterations  = 200000
	SaltLength        = 16
	NonceLength       = 12
	AESKeyLength      = 32
	MaxPlaintextBytes = 16 * 1024 * 1024
)

// EncryptedPayload is the zero-knowledge wire envelope stored in relay RAM.
type EncryptedPayload struct {
	ChannelID  string `json:"channelId,omitempty"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// DeriveAESKey computes PBKDF2-HMAC-SHA256(canonicalCode, salt, 200000, 32).
func DeriveAESKey(canonicalCode string, salt []byte) []byte {
	return pbkdf2.Key([]byte(canonicalCode), salt, PBKDF2Iterations, AESKeyLength, sha256.New)
}

// EncryptBundle compresses and encrypts a SyncBundle with fresh random salt & nonce.
func EncryptBundle(code string, bundle models.SyncBundle) (EncryptedPayload, error) {
	salt := make([]byte, SaltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return EncryptedPayload{}, fmt.Errorf("generate salt: %w", err)
	}
	nonce := make([]byte, NonceLength)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedPayload{}, fmt.Errorf("generate nonce: %w", err)
	}
	return EncryptBundleDeterministic(code, bundle, salt, nonce)
}

// EncryptBundleDeterministic encrypts a SyncBundle with explicit salt & nonce.
func EncryptBundleDeterministic(code string, bundle models.SyncBundle, salt, nonce []byte) (EncryptedPayload, error) {
	if len(salt) != SaltLength {
		return EncryptedPayload{}, errors.New("salt must be 16 bytes")
	}
	if len(nonce) != NonceLength {
		return EncryptedPayload{}, errors.New("nonce must be 12 bytes")
	}
	canonical, err := CanonicalizeCode(code)
	if err != nil {
		return EncryptedPayload{}, err
	}
	if bundle.SchemaVersion == 0 {
		bundle.SchemaVersion = ProtocolVersion
	}
	rawJSON, err := json.Marshal(bundle)
	if err != nil {
		return EncryptedPayload{}, fmt.Errorf("marshal sync bundle: %w", err)
	}
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	if _, err := gzw.Write(rawJSON); err != nil {
		return EncryptedPayload{}, fmt.Errorf("gzip sync bundle: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return EncryptedPayload{}, fmt.Errorf("close gzip writer: %w", err)
	}
	channelID := DeriveChannelID(canonical)
	block, err := aes.NewCipher(DeriveAESKey(canonical, salt))
	if err != nil {
		return EncryptedPayload{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedPayload{}, err
	}
	ct := gcm.Seal(nil, nonce, gzBuf.Bytes(), []byte(channelID))
	return EncryptedPayload{
		ChannelID:  channelID,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
	}, nil
}

// DecryptBundle verifies AAD, decrypts AES-256-GCM, gunzips, and unmarshals a SyncBundle.
func DecryptBundle(code string, payload EncryptedPayload) (models.SyncBundle, error) {
	canonical, err := CanonicalizeCode(code)
	if err != nil {
		return models.SyncBundle{}, err
	}
	salt, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Salt))
	if err != nil || len(salt) != SaltLength {
		return models.SyncBundle{}, errors.New("同步数据 salt 无效")
	}
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Nonce))
	if err != nil || len(nonce) != NonceLength {
		return models.SyncBundle{}, errors.New("同步数据 nonce 无效")
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Ciphertext))
	if err != nil || len(ct) < 16 {
		return models.SyncBundle{}, errors.New("同步数据密文格式无效")
	}
	channelID := DeriveChannelID(canonical)
	block, err := aes.NewCipher(DeriveAESKey(canonical, salt))
	if err != nil {
		return models.SyncBundle{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return models.SyncBundle{}, err
	}
	gzData, err := gcm.Open(nil, nonce, ct, []byte(channelID))
	if err != nil {
		return models.SyncBundle{}, errors.New("同步数据解密或完整性校验失败，请确认同步码正确")
	}
	gzr, err := gzip.NewReader(bytes.NewReader(gzData))
	if err != nil {
		return models.SyncBundle{}, errors.New("同步数据解压失败")
	}
	defer gzr.Close()
	plainJSON, err := io.ReadAll(io.LimitReader(gzr, MaxPlaintextBytes+1))
	if err != nil {
		return models.SyncBundle{}, errors.New("读取解压同步数据失败")
	}
	if len(plainJSON) > MaxPlaintextBytes {
		return models.SyncBundle{}, errors.New("同步数据包体积超出安全上限")
	}
	var bundle models.SyncBundle
	if err := json.Unmarshal(plainJSON, &bundle); err != nil {
		return models.SyncBundle{}, fmt.Errorf("解析同步数据失败: %w", err)
	}
	if bundle.SchemaVersion != ProtocolVersion {
		return models.SyncBundle{}, fmt.Errorf("不支持的同步协议版本: %d", bundle.SchemaVersion)
	}
	return bundle, nil
}
