package main

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/syncclient"
)

const (
	syncPreviewTTL         = 10 * time.Minute
	syncClockSkewGrace     = 135 * time.Second
	maxSyncPreviewEntries  = 8
	maxPendingSyncSessions = 8
)

type syncPreviewEntry struct {
	bundle    models.SyncBundle
	expiresAt time.Time
}

type pendingSyncSendSession struct {
	sessionID       string
	channelID       string
	senderToken     string
	sendCode        string
	relayURL        string
	relayTarget     string
	bundle          models.SyncBundle
	hasBundle       bool
	expiresAt       time.Time
	approving       bool
	approved        bool
	approvedReqID   string
	cachedReqID     string
	cachedReqSalt   string
	cachedReqNonce  string
	cachedReqCipher string
	cachedReqInfo   *syncclient.PairRequestPayload
}

func (s *pendingSyncSendSession) effectiveRelay() string {
	if s.relayTarget != "" {
		return s.relayTarget
	}
	return s.relayURL
}

type pendingSyncReceiveSession struct {
	waitID      string
	channelID   string
	sendCode    string
	recvCode    string
	requestID   string
	relayURL    string
	relayTarget string
	expiresAt   time.Time
}

func (s *pendingSyncReceiveSession) effectiveRelay() string {
	if s.relayTarget != "" {
		return s.relayTarget
	}
	return s.relayURL
}

func cloneSyncBundle(b models.SyncBundle) models.SyncBundle {
	cp := b
	if b.Servers != nil {
		cp.Servers = make([]models.ServerConfig, len(b.Servers))
		copy(cp.Servers, b.Servers)
		for i := range cp.Servers {
			if cp.Servers[i].LastConnectedAt != nil {
				t := *cp.Servers[i].LastConnectedAt
				cp.Servers[i].LastConnectedAt = &t
			}
		}
	}
	if b.SSHKeys != nil {
		cp.SSHKeys = make([]models.SSHKeyRecord, len(b.SSHKeys))
		copy(cp.SSHKeys, b.SSHKeys)
	}
	if b.Shortcuts != nil {
		cp.Shortcuts = make([]models.CommandShortcut, len(b.Shortcuts))
		copy(cp.Shortcuts, b.Shortcuts)
	}
	if b.Templates != nil {
		cp.Templates = make([]models.BuiltinTemplate, len(b.Templates))
		copy(cp.Templates, b.Templates)
	}
	return cp
}

func zeroSyncBundle(b *models.SyncBundle) {
	clear(b.Servers)
	clear(b.SSHKeys)
	clear(b.Shortcuts)
	clear(b.Templates)
	*b = models.SyncBundle{}
}

func zeroSendSession(s *pendingSyncSendSession) {
	if s == nil {
		return
	}
	zeroSyncBundle(&s.bundle)
	s.hasBundle = false
	s.senderToken = ""
	s.sendCode = ""
	s.relayTarget = ""
	s.cachedReqID = ""
	s.cachedReqSalt = ""
	s.cachedReqNonce = ""
	s.cachedReqCipher = ""
	s.cachedReqInfo = nil
}

func evictOldest[V any](m map[string]V, maxEntries int, expires func(V) time.Time, onEvict func(V)) {
	for len(m) >= maxEntries {
		var oldestKey string
		var oldestExp time.Time
		for k, v := range m {
			exp := expires(v)
			if oldestKey == "" || exp.Before(oldestExp) {
				oldestKey = k
				oldestExp = exp
			}
		}
		if oldestKey == "" {
			break
		}
		if onEvict != nil {
			onEvict(m[oldestKey])
		}
		delete(m, oldestKey)
	}
}

type syncPreviewCache struct {
	mu              sync.Mutex
	entries         map[string]syncPreviewEntry
	pendingSends    map[string]*pendingSyncSendSession
	pendingReceives map[string]*pendingSyncReceiveSession
	timer           *time.Timer
	closed          bool
}

func newSyncPreviewCache() *syncPreviewCache {
	return &syncPreviewCache{
		entries:         make(map[string]syncPreviewEntry),
		pendingSends:    make(map[string]*pendingSyncSendSession),
		pendingReceives: make(map[string]*pendingSyncReceiveSession),
	}
}

func (c *syncPreviewCache) put(bundle models.SyncBundle) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		zeroSyncBundle(&bundle)
		return ""
	}
	now := time.Now()
	c.pruneLocked(now)
	evictOldest(c.entries, maxSyncPreviewEntries, func(e syncPreviewEntry) time.Time { return e.expiresAt }, func(e syncPreviewEntry) {
		zeroSyncBundle(&e.bundle)
	})
	id := uuid.NewString()
	c.entries[id] = syncPreviewEntry{bundle: bundle, expiresAt: now.Add(syncPreviewTTL)}
	c.rescheduleLocked(now)
	return id
}

func (c *syncPreviewCache) peekClone(id string) (models.SyncBundle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	entry, ok := c.entries[id]
	c.rescheduleLocked(now)
	if !ok {
		return models.SyncBundle{}, false
	}
	return cloneSyncBundle(entry.bundle), true
}

func (c *syncPreviewCache) pop(id string) (models.SyncBundle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	entry, ok := c.entries[id]
	if !ok {
		c.rescheduleLocked(now)
		return models.SyncBundle{}, false
	}
	delete(c.entries, id)
	c.rescheduleLocked(now)
	return entry.bundle, true
}

func (c *syncPreviewCache) putSend(sess *pendingSyncSendSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		zeroSendSession(sess)
		return
	}
	stored := *sess
	if stored.hasBundle {
		stored.bundle = cloneSyncBundle(sess.bundle)
	}
	now := time.Now()
	c.pruneLocked(now)
	evictOldest(c.pendingSends, maxPendingSyncSessions, func(s *pendingSyncSendSession) time.Time { return s.expiresAt }, zeroSendSession)
	c.pendingSends[stored.sessionID] = &stored
	c.rescheduleLocked(now)
}

func (c *syncPreviewCache) getSend(sessionID string) (*pendingSyncSendSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingSends[sessionID]
	if !ok {
		c.rescheduleLocked(now)
		return nil, false
	}
	cp := *sess
	cp.bundle = models.SyncBundle{}
	cp.cachedReqInfo = nil
	c.rescheduleLocked(now)
	return &cp, true
}

func (c *syncPreviewCache) getCachedPairRequest(sessionID, requestID, salt, nonce, ciphertext string) (syncclient.PairRequestPayload, bool) {
	if requestID == "" {
		return syncclient.PairRequestPayload{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sess, ok := c.pendingSends[sessionID]
	if !ok || sess.cachedReqInfo == nil {
		return syncclient.PairRequestPayload{}, false
	}
	if sess.cachedReqID != requestID ||
		sess.cachedReqSalt != salt ||
		sess.cachedReqNonce != nonce ||
		sess.cachedReqCipher != ciphertext {
		return syncclient.PairRequestPayload{}, false
	}
	return *sess.cachedReqInfo, true
}

func (c *syncPreviewCache) setCachedPairRequest(sessionID, requestID, salt, nonce, ciphertext string, info syncclient.PairRequestPayload) {
	if requestID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if sess, ok := c.pendingSends[sessionID]; ok {
		cp := info
		sess.cachedReqID = requestID
		sess.cachedReqSalt = salt
		sess.cachedReqNonce = nonce
		sess.cachedReqCipher = ciphertext
		sess.cachedReqInfo = &cp
	}
}

func (c *syncPreviewCache) drainAllOnLock() []*pendingSyncSendSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	sends := make([]*pendingSyncSendSession, 0, len(c.pendingSends))
	for k, s := range c.pendingSends {
		cp := *s
		cp.bundle = models.SyncBundle{}
		cp.cachedReqInfo = nil
		sends = append(sends, &cp)
		zeroSendSession(s)
		delete(c.pendingSends, k)
	}
	for k, e := range c.entries {
		zeroSyncBundle(&e.bundle)
		delete(c.entries, k)
	}
	clear(c.pendingReceives)
	c.rescheduleLocked(time.Now())
	return sends
}

// beginSendApprove atomically marks a pending send session as approving and
// returns a metadata snapshot plus a private deep-cloned SyncBundle. Callers
// own the returned bundle copy and must zero it when finished; concurrent
// cancellation/pruning only zeroes the cache's own copy.
func (c *syncPreviewCache) beginSendApprove(sessionID string) (*pendingSyncSendSession, models.SyncBundle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingSends[sessionID]
	if !ok {
		c.rescheduleLocked(now)
		return nil, models.SyncBundle{}, errors.New("同步会话已过期，请重新生成同步码")
	}
	if sess.approved || !sess.hasBundle {
		c.rescheduleLocked(now)
		return nil, models.SyncBundle{}, errors.New("该同步会话已处理，请勿重复发送")
	}
	if sess.approving {
		c.rescheduleLocked(now)
		return nil, models.SyncBundle{}, errors.New("该同步会话正在处理中，请勿重复发送")
	}
	sess.approving = true
	bundleCopy := cloneSyncBundle(sess.bundle)
	meta := *sess
	meta.bundle = models.SyncBundle{}
	c.rescheduleLocked(now)
	return &meta, bundleCopy, nil
}

func (c *syncPreviewCache) isSendApproving(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingSends[sessionID]
	c.rescheduleLocked(now)
	return ok && sess.approving && !sess.approved
}

func (c *syncPreviewCache) abortSendApprove(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sess, ok := c.pendingSends[sessionID]; ok && !sess.approved {
		sess.approving = false
	}
}

func (c *syncPreviewCache) markSendApproved(sessionID, requestID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingSends[sessionID]
	if !ok || !sess.hasBundle || sess.approved {
		c.rescheduleLocked(now)
		return false
	}
	sess.approving = false
	sess.approved = true
	sess.approvedReqID = requestID
	// Drop cached plaintext vault bundle once encrypted & uploaded to relay.
	zeroSyncBundle(&sess.bundle)
	sess.hasBundle = false
	c.rescheduleLocked(now)
	return true
}

func (c *syncPreviewCache) popSend(sessionIDOrCode string) (*pendingSyncSendSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	if sess, ok := c.pendingSends[sessionIDOrCode]; ok {
		delete(c.pendingSends, sessionIDOrCode)
		cp := *sess
		cp.bundle = models.SyncBundle{}
		zeroSendSession(sess)
		c.rescheduleLocked(now)
		return &cp, true
	}
	if canonical, err := syncclient.CanonicalizeCode(sessionIDOrCode); err == nil {
		for k, sess := range c.pendingSends {
			if sess.sendCode == canonical {
				delete(c.pendingSends, k)
				cp := *sess
				cp.bundle = models.SyncBundle{}
				zeroSendSession(sess)
				c.rescheduleLocked(now)
				return &cp, true
			}
		}
	}
	c.rescheduleLocked(now)
	return nil, false
}

func (c *syncPreviewCache) putReceive(sess *pendingSyncReceiveSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	now := time.Now()
	c.pruneLocked(now)
	evictOldest(c.pendingReceives, maxPendingSyncSessions, func(r *pendingSyncReceiveSession) time.Time { return r.expiresAt }, nil)
	c.pendingReceives[sess.waitID] = sess
	c.rescheduleLocked(now)
}

func (c *syncPreviewCache) getReceive(waitID string) (*pendingSyncReceiveSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	sess, ok := c.pendingReceives[waitID]
	if !ok {
		c.rescheduleLocked(now)
		return nil, false
	}
	cp := *sess
	c.rescheduleLocked(now)
	return &cp, true
}

func zeroReceiveSession(r *pendingSyncReceiveSession) {
	if r == nil {
		return
	}
	r.sendCode = ""
	r.recvCode = ""
	r.relayTarget = ""
}

func (c *syncPreviewCache) removeReceive(waitID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.pendingReceives[waitID]; ok {
		zeroReceiveSession(r)
		delete(c.pendingReceives, waitID)
	}
	c.rescheduleLocked(time.Now())
}

func (c *syncPreviewCache) clear(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(id) == "" {
		for k, e := range c.entries {
			zeroSyncBundle(&e.bundle)
			delete(c.entries, k)
		}
		for k, s := range c.pendingSends {
			zeroSendSession(s)
			delete(c.pendingSends, k)
		}
		for k, r := range c.pendingReceives {
			zeroReceiveSession(r)
			delete(c.pendingReceives, k)
		}
	} else {
		if e, ok := c.entries[id]; ok {
			zeroSyncBundle(&e.bundle)
			delete(c.entries, id)
		}
	}
	c.rescheduleLocked(time.Now())
}

func (c *syncPreviewCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	for k, s := range c.pendingSends {
		zeroSendSession(s)
		delete(c.pendingSends, k)
	}
	for k, r := range c.pendingReceives {
		zeroReceiveSession(r)
		delete(c.pendingReceives, k)
	}
	for k, e := range c.entries {
		zeroSyncBundle(&e.bundle)
		delete(c.entries, k)
	}
}

func (c *syncPreviewCache) pruneLocked(now time.Time) {
	for k, v := range c.entries {
		if !now.Before(v.expiresAt) {
			zeroSyncBundle(&v.bundle)
			delete(c.entries, k)
		}
	}
	for k, s := range c.pendingSends {
		if !now.Before(s.expiresAt.Add(syncClockSkewGrace)) {
			zeroSendSession(s)
			delete(c.pendingSends, k)
		}
	}
	for k, r := range c.pendingReceives {
		if !now.Before(r.expiresAt.Add(syncClockSkewGrace)) {
			zeroReceiveSession(r)
			delete(c.pendingReceives, k)
		}
	}
}

func (c *syncPreviewCache) rescheduleLocked(now time.Time) {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.closed || (len(c.entries) == 0 && len(c.pendingSends) == 0 && len(c.pendingReceives) == 0) {
		return
	}
	var earliest time.Time
	consider := func(t time.Time) {
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	for _, v := range c.entries {
		consider(v.expiresAt)
	}
	for _, s := range c.pendingSends {
		consider(s.expiresAt.Add(syncClockSkewGrace))
	}
	for _, r := range c.pendingReceives {
		consider(r.expiresAt.Add(syncClockSkewGrace))
	}
	delay := earliest.Sub(now)
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	var t *time.Timer
	t = time.AfterFunc(delay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.timer != t {
			return
		}
		c.timer = nil
		if c.closed {
			return
		}
		now := time.Now()
		c.pruneLocked(now)
		c.rescheduleLocked(now)
	})
	c.timer = t
}
