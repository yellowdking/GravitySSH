package syncclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Ping verifies TLS 1.3 + App Gate HMAC reachability via POST /v1/ping and returns RTT in ms.
func (c *Client) Ping(ctx context.Context) (int64, error) {
	start := time.Now()
	code, body, err := c.postJSON(ctx, "/v1/ping", map[string]any{})
	if err != nil {
		return 0, err
	}
	rtt := time.Since(start).Milliseconds()
	switch code {
	case http.StatusOK:
		var out struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(body, &out); err != nil || !out.OK {
			return 0, errors.New("中继响应异常，请确认目标运行的是 GravitySSH 同步中继")
		}
		return rtt, nil
	case http.StatusTooManyRequests:
		return 0, errors.New("中继服务器当前繁忙，请稍后再试")
	default:
		return 0, fmt.Errorf("中继连通测试失败（HTTP %d）", code)
	}
}
