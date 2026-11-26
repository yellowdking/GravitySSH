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
}

func (a *App) getSyncCache() *syncPreviewCache {
	if a.syncCache == nil {
		a.syncCache = &syncPreviewCache{entries: make(map[string]syncPreviewEntry)}
	}
	return a.syncCache
}

func (c *syncPreviewCache) put(bundle models.SyncBundle) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	id := uuid.NewString()
	c.entries[id] = syncPreviewEntry{bundle: bundle, expiresAt: now.Add(syncPreviewTTL)}
	return id
}

func (c *syncPreviewCache) pop(id string) (models.SyncBundle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.pruneLocked(now)
	entry, ok := c.entries[id]
	if !ok {
		return models.SyncBundle{}, false
	}
	delete(c.entries, id)
	return entry.bundle, true
}

func (c *syncPreviewCache) pruneLocked(now time.Time) {
	for k, v := range c.entries {
		if now.After(v.expiresAt) {
			delete(c.entries, k)
		}
	}
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
	cleanURL, _, err := syncclient.NormalizeRelayURL(a.resolveRelayURL(relayURL))
	if err != nil {
		return models.SyncSendSession{}, err
	}
	client, err := syncclient.NewClient(cleanURL)
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
		PreviewID:      a.getSyncCache().put(bundle),
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
	bundle, ok := a.getSyncCache().pop(previewID)
	if !ok {
		return models.SyncImportSummary{}, errors.New("预览已过期或已导入，请重新获取同步码")
	}
	return a.store.ImportSyncBundle(bundle, mode)
}
