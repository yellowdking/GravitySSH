package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type pairInitRequest struct {
	ChannelID   string `json:"channelId"`
	SenderToken string `json:"senderToken"`
	TTLSeconds  int    `json:"ttlSeconds"`
}

type pairRequestBody struct {
	ChannelID  string   `json:"channelId"`
	RequestID  string   `json:"requestId"`
	Salt       b64Bytes `json:"salt"`
	Nonce      b64Bytes `json:"nonce"`
	Ciphertext b64Bytes `json:"ciphertext"`
}

type pairTokenRequest struct {
	ChannelID   string `json:"channelId"`
	SenderToken string `json:"senderToken"`
}

type pairApproveRequest struct {
	ChannelID   string   `json:"channelId"`
	SenderToken string   `json:"senderToken"`
	RequestID   string   `json:"requestId"`
	Salt        b64Bytes `json:"salt"`
	Nonce       b64Bytes `json:"nonce"`
	Ciphertext  b64Bytes `json:"ciphertext"`
}

type pairRejectRequest struct {
	ChannelID   string `json:"channelId"`
	SenderToken string `json:"senderToken"`
	RequestID   string `json:"requestId"`
}

type pairStatusRequest struct {
	ChannelID string `json:"channelId"`
	RequestID string `json:"requestId"`
}

func writeOKExpiry(w http.ResponseWriter, exp time.Time) {
	resp, _ := json.Marshal(struct {
		OK        bool   `json:"ok"`
		ExpiresAt string `json:"expiresAt"`
	}{OK: true, ExpiresAt: exp.UTC().Format(time.RFC3339)})
	writeJSON(w, http.StatusOK, resp)
}

func (s *relayServer) writeStoreErr(w http.ResponseWriter, ip string, err error) {
	switch {
	case errors.Is(err, errPairNotFound):
		s.guard.RecordStrike(ip)
		silentNotFound(w)
	case errors.Is(err, errSlotExists):
		writeJSON(w, http.StatusConflict, []byte(`{"error":"exists"}`))
	case errors.Is(err, errSlotBusy):
		writeJSON(w, http.StatusConflict, []byte(`{"error":"busy"}`))
	case errors.Is(err, errStoreFull):
		writeJSON(w, http.StatusTooManyRequests, []byte(`{"error":"full"}`))
	default:
		writeJSON(w, http.StatusInternalServerError, []byte(`{"error":"internal"}`))
	}
}

func decodeHexToken64(s string) ([]byte, bool) {
	if !isLowerHex64(s) {
		return nil, false
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		clear(b)
		return nil, false
	}
	return b, true
}

func (s *relayServer) handlePing(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, []byte(`{"ok":true,"service":"sshsync","protocol":"gssh-sync-v2"}`))
}

func (s *relayServer) handlePairInit(w http.ResponseWriter, ip string, rawBody []byte) {
	var req pairInitRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	tok, ok := decodeHexToken64(req.SenderToken)
	defer clear(tok)
	if !isLowerHex64(req.ChannelID) || !ok {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	exp, err := s.store.PairInit(req.ChannelID, tok, req.TTLSeconds)
	if err != nil {
		s.writeStoreErr(w, ip, err)
		return
	}
	writeOKExpiry(w, exp)
}

func (s *relayServer) handlePairRequest(w http.ResponseWriter, ip string, rawBody []byte) {
	var req pairRequestBody
	defer func() {
		clear(req.Salt)
		clear(req.Nonce)
		clear(req.Ciphertext)
	}()
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	if !isLowerHex64(req.ChannelID) || !isLowerHex64(req.RequestID) || len(req.Salt) == 0 || len(req.Nonce) == 0 || len(req.Ciphertext) == 0 {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	exp, err := s.store.PairRequest(req.ChannelID, req.RequestID, req.Salt, req.Nonce, req.Ciphertext)
	if err != nil {
		s.writeStoreErr(w, ip, err)
		return
	}
	writeOKExpiry(w, exp)
}

func (s *relayServer) handlePairPoll(w http.ResponseWriter, ip string, rawBody []byte) {
	var req pairTokenRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	tok, ok := decodeHexToken64(req.SenderToken)
	defer clear(tok)
	if !isLowerHex64(req.ChannelID) || !ok {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	state, requestID, salt, nonce, ct, status := s.store.PairPoll(req.ChannelID, tok)
	defer func() {
		clear(salt)
		clear(nonce)
		clear(ct)
	}()
	if status != http.StatusOK {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}
	resp, err := json.Marshal(struct {
		State      string   `json:"state"`
		RequestID  string   `json:"requestId,omitempty"`
		Salt       b64Bytes `json:"salt,omitempty"`
		Nonce      b64Bytes `json:"nonce,omitempty"`
		Ciphertext b64Bytes `json:"ciphertext,omitempty"`
	}{
		State:      state,
		RequestID:  requestID,
		Salt:       b64Bytes(salt),
		Nonce:      b64Bytes(nonce),
		Ciphertext: b64Bytes(ct),
	})
	defer clear(resp)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, []byte(`{"error":"encode"}`))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *relayServer) handlePairApprove(w http.ResponseWriter, ip string, rawBody []byte) {
	var req pairApproveRequest
	defer func() {
		clear(req.Salt)
		clear(req.Nonce)
		clear(req.Ciphertext)
	}()
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	tok, ok := decodeHexToken64(req.SenderToken)
	defer clear(tok)
	if !isLowerHex64(req.ChannelID) || !ok || !isLowerHex64(req.RequestID) ||
		len(req.Salt) == 0 || len(req.Nonce) == 0 || len(req.Ciphertext) == 0 {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	if err := s.store.PairApproveOwned(req.ChannelID, tok, req.RequestID, req.Salt, req.Nonce, req.Ciphertext); err != nil {
		s.writeStoreErr(w, ip, err)
		return
	}
	// Ownership of decoded byte slices transferred to memStore; prevent defer from wiping stored data.
	req.Salt, req.Nonce, req.Ciphertext = nil, nil, nil
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}

func (s *relayServer) handlePairReject(w http.ResponseWriter, ip string, rawBody []byte) {
	var req pairRejectRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	tok, ok := decodeHexToken64(req.SenderToken)
	defer clear(tok)
	if !isLowerHex64(req.ChannelID) || !ok || !isLowerHex64(req.RequestID) {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	if err := s.store.PairReject(req.ChannelID, tok, req.RequestID); err != nil {
		s.writeStoreErr(w, ip, err)
		return
	}
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}

func (s *relayServer) handlePairStatus(w http.ResponseWriter, ip string, rawBody []byte) {
	var req pairStatusRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	if !isLowerHex64(req.ChannelID) || !isLowerHex64(req.RequestID) {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	state, entry, status := s.store.PairStatus(req.ChannelID, req.RequestID)
	defer zeroSlot(entry)
	if status != http.StatusOK {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}
	out := struct {
		State      string   `json:"state"`
		Salt       b64Bytes `json:"salt,omitempty"`
		Nonce      b64Bytes `json:"nonce,omitempty"`
		Ciphertext b64Bytes `json:"ciphertext,omitempty"`
	}{
		State: state,
	}
	if entry != nil {
		out.Salt = b64Bytes(entry.salt)
		out.Nonce = b64Bytes(entry.nonce)
		out.Ciphertext = b64Bytes(entry.ciphertext)
	}
	resp, err := json.Marshal(out)
	defer clear(resp)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, []byte(`{"error":"encode"}`))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *relayServer) handlePairCancel(w http.ResponseWriter, ip string, rawBody []byte) {
	var req pairTokenRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	tok, ok := decodeHexToken64(req.SenderToken)
	defer clear(tok)
	if !isLowerHex64(req.ChannelID) || !ok {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	if !s.store.PairCancel(req.ChannelID, tok) {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}
