package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSilent404AndAutoBan(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	srv := newRelayServer(newMemStore(nowFn), newIPGuard(nowFn), testAppKey, nowFn)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:1111"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 {
		t.Fatalf("expected silent 404, got %d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Server") != "" || rec.Header().Get("X-Powered-By") != "" {
		t.Fatalf("unexpected server headers: %+v", rec.Header())
	}

	body := []byte(fmt.Sprintf(`{"channelId":"%s"}`, makeTestChannel("skew")))
	recSkew := httptest.NewRecorder()
	srv.ServeHTTP(recSkew, signReq(t, http.MethodPost, "/v1/cancel", body, testAppKey, now.Unix()-125, "203.0.113.6"))
	if recSkew.Code != http.StatusNotFound || recSkew.Body.Len() != 0 {
		t.Fatalf("expected 404 on clock skew, got %d", recSkew.Code)
	}

	badIP := "203.0.113.99"
	for i := 0; i < strikeLimit; i++ {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, signReq(t, http.MethodPost, "/v1/cancel", body, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", now.Unix(), badIP))
		if r.Code != http.StatusNotFound {
			t.Fatalf("strike %d expected 404, got %d", i+1, r.Code)
		}
	}

	rValid := httptest.NewRecorder()
	srv.ServeHTTP(rValid, signReq(t, http.MethodPost, "/v1/cancel", body, testAppKey, now.Unix(), badIP))
	if rValid.Code != http.StatusNotFound {
		t.Fatalf("expected banned IP to get 404, got %d", rValid.Code)
	}

	now = now.Add(banDuration + time.Second)
	rAfter := httptest.NewRecorder()
	srv.ServeHTTP(rAfter, signReq(t, http.MethodPost, "/v1/cancel", body, testAppKey, now.Unix(), badIP))
	if rAfter.Code != http.StatusOK {
		t.Fatalf("expected 200 after ban expiry, got %d", rAfter.Code)
	}
}
