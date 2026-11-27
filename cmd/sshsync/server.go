package main

import (
	"bytes"
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
	clientHeaderName   = "X-GSSH-Client"
	clientHeaderValue  = "GravitySSH/1"
	tsHeaderName       = "X-GSSH-TS"
	sigHeaderName      = "X-GSSH-Sig"
	maxClockSkewSec    = 120
	maxBodyBytes       = 2 << 20
	maxSmallBodyBytes  = 4096
	maxConcurrentDrops = 4
)

type relayServer struct {
	store        *memStore
	guard        *ipGuard
	appGateKey   []byte
	now          func() time.Time
	heavyBodySem chan struct{}
}

func newRelayServer(store *memStore, guard *ipGuard, appGateKey string, nowFn func() time.Time) *relayServer {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &relayServer{
		store:        store,
		guard:        guard,
		appGateKey:   []byte(appGateKey),
		now:          nowFn,
		heavyBodySem: make(chan struct{}, maxConcurrentDrops),
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
	isHeavy := r.URL.Path == "/v1/drop" || r.URL.Path == "/v1/pair/approve"
	if isHeavy {
		limit = maxBodyBytes
	}
	if r.ContentLength > limit {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}

	if isHeavy {
		select {
		case s.heavyBodySem <- struct{}{}:
			defer func() { <-s.heavyBodySem }()
		case <-r.Context().Done():
			silentNotFound(w)
			return
		}
	}

	rawBody, err := readBoundedBody(r.Body, r.ContentLength, limit)
	defer clear(rawBody)
	if err != nil {
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
	case "/v1/pair/init":
		s.handlePairInit(w, ip, rawBody)
	case "/v1/pair/request":
		s.handlePairRequest(w, ip, rawBody)
	case "/v1/pair/poll":
		s.handlePairPoll(w, ip, rawBody)
	case "/v1/pair/approve":
		s.handlePairApprove(w, ip, rawBody)
	case "/v1/pair/reject":
		s.handlePairReject(w, ip, rawBody)
	case "/v1/pair/status":
		s.handlePairStatus(w, ip, rawBody)
	case "/v1/pair/cancel":
		s.handlePairCancel(w, ip, rawBody)
	}
}

func isValidEndpoint(p string) bool {
	switch p {
	case "/v1/drop", "/v1/pickup", "/v1/cancel", "/v1/ping",
		"/v1/pair/init", "/v1/pair/request", "/v1/pair/poll",
		"/v1/pair/approve", "/v1/pair/reject", "/v1/pair/status", "/v1/pair/cancel":
		return true
	default:
		return false
	}
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

func readBoundedBody(rc io.ReadCloser, contentLength, limit int64) ([]byte, error) {
	defer rc.Close()
	if contentLength > limit {
		return nil, errBodyTooLarge
	}

	if contentLength > 0 {
		buf := make([]byte, contentLength)
		n, err := io.ReadFull(rc, buf)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				return buf[:n], nil
			}
			clear(buf)
			return nil, err
		}
		var extra [1]byte
		en, eerr := rc.Read(extra[:])
		if en > 0 {
			if contentLength+int64(en) > limit {
				clear(buf)
				return nil, errBodyTooLarge
			}
			var rest bytes.Buffer
			rest.Write(buf)
			clear(buf)
			rest.Write(extra[:en])
			if _, rerr := io.Copy(&rest, io.LimitReader(rc, limit-int64(rest.Len())+1)); rerr != nil {
				clear(rest.Bytes())
				return nil, rerr
			}
			b := rest.Bytes()
			if int64(len(b)) > limit {
				clear(b)
				return nil, errBodyTooLarge
			}
			return b, nil
		}
		if eerr != nil && !errors.Is(eerr, io.EOF) {
			clear(buf)
			return nil, eerr
		}
		return buf, nil
	}

	var r io.Reader = rc
	if contentLength == 0 {
		var probe [1]byte
		n, err := rc.Read(probe[:])
		if n == 0 {
			if err == nil || errors.Is(err, io.EOF) {
				return nil, nil
			}
			return nil, err
		}
		r = io.MultiReader(bytes.NewReader(probe[:n]), rc)
	}

	var out bytes.Buffer
	if _, err := io.Copy(&out, io.LimitReader(r, limit+1)); err != nil {
		clear(out.Bytes())
		return nil, err
	}
	b := out.Bytes()
	if int64(len(b)) > limit {
		clear(b)
		return nil, errBodyTooLarge
	}
	return b, nil
}
