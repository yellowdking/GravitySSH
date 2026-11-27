package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/syncclient"
)

const syncPreviewTTL = 10 * time.Minute

type syncPreviewEntry struct {
	bundle    models.SyncBundle
	expiresAt time.Time
}

type syncPreviewCache struct {
	mu      sync.Mutex
	entries map[string]syncPreviewEntry
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
		return ""
	}
	now := time.Now()
	c.pruneLocked(now)
	id := uuid.NewString()
	c.entries[id] = syncPreviewEntry{bundle: bundle, expiresAt: now.Add(syncPreviewTTL)}
	c.rescheduleLocked(now)
	return id
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

func (c *syncPreviewCache) clear(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.TrimSpace(id) == "" {
		clear(c.entries)
	} else {
		delete(c.entries, id)
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
	clear(c.entries)
}

func (c *syncPreviewCache) pruneLocked(now time.Time) {
	for k, v := range c.entries {
		if !now.Before(v.expiresAt) {
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
	delay := time.Until(earliest)
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	c.timer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		now := time.Now()
		c.pruneLocked(now)
		c.rescheduleLocked(now)
	})
}

func (a *App) syncContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) resolveRelayURL(relayURL string) string {
	if trimmed := strings.TrimSpace(relayURL); trimmed != "" {
		return trimmed
	}
	if a.store != nil {
		if saved := strings.TrimSpace(a.store.GetSettings().SyncRelayURL); saved != "" {
			return saved
		}
	}
	return syncclient.DefaultRelayURL
}

func (a *App) GetDefaultSyncRelayURL() string { return syncclient.DefaultRelayURL }

// ClearSyncPreview discards a pending decrypted sync preview from memory
// (or all pending previews when previewID is empty).
func (a *App) ClearSyncPreview(previewID string) {
	if a.syncCache != nil {
		a.syncCache.clear(previewID)
	}
}

// TestSyncRelay checks TLS 1.3 + App Gate connectivity to the given or saved relay URL.
func (a *App) TestSyncRelay(relayURL string) (int64, error) {
	if err := a.ensureReady(); err != nil {
		return 0, err
	}
	client, err := syncclient.NewClient(a.resolveRelayURL(relayURL))
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(a.syncContext(), 10*time.Second)
	defer cancel()
	return client.Ping(ctx)
}

func (a *App) StartSyncSend(relayURL string) (models.SyncSendSession, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncSendSession{}, err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "Windows-PC"
	}
	bundle, err := a.store.ExportSyncBundle(host)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	code, err := syncclient.GenerateCode()
	if err != nil {
		return models.SyncSendSession{}, err
	}
	qrText, err := syncclient.FormatQRText(code)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	payload, err := syncclient.EncryptBundle(code, bundle)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	cleanURL, mode, err := syncclient.NormalizeRelayURL(a.resolveRelayURL(relayURL))
	if err != nil {
		return models.SyncSendSession{}, err
	}
	client, err := syncclient.NewClientWithMode(cleanURL, syncclient.AppGateKey, syncclient.PinnedSPKISHA256, mode)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	expiresAt, err := client.Drop(a.syncContext(), payload, syncclient.DefaultTTLSeconds)
	if err != nil {
		return models.SyncSendSession{}, err
	}
	return models.SyncSendSession{
		Code:          code,
		QRText:        qrText,
		ExpiresAt:     expiresAt.UTC().Format(time.RFC3339),
		RelayURL:      cleanURL,
		ServerCount:   len(bundle.Servers),
		KeyCount:      len(bundle.SSHKeys),
		ShortcutCount: len(bundle.Shortcuts),
		TemplateCount: len(bundle.Templates),
	}, nil
}

func (a *App) CancelSyncSend(code, relayURL string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	canonical, err := syncclient.CanonicalizeCode(code)
	if err != nil {
		return err
	}
	client, err := syncclient.NewClient(a.resolveRelayURL(relayURL))
	if err != nil {
		return err
	}
	return client.Cancel(a.syncContext(), syncclient.DeriveChannelID(canonical))
}

func (a *App) PreviewSyncReceive(code, relayURL string) (models.SyncReceivePreview, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncReceivePreview{}, err
	}
	canonical, err := syncclient.CanonicalizeCode(code)
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	client, err := syncclient.NewClient(a.resolveRelayURL(relayURL))
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	payload, err := client.Pickup(a.syncContext(), syncclient.DeriveChannelID(canonical))
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	bundle, err := syncclient.DecryptBundle(canonical, payload)
	if err != nil {
		return models.SyncReceivePreview{}, err
	}
	return models.SyncReceivePreview{
		PreviewID:      a.syncCache.put(bundle),
		SchemaVersion:  bundle.SchemaVersion,
		ExportedAt:     bundle.ExportedAt,
		SourcePlatform: bundle.SourcePlatform,
		SourceDevice:   bundle.SourceDevice,
		ServerCount:    len(bundle.Servers),
		KeyCount:       len(bundle.SSHKeys),
		ShortcutCount:  len(bundle.Shortcuts),
		TemplateCount:  len(bundle.Templates),
	}, nil
}

func (a *App) ApplySyncReceive(previewID, mode string) (models.SyncImportSummary, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncImportSummary{}, err
	}
	bundle, ok := a.syncCache.pop(previewID)
	if !ok {
		return models.SyncImportSummary{}, errors.New("预览已过期或已导入，请重新获取同步码")
	}
	return a.store.ImportSyncBundle(bundle, mode)
}
