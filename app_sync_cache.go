package main

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/syncsvc"
)

const (
	syncPreviewTTL        = 10 * time.Minute
	maxSyncPreviewEntries = 8
)

type syncPreviewEntry struct {
	bundle    models.SyncBundle
	expiresAt time.Time
	seq       uint64
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
	syncsvc.ZeroSyncBundle(b)
}

type syncPreviewCache struct {
	mu      sync.Mutex
	entries map[string]syncPreviewEntry
	nextSeq uint64
	timer   *time.Timer
	closed  bool
}

func newSyncPreviewCache() *syncPreviewCache {
	return &syncPreviewCache{
		entries: make(map[string]syncPreviewEntry),
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
	for len(c.entries) >= maxSyncPreviewEntries {
		var oldestKey string
		var oldestExp time.Time
		var oldestSeq uint64
		for k, v := range c.entries {
			if oldestKey == "" || v.expiresAt.Before(oldestExp) || (v.expiresAt.Equal(oldestExp) && v.seq < oldestSeq) {
				oldestKey = k
				oldestExp = v.expiresAt
				oldestSeq = v.seq
			}
		}
		if oldestKey == "" {
			break
		}
		victim := c.entries[oldestKey]
		zeroSyncBundle(&victim.bundle)
		delete(c.entries, oldestKey)
	}
	c.nextSeq++
	id := uuid.NewString()
	c.entries[id] = syncPreviewEntry{
		bundle:    cloneSyncBundle(bundle),
		expiresAt: now.Add(syncPreviewTTL),
		seq:       c.nextSeq,
	}
	zeroSyncBundle(&bundle)
	c.rescheduleLocked(now)
	return id
}

func (c *syncPreviewCache) peekClone(id string) (models.SyncBundle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	entry, ok := c.entries[strings.TrimSpace(id)]
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
	cleanID := strings.TrimSpace(id)
	entry, ok := c.entries[cleanID]
	if !ok {
		c.rescheduleLocked(now)
		return models.SyncBundle{}, false
	}
	delete(c.entries, cleanID)
	c.rescheduleLocked(now)
	return entry.bundle, true
}

func (c *syncPreviewCache) clear(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		for k, e := range c.entries {
			zeroSyncBundle(&e.bundle)
			delete(c.entries, k)
		}
	} else if e, ok := c.entries[cleanID]; ok {
		zeroSyncBundle(&e.bundle)
		delete(c.entries, cleanID)
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
}

func (c *syncPreviewCache) rescheduleLocked(now time.Time) {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.closed || len(c.entries) == 0 {
		return
	}
	var earliest time.Time
	for _, v := range c.entries {
		if earliest.IsZero() || v.expiresAt.Before(earliest) {
			earliest = v.expiresAt
		}
	}
	delay := max(time.Millisecond, earliest.Sub(now))
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
		cur := time.Now()
		c.pruneLocked(cur)
		c.rescheduleLocked(cur)
	})
	c.timer = t
}
