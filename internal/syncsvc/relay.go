package syncsvc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/vault"
	synccrypto "github.com/yellowdking/synctool/sdk/go/crypto"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

// Built-in default VPS2 relay configuration (overridable via -ldflags or SYNCTOOL_DEFAULT_RELAY_URI).
var (
	defaultRelayEndpoint  = "https://43.155.231.14:54843"
	defaultRelaySPKIPin   = "f7757f3863cfd16d6f3c92c36d48d12fc500058f42a8946987056f6263335da8"
	defaultRelayAccessKey = "vAs_uYp5mZn187h54j6LsFG-0w3b5qtEQif3C5lcTyQ"
)

// BuiltInRelayProfile constructs the built-in default RelayProfile, honoring SYNCTOOL_DEFAULT_RELAY_URI if set.
func BuiltInRelayProfile() (*envelope.RelayProfile, error) {
	if envURI := strings.TrimSpace(os.Getenv("SYNCTOOL_DEFAULT_RELAY_URI")); envURI != "" {
		return envelope.ParseRelayURI(envURI)
	}
	origin, err := synccrypto.CanonicalRelayOrigin(defaultRelayEndpoint)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrCodeURIInvalid, false, 0, err.Error(), nil)
	}
	var pins []string
	tlsMode := envelope.TLSModeSystemCA
	if strings.TrimSpace(defaultRelaySPKIPin) != "" {
		pins, err = envelope.ParseSPKIPins(defaultRelaySPKIPin)
		if err != nil {
			return nil, err
		}
		tlsMode = envelope.TLSModeSPKIPin
	}
	prof := &envelope.RelayProfile{
		RelayID:   synccrypto.DeriveRelayID(origin),
		Endpoint:  origin,
		AccessKey: defaultRelayAccessKey,
		TLSMode:   tlsMode,
		SPKIPins:  pins,
		IsDefault: true,
	}
	if err := prof.SyncPins(); err != nil {
		return nil, err
	}
	return prof, nil
}

// ResolveActiveRelay returns the user's custom RelayProfile (mode="custom") if configured in VaultSyncKeyStore,
// otherwise the built-in default RelayProfile (mode="default").
func ResolveActiveRelay(ks *vault.VaultSyncKeyStore) (*envelope.RelayProfile, string, error) {
	if ks != nil {
		custom, err := ks.GetCustomRelay()
		if err != nil {
			return nil, "", err
		}
		if custom != nil {
			origin, oErr := synccrypto.CanonicalRelayOrigin(custom.Endpoint)
			if oErr != nil {
				return nil, "", envelope.NewError(envelope.ErrCodeURIInvalid, false, 0, oErr.Error(), nil)
			}
			custom.Endpoint = origin
			if custom.RelayID == "" {
				custom.RelayID = synccrypto.DeriveRelayID(origin)
			}
			if err := custom.SyncPins(); err != nil {
				return nil, "", err
			}
			return custom, "custom", nil
		}
	}
	def, err := BuiltInRelayProfile()
	if err != nil {
		return nil, "", err
	}
	return def, "default", nil
}

// RedactedRelayURI formats a RelayProfile into a displayable synctool://relay URI with #[REDACTED]
// so AccessKey (#k=) is NEVER exposed to frontend DTOs or logs.
func RedactedRelayURI(prof *envelope.RelayProfile) string {
	if prof == nil {
		return ""
	}
	origin, err := synccrypto.CanonicalRelayOrigin(prof.Endpoint)
	if err != nil {
		origin = strings.TrimSpace(prof.Endpoint)
	}
	pins, _ := envelope.NormalizeSPKIPins(prof.SPKIPins, prof.SPKISHA256, prof.SPKIPin)
	var sb strings.Builder
	sb.WriteString("synctool://relay?v=1&u=")
	sb.WriteString(url.QueryEscape(origin))
	if len(pins) > 0 {
		sb.WriteString("&pin=")
		sb.WriteString(strings.Join(pins, ","))
	}
	sb.WriteString("#[REDACTED]")
	return sb.String()
}

// ShortSPKIPin returns an abbreviated display fingerprint (e.g. "a1b2c3d4...ef012345") for UI display.
func ShortSPKIPin(prof *envelope.RelayProfile) string {
	if prof == nil {
		return ""
	}
	pins, _ := envelope.NormalizeSPKIPins(prof.SPKIPins, prof.SPKISHA256, prof.SPKIPin)
	if len(pins) == 0 {
		return ""
	}
	p := pins[0]
	short := p
	if len(p) >= 16 {
		short = p[:8] + "..." + p[len(p)-8:]
	}
	if len(pins) > 1 {
		return fmt.Sprintf("%s (+%d)", short, len(pins)-1)
	}
	return short
}

func formatUnixRFC3339(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}

// BuildRelayStatus constructs a secret-free SyncRelayStatus DTO for the frontend.
func BuildRelayStatus(prof *envelope.RelayProfile, mode string, certExpiresAt int64, certRotateRecommended bool) models.SyncRelayStatus {
	if prof == nil {
		return models.SyncRelayStatus{Mode: mode}
	}
	if mode == "" {
		mode = "default"
	}
	tlsMode := prof.TLSMode
	if tlsMode == "" {
		if len(prof.SPKIPins) > 0 || prof.SPKISHA256 != "" {
			tlsMode = envelope.TLSModeSPKIPin
		} else {
			tlsMode = envelope.TLSModeSystemCA
		}
	}
	if certExpiresAt > 0 && !certRotateRecommended {
		if remDays := (certExpiresAt - time.Now().Unix()) / 86400; remDays < 30 {
			certRotateRecommended = true
		}
	}
	return models.SyncRelayStatus{
		Mode:                  mode,
		Endpoint:              prof.Endpoint,
		RedactedURI:           RedactedRelayURI(prof),
		RelayID:               prof.RelayID,
		TLSMode:               tlsMode,
		SPKIPinShort:          ShortSPKIPin(prof),
		CertExpiresAt:         formatUnixRFC3339(certExpiresAt),
		CertRotateRecommended: certRotateRecommended,
	}
}

// FormatSyncError scrubs any #fragment/?k= secrets and translates SyncError codes into clear UI errors.
func FormatSyncError(err error) error {
	if err == nil {
		return nil
	}
	se, ok := envelope.AsSyncError(err)
	if !ok {
		return errors.New(envelope.RedactMessage(err.Error()))
	}
	safeMsg := envelope.RedactMessage(se.Message)
	switch se.Code {
	case envelope.ErrCodeURIInvalid:
		return fmt.Errorf("同步链接或中继地址格式无效: %s", safeMsg)
	case envelope.ErrCodeTLSPinMismatch:
		return errors.New("中继服务器 TLS 证书指纹不匹配")
	case envelope.ErrCodeTLSCertInvalid:
		return fmt.Errorf("中继服务器 TLS 证书无效或已过期: %s", safeMsg)
	case envelope.ErrCodeAuthFailed:
		return errors.New("中继访问密钥 (#k=) 验证失败，请检查自定义中继配置")
	case envelope.ErrCodeRelayUnreachable:
		return fmt.Errorf("无法连接到同步中继服务器: %s", safeMsg)
	case envelope.ErrCodeSASMismatch:
		return errors.New("6 位安全核验码不匹配或对方已拒绝核验，房间已销毁")
	case envelope.ErrCodeRoomBurned:
		return errors.New("同步房间已销毁或核验失败，请重新发起")
	case envelope.ErrCodeHandshakeTimeout, envelope.ErrCodePipePeerTimeout:
		return errors.New("等待对端响应超时，请确认双方同时在线后重试")
	case envelope.ErrCodeSpoolDisabled:
		return errors.New("当前中继未开启离线暂存信箱 (--spool=off)，请改用实时直传")
	case envelope.ErrCodeOfflineQuotaExceeded:
		return errors.New("离线暂存信箱配额已满或数据超限，请清理收件箱或改用实时直传")
	case envelope.ErrCodeDeviceUntrusted:
		return errors.New("该设备未配对或已被吊销信任，请重新绑定设备")
	case envelope.ErrCodeMsgReplayDetected:
		return errors.New("检测到重复或过期的同步数据包（防重放保护已拦截）")
	case envelope.ErrCodeRateLimited:
		return errors.New("中继请求过于频繁，请稍后再试")
	case envelope.ErrCodeNotFound:
		return errors.New("同步房间或信箱条目不存在、已过期或已被领取销毁")
	default:
		if safeMsg != "" {
			return fmt.Errorf("[%s] %s", se.Code, safeMsg)
		}
		return errors.New(se.Code)
	}
}

// buildPinnedControlClient creates a short-lived TLS 1.3 + SPKI-pinned HTTP client for zero-blob ACK burn.
func buildPinnedControlClient(profile *envelope.RelayProfile) (*http.Client, error) {
	pins, err := envelope.NormalizeSPKIPins(profile.SPKIPins, profile.SPKISHA256, profile.SPKIPin)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	if profile.TLSMode == envelope.TLSModeSPKIPin || len(pins) > 0 {
		if len(pins) == 0 {
			return nil, envelope.NewError(envelope.ErrCodeURIInvalid, false, 0, "spki_pin mode requires SPKI pin", nil)
		}
		expectedPins := append([]string(nil), pins...)
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return envelope.NewError(envelope.ErrCodeTLSCertInvalid, false, 0, "no TLS certificate", nil)
			}
			leaf := cs.PeerCertificates[0]
			now := time.Now()
			if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
				return envelope.NewError(envelope.ErrCodeTLSCertInvalid, false, 0, "TLS certificate expired", nil)
			}
			sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
			gotPin := hex.EncodeToString(sum[:])
			matched := 0
			for _, exp := range expectedPins {
				matched |= subtle.ConstantTimeCompare([]byte(gotPin), []byte(exp))
			}
			if matched != 1 {
				return envelope.NewError(envelope.ErrCodeTLSPinMismatch, false, 0, "SPKI pin mismatch", nil)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSClientConfig:       tlsCfg,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// ackSpoolItemDirect sends an Ed25519-signed POST /v1/spool/{item_id}/ack directly without downloading blobs.
func ackSpoolItemDirect(
	ctx context.Context,
	prof *envelope.RelayProfile,
	mailboxID string,
	mailboxAuthSK ed25519.PrivateKey,
	itemID string,
	clockOffsetSec int64,
) error {
	cleanID := strings.TrimSpace(itemID)
	if cleanID == "" {
		return envelope.NewError(envelope.ErrCodeBadRequest, false, 0, "empty inbox itemId", nil)
	}
	httpCli, err := buildPinnedControlClient(prof)
	if err != nil {
		return err
	}
	defer httpCli.CloseIdleConnections()

	path := "/v1/spool/" + url.PathEscape(cleanID) + "/ack"
	bodyBytes := []byte("{}")
	bodyBind := synccrypto.BodyBindingControl(bodyBytes)

	offset := clockOffsetSec
	for attempt := 0; attempt < 2; attempt++ {
		ts := strconv.FormatInt(time.Now().Unix()+offset, 10)
		nonce, err := synccrypto.GenerateNonceHex()
		if err != nil {
			return err
		}
		sig := synccrypto.SignRelayRequest(prof.AccessKey, http.MethodPost, path, ts, nonce, bodyBind, "")
		mbSig := synccrypto.SignMailboxRequest(mailboxAuthSK, http.MethodPost, path, mailboxID, ts, nonce, bodyBind)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, prof.Endpoint+path, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		req.ContentLength = int64(len(bodyBytes))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set(synccrypto.HeaderClientAgent, "synctool-go/1.0")
		req.Header.Set(synccrypto.HeaderRelayTS, ts)
		req.Header.Set(synccrypto.HeaderRelayNonce, nonce)
		req.Header.Set(synccrypto.HeaderRelaySig, sig)
		req.Header.Set(synccrypto.HeaderMailboxSig, mbSig)

		resp, err := httpCli.Do(req)
		if err != nil {
			return envelope.WrapError(envelope.ErrCodeRelayUnreachable, true, 0, "ack inbox item", err)
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == http.StatusNotFound {
			return nil
		}
		if resp.StatusCode == http.StatusConflict && attempt == 0 {
			if srvTS, pErr := strconv.ParseInt(strings.TrimSpace(resp.Header.Get(synccrypto.HeaderRelayTS)), 10, 64); pErr == nil && srvTS > 0 {
				offset = srvTS - time.Now().Unix()
				continue
			}
		}
		return envelope.NewError(envelope.ErrCodeBadRequest, false, resp.StatusCode, fmt.Sprintf("ack failed (HTTP %d): %s", resp.StatusCode, string(respBody)), nil)
	}
	return nil
}
