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
	PBKDF2Iterations         = 200000
	SaltLength               = 16
	NonceLength              = 12
	AESKeyLength             = 32
	MaxPlaintextBytes        = 16 * 1024 * 1024
	MaxPairRequestPlainBytes = 64 * 1024
)

// EncryptedPayload is the zero-knowledge wire envelope stored in relay RAM.
type EncryptedPayload struct {
	ChannelID  string `json:"channelId,omitempty"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// PairRequestPayload is the encrypted receiver verification payload exchanged in gssh-sync-v2.
type PairRequestPayload struct {
	ReceiverCode     string `json:"receiverCode"`
	ReceiverDevice   string `json:"receiverDevice"`
	ReceiverPlatform string `json:"receiverPlatform"`
	RequestedAt      string `json:"requestedAt"`
}

// DeriveAESKey computes PBKDF2-HMAC-SHA256(secret, salt, 200000, 32).
func DeriveAESKey(secret string, salt []byte) []byte {
	return pbkdf2.Key([]byte(secret), salt, PBKDF2Iterations, AESKeyLength, sha256.New)
}

func randomSaltAndNonce() ([]byte, []byte, error) {
	salt := make([]byte, SaltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("generate salt: %w", err)
	}
	nonce := make([]byte, NonceLength)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	return salt, nonce, nil
}

func sealGzipAESGCM(secret, aad, channelID string, rawJSON, salt, nonce []byte) (EncryptedPayload, error) {
	if len(salt) != SaltLength {
		return EncryptedPayload{}, errors.New("salt must be 16 bytes")
	}
	if len(nonce) != NonceLength {
		return EncryptedPayload{}, errors.New("nonce must be 12 bytes")
	}
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	if _, err := gzw.Write(rawJSON); err != nil {
		return EncryptedPayload{}, fmt.Errorf("gzip sync payload: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return EncryptedPayload{}, fmt.Errorf("close gzip writer: %w", err)
	}
	key := DeriveAESKey(secret, salt)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return EncryptedPayload{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedPayload{}, err
	}
	ct := gcm.Seal(nil, nonce, gzBuf.Bytes(), []byte(aad))
	return EncryptedPayload{
		ChannelID:  channelID,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
	}, nil
}

func openAESGCMGunzip(secret, aad string, payload EncryptedPayload, maxPlainBytes int64, badTagMsg string) ([]byte, error) {
	if maxPlainBytes <= 0 {
		maxPlainBytes = MaxPlaintextBytes
	}
	salt, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Salt))
	if err != nil || len(salt) != SaltLength {
		return nil, errors.New("同步数据 salt 无效")
	}
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Nonce))
	if err != nil || len(nonce) != NonceLength {
		return nil, errors.New("同步数据 nonce 无效")
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Ciphertext))
	if err != nil || len(ct) < 16 {
		return nil, errors.New("同步数据密文格式无效")
	}
	key := DeriveAESKey(secret, salt)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	gzData, err := gcm.Open(nil, nonce, ct, []byte(aad))
	if err != nil {
		return nil, errors.New(badTagMsg)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(gzData))
	if err != nil {
		return nil, errors.New("同步数据解压失败")
	}
	defer gzr.Close()
	plainJSON, err := io.ReadAll(io.LimitReader(gzr, maxPlainBytes+1))
	if err != nil {
		return nil, errors.New("读取解压同步数据失败")
	}
	if int64(len(plainJSON)) > maxPlainBytes {
		clear(plainJSON)
		return nil, errors.New("同步数据包体积超出安全上限")
	}
	return plainJSON, nil
}

// EncryptPairRequest encrypts receiver verification info using canonicalSend and AAD = channelID.
func EncryptPairRequest(sendCode string, info PairRequestPayload) (EncryptedPayload, error) {
	salt, nonce, err := randomSaltAndNonce()
	if err != nil {
		return EncryptedPayload{}, err
	}
	return EncryptPairRequestDeterministic(sendCode, info, salt, nonce)
}

// EncryptPairRequestDeterministic encrypts PairRequestPayload with explicit salt & nonce.
func EncryptPairRequestDeterministic(sendCode string, info PairRequestPayload, salt, nonce []byte) (EncryptedPayload, error) {
	canonicalSend, err := CanonicalizeCode(sendCode)
	if err != nil {
		return EncryptedPayload{}, err
	}
	if _, err := CanonicalizeCode(info.ReceiverCode); err != nil {
		return EncryptedPayload{}, err
	}
	rawJSON, err := json.Marshal(info)
	if err != nil {
		return EncryptedPayload{}, fmt.Errorf("marshal pair request: %w", err)
	}
	defer clear(rawJSON)
	channelID := DeriveChannelID(canonicalSend)
	return sealGzipAESGCM(canonicalSend, channelID, channelID, rawJSON, salt, nonce)
}

// DecryptPairRequest decrypts and verifies a receiver PairRequestPayload using sendCode.
func DecryptPairRequest(sendCode string, payload EncryptedPayload) (PairRequestPayload, error) {
	canonicalSend, err := CanonicalizeCode(sendCode)
	if err != nil {
		return PairRequestPayload{}, err
	}
	channelID := DeriveChannelID(canonicalSend)
	plainJSON, err := openAESGCMGunzip(canonicalSend, channelID, payload, MaxPairRequestPlainBytes, "配对验证请求解密或完整性校验失败")
	if err != nil {
		return PairRequestPayload{}, err
	}
	defer clear(plainJSON)
	var info PairRequestPayload
	if err := json.Unmarshal(plainJSON, &info); err != nil {
		return PairRequestPayload{}, fmt.Errorf("解析配对验证请求失败: %w", err)
	}
	if _, err := CanonicalizeCode(info.ReceiverCode); err != nil {
		return PairRequestPayload{}, errors.New("配对请求中的接收端确认码格式无效")
	}
	return info, nil
}

func deriveDualParams(sendCode, recvCode, requestID string) (dualSecret, dualAAD, channelID string, err error) {
	dualSecret, err = DeriveDualSecret(sendCode, recvCode)
	if err != nil {
		return "", "", "", err
	}
	reqID := strings.TrimSpace(requestID)
	if reqID == "" {
		return "", "", "", errors.New("requestId 不能为空")
	}
	canonicalSend, canonicalRecv, _ := strings.Cut(dualSecret, ":")
	channelID = DeriveChannelID(canonicalSend)
	dualAAD = DeriveDualAAD(channelID, reqID, canonicalRecv)
	return dualSecret, dualAAD, channelID, nil
}

// EncryptBundleDual encrypts a SyncBundle with both sendCode and recvCode bound to requestID (gssh-sync-v2).
func EncryptBundleDual(sendCode, recvCode, requestID string, bundle models.SyncBundle) (EncryptedPayload, error) {
	salt, nonce, err := randomSaltAndNonce()
	if err != nil {
		return EncryptedPayload{}, err
	}
	return EncryptBundleDualDeterministic(sendCode, recvCode, requestID, bundle, salt, nonce)
}

// EncryptBundleDualDeterministic encrypts a SyncBundle with explicit salt & nonce using dual-key derivation.
func EncryptBundleDualDeterministic(sendCode, recvCode, requestID string, bundle models.SyncBundle, salt, nonce []byte) (EncryptedPayload, error) {
	dualSecret, dualAAD, channelID, err := deriveDualParams(sendCode, recvCode, requestID)
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
	defer clear(rawJSON)
	return sealGzipAESGCM(dualSecret, dualAAD, channelID, rawJSON, salt, nonce)
}

// DecryptBundleDual decrypts and verifies a gssh-sync-v2 dual-key SyncBundle payload.
func DecryptBundleDual(sendCode, recvCode, requestID string, payload EncryptedPayload) (models.SyncBundle, error) {
	dualSecret, dualAAD, _, err := deriveDualParams(sendCode, recvCode, requestID)
	if err != nil {
		return models.SyncBundle{}, err
	}
	plainJSON, err := openAESGCMGunzip(dualSecret, dualAAD, payload, MaxPlaintextBytes, "双向密钥解密或完整性校验失败，请确认双方同步码与确认码一致")
	if err != nil {
		return models.SyncBundle{}, err
	}
	defer clear(plainJSON)
	var bundle models.SyncBundle
	if err := json.Unmarshal(plainJSON, &bundle); err != nil {
		return models.SyncBundle{}, fmt.Errorf("解析同步数据失败: %w", err)
	}
	if bundle.SchemaVersion < MinBundleSchemaVersion || bundle.SchemaVersion > MaxBundleSchemaVersion {
		return models.SyncBundle{}, fmt.Errorf("不支持的同步协议版本: %d", bundle.SchemaVersion)
	}
	return bundle, nil
}
