package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	clientHeaderName  = "X-GSSH-Client"
	clientHeaderValue = "GravitySSH/1"
	tsHeaderName      = "X-GSSH-TS"
	sigHeaderName     = "X-GSSH-Sig"
	maxClockSkewSec   = 120
	maxBodyBytes      = 2 << 20
	maxSmallBodyBytes = 4096
)

type relayServer struct {
	store      *memStore
	guard      *ipGuard
	appGateKey []byte
	now        func() time.Time
}

func newRelayServer(store *memStore, guard *ipGuard, appGateKey string, nowFn func() time.Time) *relayServer {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &relayServer{
		store:      store,
		guard:      guard,
		appGateKey: []byte(appGateKey),
		now:        nowFn,
	}
}

func silentNotFound(w http.ResponseWriter) {
	h := w.Header()
	delete(h, "Server")
	delete(h, "X-Powered-By")
	h.Set("Content-Length", "0")
	w.WriteHeader(http.StatusNotFound)
}

func writeJSON(w http.ResponseWriter, status int, payload []byte) {
	h := w.Header()
	delete(h, "Server")
	delete(h, "X-Powered-By")
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(payload)))
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func (s *relayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.guard.IsBanned(ip) {
		silentNotFound(w)
		return
	}

	if r.Method != http.MethodPost || !isValidEndpoint(r.URL.Path) {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}

	tsStr, sigBytes, ok := s.validateHeaders(r)
	if !ok {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}

	limit := int64(maxSmallBodyBytes)
	if r.URL.Path == "/v1/drop" {
		limit = maxBodyBytes
	}
	rawBody, err := readBoundedBody(r.Body, limit)
	defer clear(rawBody)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, []byte(`{"error":"body_too_large"}`))
			return
		}
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}

	if !s.verifySignature(r.Method, r.URL.Path, tsStr, rawBody, sigBytes) {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}

	if !s.guard.AllowToken(ip) {
		writeJSON(w, http.StatusTooManyRequests, []byte(`{"error":"rate_limited"}`))
		return
	}

	switch r.URL.Path {
	case "/v1/drop":
		s.handleDrop(w, rawBody)
	case "/v1/pickup":
		s.handlePickup(w, ip, rawBody)
	case "/v1/cancel":
		s.handleCancel(w, rawBody)
	case "/v1/ping":
		s.handlePing(w)
	default:
		s.guard.RecordStrike(ip)
		silentNotFound(w)
	}
}

func isValidEndpoint(p string) bool {
	return p == "/v1/drop" || p == "/v1/pickup" || p == "/v1/cancel" || p == "/v1/ping"
}

func (s *relayServer) validateHeaders(r *http.Request) (string, []byte, bool) {
	if r.Header.Get(clientHeaderName) != clientHeaderValue {
		return "", nil, false
	}
	tsStr := r.Header.Get(tsHeaderName)
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || strconv.FormatInt(ts, 10) != tsStr {
		return "", nil, false
	}
	diff := s.now().UTC().Unix() - ts
	if diff < -maxClockSkewSec || diff > maxClockSkewSec {
		return "", nil, false
	}
	sigHex := r.Header.Get(sigHeaderName)
	if !isLowerHex64(sigHex) {
		return "", nil, false
	}
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != sha256.Size {
		return "", nil, false
	}
	return tsStr, sigBytes, true
}

func (s *relayServer) verifySignature(method, path, tsStr string, rawBody, gotSig []byte) bool {
	bodySum := sha256.Sum256(rawBody)
	bodyHex := hex.EncodeToString(bodySum[:])
	mac := hmac.New(sha256.New, s.appGateKey)
	mac.Write([]byte(method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(path))
	mac.Write([]byte("\n"))
	mac.Write([]byte(tsStr))
	mac.Write([]byte("\n"))
	mac.Write([]byte(bodyHex))
	return hmac.Equal(gotSig, mac.Sum(nil))
}

var errBodyTooLarge = errors.New("body too large")

func readBoundedBody(rc io.ReadCloser, limit int64) ([]byte, error) {
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		clear(b)
		return nil, err
	}
	if int64(len(b)) > limit {
		clear(b)
		return nil, errBodyTooLarge
	}
	return b, nil
}
