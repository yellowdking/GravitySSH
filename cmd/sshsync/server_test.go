package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testAppKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func makeTestChannel(tag string) string {
	s := sha256.Sum256([]byte("chan:" + tag))
	return hex.EncodeToString(s[:])
}

func signReq(t *testing.T, method, path string, body []byte, key string, ts int64, ip string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.RemoteAddr = ip + ":44321"
	tsStr := strconv.FormatInt(ts, 10)
	bSum := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(method + "\n" + path + "\n" + tsStr + "\n" + hex.EncodeToString(bSum[:])))
	req.Header.Set(clientHeaderName, clientHeaderValue)
	req.Header.Set(tsHeaderName, tsStr)
	req.Header.Set(sigHeaderName, hex.EncodeToString(mac.Sum(nil)))
	return req
}

func TestDropPickupBurnAndCancel(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	srv := newRelayServer(newMemStore(nowFn), newIPGuard(nowFn), testAppKey, nowFn)

	ch := makeTestChannel("alpha")
	salt := base64.StdEncoding.EncodeToString([]byte{0xff, 0xff, 0xfe, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13})
	nonce := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	ct := base64.StdEncoding.EncodeToString([]byte("secret-ciphertext-blob"))
	dropBody := []byte(fmt.Sprintf(
		`{"channelId":"%s","salt":"%s","nonce":"%s","ciphertext":"%s","ttlSeconds":900}`,
		ch, strings.ReplaceAll(salt, "/", `\/`), nonce, ct,
	))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, signReq(t, http.MethodPost, "/v1/drop", dropBody, testAppKey, now.Unix(), "198.51.100.10"))
	if rec.Code != http.StatusOK {
		t.Fatalf("drop status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dropResp struct {
		OK        bool   `json:"ok"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dropResp); err != nil || !dropResp.OK {
		t.Fatalf("bad drop resp: %s", rec.Body.String())
	}
	if dropResp.ExpiresAt != now.Add(600*time.Second).Format(time.RFC3339) {
		t.Fatalf("expected clamped 600s ttl, got %s", dropResp.ExpiresAt)
	}

	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, signReq(t, http.MethodPost, "/v1/drop", dropBody, testAppKey, now.Unix(), "198.51.100.10"))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec2.Code)
	}

	pickBody := []byte(fmt.Sprintf(`{"channelId":"%s"}`, ch))
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, signReq(t, http.MethodPost, "/v1/pickup", pickBody, testAppKey, now.Unix(), "198.51.100.11"))
	if rec3.Code != http.StatusOK {
		t.Fatalf("pickup status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	var pickResp struct {
		Salt       string `json:"salt"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(rec3.Body.Bytes(), &pickResp); err != nil {
		t.Fatalf("unmarshal pickup: %v", err)
	}
	if pickResp.Salt != salt || pickResp.Nonce != nonce || pickResp.Ciphertext != ct {
		t.Fatalf("pickup mismatch: %+v", pickResp)
	}

	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, signReq(t, http.MethodPost, "/v1/pickup", pickBody, testAppKey, now.Unix(), "198.51.100.11"))
	if rec4.Code != http.StatusNotFound || rec4.Body.Len() != 0 {
		t.Fatalf("expected silent 404 on 2nd pickup, got %d len=%d", rec4.Code, rec4.Body.Len())
	}

	ch2 := makeTestChannel("beta")
	drop2 := []byte(fmt.Sprintf(`{"channelId":"%s","salt":"%s","nonce":"%s","ciphertext":"%s"}`, ch2, salt, nonce, ct))
	rDrop := httptest.NewRecorder()
	srv.ServeHTTP(rDrop, signReq(t, http.MethodPost, "/v1/drop", drop2, testAppKey, now.Unix(), "198.51.100.12"))
	if rDrop.Code != http.StatusOK {
		t.Fatalf("drop2 status=%d", rDrop.Code)
	}
	cancelBody := []byte(fmt.Sprintf(`{"channelId":"%s"}`, ch2))
	rCan := httptest.NewRecorder()
	srv.ServeHTTP(rCan, signReq(t, http.MethodPost, "/v1/cancel", cancelBody, testAppKey, now.Unix(), "198.51.100.12"))
	if rCan.Code != http.StatusOK || !strings.Contains(rCan.Body.String(), `"ok":true`) {
		t.Fatalf("cancel failed: %d %s", rCan.Code, rCan.Body.String())
	}
	rPick := httptest.NewRecorder()
	srv.ServeHTTP(rPick, signReq(t, http.MethodPost, "/v1/pickup", cancelBody, testAppKey, now.Unix(), "198.51.100.12"))
	if rPick.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after cancel, got %d", rPick.Code)
	}
}
