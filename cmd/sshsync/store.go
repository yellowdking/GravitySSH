package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

const (
	maxSlots      = 64
	minTTLSeconds = 60
	maxTTLSeconds = 600
	defaultTTLSec = 300
	sweepInterval = 15 * time.Second
)

var (
	errSlotExists = errors.New("channel already exists")
	errStoreFull  = errors.New("relay store full")
)

type b64Bytes []byte

func (b *b64Bytes) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errors.New("expected base64 string")
	}
	inner := data[1 : len(data)-1]
	raw := make([]byte, 0, len(inner))
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) && inner[i+1] == '/' {
			raw = append(raw, '/')
			i++
			continue
		}
		raw = append(raw, inner[i])
	}
	if len(raw) == 0 {
		clear(raw)
		return errors.New("empty base64 string")
	}
	decBuf := make([]byte, base64.StdEncoding.DecodedLen(len(raw)))
	n, err := base64.StdEncoding.Decode(decBuf, raw)
	clear(decBuf)
	if err != nil || n == 0 {
		clear(raw)
		return errors.New("invalid base64")
	}
	*b = raw
	return nil
}

func (b b64Bytes) MarshalJSON() ([]byte, error) {
	out := make([]byte, len(b)+2)
	out[0] = '"'
	copy(out[1:], b)
	out[len(out)-1] = '"'
	return out, nil
}

type slot struct {
	salt       []byte
	nonce      []byte
	ciphertext []byte
	expiresAt  time.Time
}

func zeroSlot(s *slot) {
	if s == nil {
		return
	}
	clear(s.salt)
	clear(s.nonce)
	clear(s.ciphertext)
	s.salt = nil
	s.nonce = nil
	s.ciphertext = nil
}

type memStore struct {
	mu    sync.Mutex
	slots map[string]*slot
	now   func() time.Time
}

func newMemStore(nowFn func() time.Time) *memStore {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &memStore{
		slots: make(map[string]*slot, maxSlots),
		now:   nowFn,
	}
}

func clampTTL(sec int) time.Duration {
	if sec == 0 {
		sec = defaultTTLSec
	} else if sec < minTTLSeconds {
		sec = minTTLSeconds
	} else if sec > maxTTLSeconds {
		sec = maxTTLSeconds
	}
	return time.Duration(sec) * time.Second
}

func (m *memStore) purgeExpiredLocked(now time.Time) int {
	removed := 0
	for k, s := range m.slots {
		if !now.Before(s.expiresAt) {
			delete(m.slots, k)
			zeroSlot(s)
			removed++
		}
	}
	return removed
}

func (m *memStore) Drop(channelID string, salt, nonce, ciphertext []byte, ttlSeconds int) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	if _, exists := m.slots[channelID]; exists {
		return time.Time{}, errSlotExists
	}
	if len(m.slots) >= maxSlots {
		return time.Time{}, errStoreFull
	}

	exp := now.Add(clampTTL(ttlSeconds))
	m.slots[channelID] = &slot{
		salt:       bytes.Clone(salt),
		nonce:      bytes.Clone(nonce),
		ciphertext: bytes.Clone(ciphertext),
		expiresAt:  exp,
	}
	return exp, nil
}

func (m *memStore) Pickup(channelID string) (*slot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.slots[channelID]
	if !ok {
		return nil, false
	}
	delete(m.slots, channelID)
	now := m.now().UTC()
	if !now.Before(s.expiresAt) {
		zeroSlot(s)
		return nil, false
	}
	return s, true
}

func (m *memStore) Cancel(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.slots[channelID]; ok {
		delete(m.slots, channelID)
		zeroSlot(s)
	}
}

func (m *memStore) Sweep() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.purgeExpiredLocked(m.now().UTC())
}

func (m *memStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.slots)
}

func (m *memStore) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.slots {
		delete(m.slots, k)
		zeroSlot(s)
	}
}

func (m *memStore) StartSweeper(stop <-chan struct{}) {
	ticker := time.NewTicker(sweepInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.Sweep()
			case <-stop:
				return
			}
		}
	}()
}
