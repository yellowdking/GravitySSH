package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	maxSlots      = 64
	maxTotalBytes = 24 << 20
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
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.New("expected base64 string")
	}
	if s == "" {
		return errors.New("empty base64 string")
	}
	dec, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(dec) == 0 {
		clear(dec)
		return errors.New("invalid base64")
	}
	*b = dec
	return nil
}

func (b b64Bytes) MarshalJSON() ([]byte, error) {
	encLen := base64.StdEncoding.EncodedLen(len(b))
	out := make([]byte, encLen+2)
	out[0] = '"'
	base64.StdEncoding.Encode(out[1:1+encLen], b)
	out[len(out)-1] = '"'
	return out, nil
}

type slot struct {
	salt       []byte
	nonce      []byte
	ciphertext []byte
	size       int64
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
	s.size = 0
}

type memStore struct {
	mu         sync.Mutex
	slots      map[string]*slot
	totalBytes int64
	maxBytes   int64
	now        func() time.Time
}

func newMemStore(nowFn func() time.Time) *memStore {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &memStore{
		slots:    make(map[string]*slot, maxSlots),
		maxBytes: maxTotalBytes,
		now:      nowFn,
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

func (m *memStore) releaseBytesLocked(n int64) {
	m.totalBytes -= n
	if m.totalBytes < 0 {
		m.totalBytes = 0
	}
}

func (m *memStore) purgeExpiredLocked(now time.Time) int {
	removed := 0
	for k, s := range m.slots {
		if !now.Before(s.expiresAt) {
			delete(m.slots, k)
			m.releaseBytesLocked(s.size)
			zeroSlot(s)
			removed++
		}
	}
	return removed
}

func (m *memStore) Drop(channelID string, salt, nonce, ciphertext []byte, ttlSeconds int) (time.Time, error) {
	return m.drop(channelID, salt, nonce, ciphertext, ttlSeconds, true)
}

func (m *memStore) DropOwned(channelID string, salt, nonce, ciphertext []byte, ttlSeconds int) (time.Time, error) {
	return m.drop(channelID, salt, nonce, ciphertext, ttlSeconds, false)
}

func (m *memStore) drop(channelID string, salt, nonce, ciphertext []byte, ttlSeconds int, clone bool) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	if _, exists := m.slots[channelID]; exists {
		return time.Time{}, errSlotExists
	}
	slotSize := int64(len(salt) + len(nonce) + len(ciphertext))
	if len(m.slots) >= maxSlots || m.totalBytes+slotSize > m.maxBytes {
		return time.Time{}, errStoreFull
	}

	if clone {
		salt = bytes.Clone(salt)
		nonce = bytes.Clone(nonce)
		ciphertext = bytes.Clone(ciphertext)
	}
	exp := now.Add(clampTTL(ttlSeconds))
	m.slots[channelID] = &slot{
		salt:       salt,
		nonce:      nonce,
		ciphertext: ciphertext,
		size:       slotSize,
		expiresAt:  exp,
	}
	m.totalBytes += slotSize
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
	m.releaseBytesLocked(s.size)
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
		m.releaseBytesLocked(s.size)
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

func (m *memStore) TotalBytes() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalBytes
}

func (m *memStore) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.slots {
		delete(m.slots, k)
		zeroSlot(s)
	}
	m.totalBytes = 0
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
