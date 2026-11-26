package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client communicates with a gssh-sync-v1 relay over TLS 1.3 with App Gate HMAC auth.
type Client struct {
	relayURL   string
	appGateKey string
	httpClient *http.Client
	nowFn      func() time.Time
}

// NewClient builds a sync relay client using SPKI pinning for the default relay,
// TLS 1.3 self-signed cert acceptance for custom raw IP relays, and system CA for domains.
func NewClient(relayURL string) (*Client, error) {
	cleanURL, mode, err := NormalizeRelayURL(relayURL)
	if err != nil {
		return nil, err
	}
	return NewClientWithMode(cleanURL, AppGateKey, PinnedSPKISHA256, mode)
}

// NewCustomClient creates a client with explicit credentials and pinning control.
func NewCustomClient(relayURL, appGateKey, spkiSHA256 string, usePin bool) (*Client, error) {
	mode := RelayTLSModeSystemCA
	if usePin {
		mode = RelayTLSModePinned
	} else if IsRawIPHost(relayURL) {
		mode = RelayTLSModeSelfSignedIP
	}
	return NewClientWithMode(relayURL, appGateKey, spkiSHA256, mode)
}

// NewClientWithMode creates a client with explicit credentials and TLS mode.
func NewClientWithMode(relayURL, appGateKey, spkiSHA256 string, mode RelayTLSMode) (*Client, error) {
	if strings.TrimSpace(appGateKey) == "" {
		return nil, errors.New("同步客户端未配置 App Gate 密钥")
	}
	tlsCfg, err := buildTLSConfig(mode, spkiSHA256)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		TLSClientConfig:     tlsCfg,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   true,
	}
	return &Client{
		relayURL:   strings.TrimRight(relayURL, "/"),
		appGateKey: appGateKey,
		httpClient: &http.Client{Transport: tr, Timeout: 20 * time.Second},
		nowFn:      time.Now,
	}, nil
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody any) (int, []byte, error) {
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.relayURL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	ts := strconv.FormatInt(c.nowFn().UTC().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GSSH-Client", ClientHeaderValue)
	req.Header.Set("X-GSSH-TS", ts)
	req.Header.Set("X-GSSH-Sig", SignRequest(c.appGateKey, http.MethodPost, path, ts, raw))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("连接同步中继失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("读取同步响应失败: %w", err)
	}
	return resp.StatusCode, body, nil
}

// Drop uploads an encrypted sync payload to the relay and returns its expiration time.
func (c *Client) Drop(ctx context.Context, payload EncryptedPayload, ttlSeconds int) (time.Time, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = DefaultTTLSeconds
	}
	req := map[string]any{
		"channelId": payload.ChannelID, "salt": payload.Salt,
		"nonce": payload.Nonce, "ciphertext": payload.Ciphertext, "ttlSeconds": ttlSeconds,
	}
	code, body, err := c.postJSON(ctx, "/v1/drop", req)
	if err != nil {
		return time.Time{}, err
	}
	switch code {
	case http.StatusOK:
		var out struct {
			OK        bool   `json:"ok"`
			ExpiresAt string `json:"expiresAt"`
		}
		if err := json.Unmarshal(body, &out); err != nil || !out.OK {
			return time.Time{}, errors.New("中继服务器返回了无效的上传响应")
		}
		exp, err := time.Parse(time.RFC3339, out.ExpiresAt)
		if err != nil {
			exp = c.nowFn().UTC().Add(time.Duration(ttlSeconds) * time.Second)
		}
		return exp, nil
	case http.StatusConflict:
		return time.Time{}, errors.New("同步码冲突，请重试生成新的同步码")
	case http.StatusTooManyRequests:
		return time.Time{}, errors.New("中继服务器当前繁忙，请稍后再试")
	case http.StatusRequestEntityTooLarge:
		return time.Time{}, errors.New("同步数据体积超过中继限制（2 MiB）")
	default:
		return time.Time{}, fmt.Errorf("上传同步数据失败（HTTP %d）", code)
	}
}

// Pickup fetches and burns an encrypted sync payload from the relay.
func (c *Client) Pickup(ctx context.Context, channelID string) (EncryptedPayload, error) {
	code, body, err := c.postJSON(ctx, "/v1/pickup", map[string]string{"channelId": channelID})
	if err != nil {
		return EncryptedPayload{}, err
	}
	switch code {
	case http.StatusOK:
		var out EncryptedPayload
		if err := json.Unmarshal(body, &out); err != nil {
			return EncryptedPayload{}, errors.New("中继返回的数据格式无效")
		}
		out.ChannelID = channelID
		return out, nil
	case http.StatusNotFound:
		return EncryptedPayload{}, errors.New("未找到对应同步数据（可能同步码错误、已过期或已被领取）")
	case http.StatusTooManyRequests:
		return EncryptedPayload{}, errors.New("请求过于频繁，请稍后再试")
	default:
		return EncryptedPayload{}, fmt.Errorf("获取同步数据失败（HTTP %d）", code)
	}
}

// Cancel deletes an active drop from the relay if it has not been picked up yet.
func (c *Client) Cancel(ctx context.Context, channelID string) error {
	code, _, err := c.postJSON(ctx, "/v1/cancel", map[string]string{"channelId": channelID})
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("撤销同步码失败（HTTP %d）", code)
	}
	return nil
}
