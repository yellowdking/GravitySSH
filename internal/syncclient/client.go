package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxDropPayloadBytes  = 2 * 1024 * 1024
	maxResponseBodyBytes = 4 * 1024 * 1024
)

// ErrPairNotFound indicates the pairing channel or request does not exist, expired, or was already consumed.
var ErrPairNotFound = errors.New("未找到对应同步配对（可能同步码错误、已过期或已被领取/取消）")

// PairPollResult is returned by POST /v1/pair/poll.
type PairPollResult struct {
	State     string `json:"state"`
	NotFound  bool   `json:"notFound,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	EncryptedPayload
}

// PairStatusResult is returned by POST /v1/pair/status.
type PairStatusResult struct {
	State string `json:"state"`
	EncryptedPayload
}

// Client communicates with a gssh-sync-v2 relay over TLS 1.3 with App Gate HMAC auth.
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
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       tlsCfg,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableKeepAlives:     false,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConnsPerHost:   2,
	}
	return &Client{
		relayURL:   strings.TrimRight(relayURL, "/"),
		appGateKey: appGateKey,
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		nowFn: time.Now,
	}, nil
}

// CloseIdleConnections closes any pooled idle keep-alive connections held by this client.
func (c *Client) CloseIdleConnections() {
	if c != nil && c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody any) (int, []byte, error) {
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return 0, nil, err
	}
	if len(raw) > maxDropPayloadBytes {
		return 0, nil, errors.New("同步数据体积超过中继限制（2 MiB）")
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("读取同步响应失败: %w", err)
	}
	if len(body) > maxResponseBodyBytes {
		return resp.StatusCode, nil, errors.New("同步中继响应体积超过安全上限（4 MiB）")
	}
	return resp.StatusCode, body, nil
}

func (c *Client) parseExpiresResponse(body []byte, ttlSeconds int, errMsg string) (time.Time, error) {
	var out struct {
		OK        bool   `json:"ok"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(body, &out); err != nil || !out.OK {
		return time.Time{}, errors.New(errMsg)
	}
	exp, err := time.Parse(time.RFC3339, out.ExpiresAt)
	if err != nil {
		exp = c.nowFn().UTC().Add(time.Duration(ttlSeconds) * time.Second)
	}
	return exp, nil
}

// PairInit registers an empty pairing slot for gssh-sync-v2 without uploading any vault payload.
func (c *Client) PairInit(ctx context.Context, channelID, senderToken string, ttlSeconds int) (time.Time, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = DefaultTTLSeconds
	}
	req := map[string]any{
		"channelId":   channelID,
		"senderToken": senderToken,
		"ttlSeconds":  ttlSeconds,
	}
	code, body, err := c.postJSON(ctx, "/v1/pair/init", req)
	if err != nil {
		return time.Time{}, err
	}
	switch code {
	case http.StatusOK:
		return c.parseExpiresResponse(body, ttlSeconds, "中继服务器返回了无效的配对初始化响应")
	case http.StatusConflict:
		return time.Time{}, errors.New("同步码冲突，请重试生成新的同步码")
	case http.StatusTooManyRequests:
		return time.Time{}, errors.New("中继服务器当前繁忙，请稍后再试")
	default:
		return time.Time{}, fmt.Errorf("初始化配对通道失败（HTTP %d）", code)
	}
}

// PairRequest submits an encrypted PairRequestPayload from the receiver to wait for sender verification.
func (c *Client) PairRequest(ctx context.Context, channelID, requestID string, req EncryptedPayload) (time.Time, error) {
	bodyReq := map[string]string{
		"channelId":  channelID,
		"requestId":  requestID,
		"salt":       req.Salt,
		"nonce":      req.Nonce,
		"ciphertext": req.Ciphertext,
	}
	code, body, err := c.postJSON(ctx, "/v1/pair/request", bodyReq)
	if err != nil {
		return time.Time{}, err
	}
	switch code {
	case http.StatusOK:
		return c.parseExpiresResponse(body, DefaultTTLSeconds, "中继服务器返回了无效的配对请求响应")
	case http.StatusNotFound:
		return time.Time{}, ErrPairNotFound
	case http.StatusConflict:
		return time.Time{}, errors.New("该同步码已有其他设备正在等待确认，请让发送端重新生成同步码")
	case http.StatusTooManyRequests:
		return time.Time{}, errors.New("请求过于频繁或中继繁忙，请稍后再试")
	case http.StatusRequestEntityTooLarge:
		return time.Time{}, errors.New("配对请求体积超过中继限制")
	default:
		return time.Time{}, fmt.Errorf("提交配对验证请求失败（HTTP %d）", code)
	}
}

// PairPoll checks the pairing slot state for the sender. HTTP 404 is returned cleanly as {State: "gone", NotFound: true}.
func (c *Client) PairPoll(ctx context.Context, channelID, senderToken string) (PairPollResult, error) {
	req := map[string]string{
		"channelId":   channelID,
		"senderToken": senderToken,
	}
	code, body, err := c.postJSON(ctx, "/v1/pair/poll", req)
	if err != nil {
		return PairPollResult{}, err
	}
	switch code {
	case http.StatusOK:
		var out PairPollResult
		if err := json.Unmarshal(body, &out); err != nil {
			return PairPollResult{}, errors.New("中继返回的配对状态格式无效")
		}
		out.EncryptedPayload.ChannelID = channelID
		return out, nil
	case http.StatusNotFound:
		return PairPollResult{State: "gone", NotFound: true}, nil
	case http.StatusTooManyRequests:
		return PairPollResult{}, errors.New("请求过于频繁，请稍后再试")
	default:
		return PairPollResult{}, fmt.Errorf("查询配对状态失败（HTTP %d）", code)
	}
}

// PairApprove uploads the dual-key encrypted SyncBundle after the sender confirms the receiver's code.
func (c *Client) PairApprove(ctx context.Context, channelID, senderToken, requestID string, payload EncryptedPayload) error {
	req := map[string]string{
		"channelId":   channelID,
		"senderToken": senderToken,
		"requestId":   requestID,
		"salt":        payload.Salt,
		"nonce":       payload.Nonce,
		"ciphertext":  payload.Ciphertext,
	}
	code, _, err := c.postJSON(ctx, "/v1/pair/approve", req)
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrPairNotFound
	case http.StatusConflict:
		return errors.New("配对状态已变更，无法重复同意")
	case http.StatusTooManyRequests:
		return errors.New("中继服务器当前繁忙，请稍后再试")
	case http.StatusRequestEntityTooLarge:
		return errors.New("同步数据体积超过中继限制（2 MiB）")
	default:
		return fmt.Errorf("上传同步数据失败（HTTP %d）", code)
	}
}

// PairReject rejects an incoming pairing request on the relay.
func (c *Client) PairReject(ctx context.Context, channelID, senderToken, requestID string) error {
	req := map[string]string{
		"channelId":   channelID,
		"senderToken": senderToken,
		"requestId":   requestID,
	}
	code, _, err := c.postJSON(ctx, "/v1/pair/reject", req)
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrPairNotFound
	case http.StatusConflict:
		return errors.New("配对状态已变更，无法拒绝")
	case http.StatusTooManyRequests:
		return errors.New("请求过于频繁，请稍后再试")
	default:
		return fmt.Errorf("拒绝配对请求失败（HTTP %d）", code)
	}
}

// PairStatus checks the pairing status for the receiver and burns-after-read the encrypted payload once approved.
func (c *Client) PairStatus(ctx context.Context, channelID, requestID string) (PairStatusResult, error) {
	req := map[string]string{
		"channelId": channelID,
		"requestId": requestID,
	}
	code, body, err := c.postJSON(ctx, "/v1/pair/status", req)
	if err != nil {
		return PairStatusResult{}, err
	}
	switch code {
	case http.StatusOK:
		var out PairStatusResult
		if err := json.Unmarshal(body, &out); err != nil {
			return PairStatusResult{}, errors.New("中继返回的配对状态格式无效")
		}
		out.EncryptedPayload.ChannelID = channelID
		return out, nil
	case http.StatusNotFound:
		return PairStatusResult{}, ErrPairNotFound
	case http.StatusTooManyRequests:
		return PairStatusResult{}, errors.New("请求过于频繁，请稍后再试")
	default:
		return PairStatusResult{}, fmt.Errorf("查询配对状态失败（HTTP %d）", code)
	}
}

// PairCancel deletes an active pairing slot from the relay using the senderToken.
func (c *Client) PairCancel(ctx context.Context, channelID, senderToken string) error {
	req := map[string]string{
		"channelId":   channelID,
		"senderToken": senderToken,
	}
	code, _, err := c.postJSON(ctx, "/v1/pair/cancel", req)
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrPairNotFound
	case http.StatusTooManyRequests:
		return errors.New("请求过于频繁，请稍后再试")
	default:
		return fmt.Errorf("撤销配对失败（HTTP %d）", code)
	}
}
