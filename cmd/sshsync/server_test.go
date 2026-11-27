package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	req.RemoteAddr = net.JoinHostPort(ip, "44321")
	tsStr := strconv.FormatInt(ts, 10)
	bSum := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(method + "\n" + path + "\n" + tsStr + "\n" + hex.EncodeToString(bSum[:])))
	req.Header.Set(clientHeaderName, clientHeaderValue)
	req.Header.Set(tsHeaderName, tsStr)
	req.Header.Set(sigHeaderName, hex.EncodeToString(mac.Sum(nil)))
	return req
}

func TestRemovedV1EndpointsReturnSilent404(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	srv := newRelayServer(newMemStore(nowFn), newIPGuard(nowFn), testAppKey, nowFn)

	ch := makeTestChannel("removed-v1")
	cases := []struct {
		path string
		body []byte
		ip   string
	}{
		{
			path: "/v1/drop",
			body: []byte(fmt.Sprintf(`{"channelId":"%s","salt":"AQID","nonce":"BAUG","ciphertext":"BwgJ","ttlSeconds":300}`, ch)),
			ip:   "198.51.100.10",
		},
		{
			path: "/v1/pickup",
			body: []byte(fmt.Sprintf(`{"channelId":"%s"}`, ch)),
			ip:   "198.51.100.11",
		},
		{
			path: "/v1/cancel",
			body: []byte(fmt.Sprintf(`{"channelId":"%s"}`, ch)),
			ip:   "198.51.100.12",
		},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, signReq(t, http.MethodPost, tc.path, tc.body, testAppKey, now.Unix(), tc.ip))
		if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "0" {
			t.Fatalf("%s expected silent 404, got code=%d body=%q headers=%+v", tc.path, rec.Code, rec.Body.String(), rec.Header())
		}
		if rec.Header().Get("Server") != "" || rec.Header().Get("X-Powered-By") != "" {
			t.Fatalf("%s leaked server headers: %+v", tc.path, rec.Header())
		}
		if rec.Header().Get("Connection") != "close" {
			t.Fatalf("%s expected Connection: close on silent 404, got %q", tc.path, rec.Header().Get("Connection"))
		}
	}
}
