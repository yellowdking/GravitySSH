package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	clientHeaderName      = "X-GSSH-Client"
	clientHeaderValue     = "GravitySSH/1"
	tsHeaderName          = "X-GSSH-TS"
	sigHeaderName         = "X-GSSH-Sig"
	protoHeaderName       = "X-GSSH-Proto"
	protoMinHeaderName    = "X-GSSH-Proto-Min"
	protoMaxHeaderName    = "X-GSSH-Proto-Max"
	MinProtoVersion       = 1
	MaxProtoVersion       = 2
	maxClockSkewSec       = 120
	maxBodyBytes          = 2 << 20
	maxSmallBodyBytes     = 4096
	maxConcurrentApproves = 4
	initialBodyBufBytes   = 4096
	readBodyChunkBytes    = 16 * 1024
)

var (
	protoMinHeaderValue     = strconv.Itoa(MinProtoVersion)
	protoMaxHeaderValue     = strconv.Itoa(MaxProtoVersion)
	unsupportedProtoPayload = []byte(fmt.Sprintf(
		`{"error":"unsupported_protocol","minVersion":%d,"maxVersion":%d}`,
		MinProtoVersion, MaxProtoVersion,
	))
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
		heavyBodySem: make(chan struct{}, maxConcurrentApproves),
	}
}

func silentNotFound(w http.ResponseWriter) {
	h := w.Header()
	delete(h, "Server")
	delete(h, "X-Powered-By")
	h.Set("Connection", "close")
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

func writeJSONClose(w http.ResponseWriter, status int, payload []byte) {
	w.Header().Set("Connection", "close")
	writeJSON(w, status, payload)
}

func setProtoVersionHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set(protoMinHeaderName, protoMinHeaderValue)
	h.Set(protoMaxHeaderName, protoMaxHeaderValue)
}

func validateProtoVersion(r *http.Request) bool {
	vals := r.Header.Values(protoHeaderName)
	if len(vals) == 0 {
		return true
	}
	if len(vals) > 1 {
		return false
	}
	v := strings.TrimSpace(vals[0])
	if v == "" {
		return true
	}
	n, err := strconv.Atoi(v)
	if err != nil || strconv.Itoa(n) != v {
		return false
	}
	return n >= MinProtoVersion && n <= MaxProtoVersion
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
	isHeavy := r.URL.Path == "/v1/pair/approve"
	if isHeavy {
		limit = maxBodyBytes
	}
	if r.ContentLength > limit {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}

	rateChecked := false
	if isHeavy {
		if !s.guard.AllowToken(ip) {
			writeJSONClose(w, http.StatusTooManyRequests, []byte(`{"error":"rate_limited"}`))
			return
		}
		rateChecked = true

		if !s.guard.TryAcquireHeavy(ip) {
			writeJSONClose(w, http.StatusTooManyRequests, []byte(`{"error":"busy"}`))
			return
		}
		defer s.guard.ReleaseHeavy(ip)

		select {
		case s.heavyBodySem <- struct{}{}:
			defer func() { <-s.heavyBodySem }()
		default:
			writeJSONClose(w, http.StatusTooManyRequests, []byte(`{"error":"busy"}`))
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

	setProtoVersionHeaders(w)

	if !rateChecked {
		isPollEndpoint := r.URL.Path == "/v1/ping" || r.URL.Path == "/v1/pair/poll" || r.URL.Path == "/v1/pair/status"
		allowed := false
		if isPollEndpoint {
			allowed = s.guard.AllowPollToken(ip)
		} else {
			allowed = s.guard.AllowToken(ip)
		}
		if !allowed {
			writeJSON(w, http.StatusTooManyRequests, []byte(`{"error":"rate_limited"}`))
			return
		}
	}

	if !validateProtoVersion(r) {
		writeJSON(w, http.StatusUpgradeRequired, unsupportedProtoPayload)
		return
	}

	switch r.URL.Path {
	case "/v1/ping":
		s.handlePing(w, ip)
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
	case "/v1/ping",
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
	if rc != nil {
		defer rc.Close()
	}
	if contentLength > limit {
		return nil, errBodyTooLarge
	}
	if rc == nil {
		return nil, nil
	}

	lr := io.LimitReader(rc, limit+1)
	var out []byte
	var tmp [readBodyChunkBytes]byte
	defer clear(tmp[:])

	for {
		n, err := lr.Read(tmp[:])
		if n > 0 {
			if int64(len(out)+n) > limit {
				clear(out)
				return nil, errBodyTooLarge
			}
			if cap(out)-len(out) < n {
				newCap := cap(out) * 2
				if newCap < initialBodyBufBytes {
					newCap = initialBodyBufBytes
				}
				if newCap < len(out)+n {
					newCap = len(out) + n
				}
				if int64(newCap) > limit {
					newCap = int(limit)
				}
				next := make([]byte, len(out), newCap)
				copy(next, out)
				clear(out)
				out = next
			}
			out = append(out, tmp[:n]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			clear(out)
			return nil, err
		}
	}
}
