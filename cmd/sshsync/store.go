package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	maxSlots      = 64
	maxSlotsPerIP = 4
	maxTotalBytes = 16 * 1024 * 1024
	minTTLSeconds = 60
	maxTTLSeconds = 600
	defaultTTLSec = 300
	sweepInterval = 15 * time.Second
)

var (
	errSlotExists   = errors.New("channel already exists")
	errStoreFull    = errors.New("relay store full")
	errIPSlotsFull  = errors.New("per-ip slot limit reached")
	errPairNotFound = errors.New("pair channel not found")
	errPairAuth     = errors.New("pair channel auth failed")
	errSlotBusy     = errors.New("pair channel busy")
	errRelayBusy    = errors.New("relay heavy handler busy")
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
	ownerIP       string
	state         string
	senderToken   []byte
	requestID     string
	reqSalt       []byte
	reqNonce      []byte
	reqCiphertext []byte
	salt          []byte
	nonce         []byte
	ciphertext    []byte
	size          int64
	expiresAt     time.Time
}

func clearReqPayload(s *slot) {
	clear(s.reqSalt)
	clear(s.reqNonce)
	clear(s.reqCiphertext)
	s.reqSalt = nil
	s.reqNonce = nil
	s.reqCiphertext = nil
}

func clearApprovedPayload(s *slot) {
	clear(s.salt)
	clear(s.nonce)
	clear(s.ciphertext)
	s.salt = nil
	s.nonce = nil
	s.ciphertext = nil
}

func zeroSlot(s *slot) {
	if s == nil {
		return
	}
	clear(s.senderToken)
	s.senderToken = nil
	clearReqPayload(s)
	clearApprovedPayload(s)
	s.ownerIP = ""
	s.requestID = ""
	s.state = ""
	s.size = 0
}

type memStore struct {
	mu         sync.Mutex
	slots      map[string]*slot
	ipSlots    map[string]int
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
		ipSlots:  make(map[string]int, maxSlots),
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

func (m *memStore) releaseOwnerSlotLocked(ownerIP string) {
	if ownerIP == "" {
		return
	}
	if n := m.ipSlots[ownerIP]; n <= 1 {
		delete(m.ipSlots, ownerIP)
	} else {
		m.ipSlots[ownerIP] = n - 1
	}
}

func (m *memStore) removeSlotLocked(channelID string, s *slot, zero bool) {
	delete(m.slots, channelID)
	m.releaseOwnerSlotLocked(s.ownerIP)
	m.releaseBytesLocked(s.size)
	if zero {
		zeroSlot(s)
	}
}

func (m *memStore) purgeExpiredLocked(now time.Time) int {
	removed := 0
	for k, s := range m.slots {
		if !now.Before(s.expiresAt) {
			m.removeSlotLocked(k, s, true)
			removed++
		}
	}
	return removed
}

func (m *memStore) lookupPairOwnedLocked(channelID string, senderToken []byte) (*slot, error) {
	s, ok := m.slots[channelID]
	if !ok {
		return nil, errPairNotFound
	}
	if len(s.senderToken) == 0 || subtle.ConstantTimeCompare(s.senderToken, senderToken) != 1 {
		return nil, errPairAuth
	}
	return s, nil
}

func matchRequestID(s *slot, requestID string) bool {
	return s.requestID != "" && subtle.ConstantTimeCompare([]byte(s.requestID), []byte(requestID)) == 1
}

func (m *memStore) PairInit(channelID string, senderToken []byte, ttlSeconds int, ownerIP string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	if _, exists := m.slots[channelID]; exists {
		return time.Time{}, errSlotExists
	}
	ipKey := normalizeIPKey(ownerIP)
	if ipKey != "" && m.ipSlots[ipKey] >= maxSlotsPerIP {
		return time.Time{}, errIPSlotsFull
	}
	if len(m.slots) >= maxSlots {
		return time.Time{}, errStoreFull
	}

	exp := now.Add(clampTTL(ttlSeconds))
	m.slots[channelID] = &slot{
		ownerIP:     ipKey,
		state:       "waiting",
		senderToken: bytes.Clone(senderToken),
		expiresAt:   exp,
	}
	if ipKey != "" {
		m.ipSlots[ipKey]++
	}
	return exp, nil
}

func (m *memStore) PairRequest(channelID, requestID string, salt, nonce, ciphertext []byte) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	s, ok := m.slots[channelID]
	if !ok {
		return time.Time{}, errPairNotFound
	}
	if s.state != "waiting" {
		return time.Time{}, errSlotBusy
	}

	reqSize := int64(len(salt) + len(nonce) + len(ciphertext))
	if m.totalBytes+reqSize > m.maxBytes {
		return time.Time{}, errStoreFull
	}

	s.requestID = requestID
	s.reqSalt = bytes.Clone(salt)
	s.reqNonce = bytes.Clone(nonce)
	s.reqCiphertext = bytes.Clone(ciphertext)
	s.state = "requested"
	s.size = reqSize
	m.totalBytes += reqSize
	return s.expiresAt, nil
}

func (m *memStore) PairPoll(channelID string, senderToken []byte) (state, requestID string, salt, nonce, ct []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	s, err := m.lookupPairOwnedLocked(channelID, senderToken)
	if err != nil {
		return "", "", nil, nil, nil, err
	}
	if s.state == "requested" {
		return s.state, s.requestID, bytes.Clone(s.reqSalt), bytes.Clone(s.reqNonce), bytes.Clone(s.reqCiphertext), nil
	}
	return s.state, s.requestID, nil, nil, nil, nil
}

func (m *memStore) PairApprove(channelID string, senderToken []byte, requestID string, salt, nonce, ciphertext []byte) error {
	return m.pairApprove(channelID, senderToken, requestID, salt, nonce, ciphertext, true)
}

func (m *memStore) PairApproveOwned(channelID string, senderToken []byte, requestID string, salt, nonce, ciphertext []byte) error {
	return m.pairApprove(channelID, senderToken, requestID, salt, nonce, ciphertext, false)
}

func (m *memStore) pairApprove(channelID string, senderToken []byte, requestID string, salt, nonce, ciphertext []byte, clone bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	s, err := m.lookupPairOwnedLocked(channelID, senderToken)
	if err != nil {
		return err
	}
	if !matchRequestID(s, requestID) {
		return errPairAuth
	}
	if s.state != "requested" {
		return errSlotBusy
	}

	newSize := int64(len(salt) + len(nonce) + len(ciphertext))
	delta := newSize - s.size
	if m.totalBytes+delta > m.maxBytes {
		return errStoreFull
	}

	clearReqPayload(s)
	m.releaseBytesLocked(s.size)

	if clone {
		salt = bytes.Clone(salt)
		nonce = bytes.Clone(nonce)
		ciphertext = bytes.Clone(ciphertext)
	}
	s.salt = salt
	s.nonce = nonce
	s.ciphertext = ciphertext
	s.size = newSize
	m.totalBytes += newSize
	s.state = "approved"
	return nil
}

func (m *memStore) PairReject(channelID string, senderToken []byte, requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	s, err := m.lookupPairOwnedLocked(channelID, senderToken)
	if err != nil {
		return err
	}
	if !matchRequestID(s, requestID) {
		return errPairAuth
	}

	clearReqPayload(s)
	clearApprovedPayload(s)
	m.releaseBytesLocked(s.size)
	s.size = 0
	s.state = "rejected"

	rejExp := now.Add(30 * time.Second)
	if rejExp.Before(s.expiresAt) {
		s.expiresAt = rejExp
	}
	return nil
}

func (m *memStore) PairStatus(channelID, requestID string) (state string, entry *slot, err error) {
	return m.PairStatusGated(channelID, requestID, nil)
}

func (m *memStore) PairStatusGated(channelID, requestID string, tryAcquireHeavy func() bool) (state string, entry *slot, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	s, ok := m.slots[channelID]
	if !ok {
		return "", nil, errPairNotFound
	}
	if !matchRequestID(s, requestID) {
		return "", nil, errPairAuth
	}

	switch s.state {
	case "waiting", "requested":
		return "pending", nil, nil
	case "rejected":
		m.removeSlotLocked(channelID, s, true)
		return "rejected", nil, nil
	case "approved":
		if tryAcquireHeavy != nil && !tryAcquireHeavy() {
			return "", nil, errRelayBusy
		}
		m.removeSlotLocked(channelID, s, false)
		return "approved", s, nil
	default:
		return "", nil, errPairNotFound
	}
}

func (m *memStore) PairCancel(channelID string, senderToken []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().UTC()
	m.purgeExpiredLocked(now)

	s, err := m.lookupPairOwnedLocked(channelID, senderToken)
	if err != nil {
		return err
	}
	m.removeSlotLocked(channelID, s, true)
	return nil
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
	clear(m.ipSlots)
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
