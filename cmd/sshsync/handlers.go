package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type dropRequest struct {
	ChannelID  string   `json:"channelId"`
	Salt       b64Bytes `json:"salt"`
	Nonce      b64Bytes `json:"nonce"`
	Ciphertext b64Bytes `json:"ciphertext"`
	TTLSeconds int      `json:"ttlSeconds"`
}

type channelRequest struct {
	ChannelID string `json:"channelId"`
}

func (s *relayServer) handleDrop(w http.ResponseWriter, rawBody []byte) {
	var req dropRequest
	defer func() {
		clear(req.Salt)
		clear(req.Nonce)
		clear(req.Ciphertext)
	}()
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_json"}`))
		return
	}
	if !isLowerHex64(req.ChannelID) || len(req.Salt) == 0 || len(req.Nonce) == 0 || len(req.Ciphertext) == 0 {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_fields"}`))
		return
	}
	exp, err := s.store.Drop(req.ChannelID, req.Salt, req.Nonce, req.Ciphertext, req.TTLSeconds)
	if err != nil {
		switch {
		case errors.Is(err, errSlotExists):
			writeJSON(w, http.StatusConflict, []byte(`{"error":"exists"}`))
		case errors.Is(err, errStoreFull):
			writeJSON(w, http.StatusTooManyRequests, []byte(`{"error":"full"}`))
		default:
			writeJSON(w, http.StatusInternalServerError, []byte(`{"error":"internal"}`))
		}
		return
	}
	resp, _ := json.Marshal(struct {
		OK        bool   `json:"ok"`
		ExpiresAt string `json:"expiresAt"`
	}{OK: true, ExpiresAt: exp.UTC().Format(time.RFC3339)})
	writeJSON(w, http.StatusOK, resp)
}

func (s *relayServer) handlePickup(w http.ResponseWriter, ip string, rawBody []byte) {
	var req channelRequest
	if err := json.Unmarshal(rawBody, &req); err != nil || !isLowerHex64(req.ChannelID) {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_channel"}`))
		return
	}
	entry, ok := s.store.Pickup(req.ChannelID)
	if !ok {
		s.guard.RecordStrike(ip)
		silentNotFound(w)
		return
	}
	defer zeroSlot(entry)

	resp, err := json.Marshal(struct {
		Salt       b64Bytes `json:"salt"`
		Nonce      b64Bytes `json:"nonce"`
		Ciphertext b64Bytes `json:"ciphertext"`
	}{
		Salt:       b64Bytes(entry.salt),
		Nonce:      b64Bytes(entry.nonce),
		Ciphertext: b64Bytes(entry.ciphertext),
	})
	defer clear(resp)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, []byte(`{"error":"encode"}`))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *relayServer) handleCancel(w http.ResponseWriter, rawBody []byte) {
	var req channelRequest
	if err := json.Unmarshal(rawBody, &req); err != nil || !isLowerHex64(req.ChannelID) {
		writeJSON(w, http.StatusBadRequest, []byte(`{"error":"invalid_channel"}`))
		return
	}
	s.store.Cancel(req.ChannelID)
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}

func (s *relayServer) handlePing(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, []byte(`{"ok":true,"service":"sshsync","protocol":"gssh-sync-v1"}`))
}
