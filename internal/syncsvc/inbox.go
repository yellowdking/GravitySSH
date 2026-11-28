package syncsvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/vault"
	"github.com/yellowdking/synctool/sdk/go/client"
	synccrypto "github.com/yellowdking/synctool/sdk/go/crypto"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

// ListInbox lists Phase-1 Manifest summaries in this device's Mode B mailbox without downloading blobs.
func (m *Manager) ListInbox(ctx context.Context, store *vault.Store) ([]models.SyncInboxItem, error) {
	cli, _, _, err := m.getOrCreateClient(store)
	if err != nil {
		return nil, FormatSyncError(err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, defaultControlTimeout)
	defer cancel()

	previews, err := cli.ListInbox(reqCtx, AppID)
	if err != nil {
		if envelope.IsCode(err, envelope.ErrCodeSpoolDisabled) {
			return []models.SyncInboxItem{}, nil
		}
		return nil, FormatSyncError(err)
	}

	ks := store.SyncKeyStore()
	out := make([]models.SyncInboxItem, 0, len(previews))
	for _, pv := range previews {
		item := models.SyncInboxItem{
			ItemID:         pv.ItemID,
			MsgID:          pv.Manifest.MsgID,
			SenderDeviceID: pv.Manifest.Sender.DeviceID,
			SenderLabel:    pv.Manifest.Sender.Label,
			SenderPlatform: pv.Manifest.Sender.Platform,
			CreatedAt:      pv.CreatedAt,
			ExpiresAt:      pv.ExpiresAt,
			TotalBytes:     pv.TotalBytes,
		}
		if card, _ := ks.GetDevice(pv.Manifest.Sender.DeviceID); card != nil && !card.Revoked {
			if strings.TrimSpace(card.Label) != "" {
				item.SenderLabel = card.Label
			}
			if strings.TrimSpace(card.Platform) != "" {
				item.SenderPlatform = card.Platform
			}
		} else {
			item.PairingBroken = true
		}
		if pv.Manifest.Schema != Schema {
			item.ErrorMessage = "不支持的数据格式: " + pv.Manifest.Schema
		} else {
			sc, kc, shc, tc, _, plat, dev := ParseManifestSummary(pv.Manifest)
			item.ServerCount = sc
			item.KeyCount = kc
			item.ShortcutCount = shc
			item.TemplateCount = tc
			if item.SenderPlatform == "" {
				item.SenderPlatform = plat
			}
			if item.SenderLabel == "" {
				item.SenderLabel = dev
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// StartWatchInbox starts a background SSE/Long-Poll inbox watcher emitting "sync:inbox_updated".
func (m *Manager) StartWatchInbox(ctx context.Context, store *vault.Store) error {
	cli, _, _, err := m.getOrCreateClient(store)
	if err != nil {
		return FormatSyncError(err)
	}

	m.watchMu.Lock()
	if m.watchCancel != nil && m.watchStore == store {
		m.watchMu.Unlock()
		return nil
	}
	if m.watchCancel != nil {
		m.watchCancel()
		m.watchCancel = nil
	}
	watchCtx, cancel := context.WithCancel(ctx)
	m.watchCancel = cancel
	m.watchStore = store
	m.watchMu.Unlock()

	go func() {
		_ = cli.WatchInbox(watchCtx, func(ev client.InboxWatchEvent) error {
			if watchCtx.Err() != nil || store.IsLocked() {
				return context.Canceled
			}
			m.emitEvent("sync:inbox_updated", map[string]any{
				"itemId":     ev.ItemID,
				"totalBytes": ev.TotalBytes,
				"createdAt":  ev.CreatedAt,
				"expiresAt":  ev.ExpiresAt,
			})
			return nil
		})
		m.watchMu.Lock()
		if m.watchStore == store {
			m.watchCancel = nil
			m.watchStore = nil
		}
		m.watchMu.Unlock()
	}()
	return nil
}

// StopWatchInbox cancels any running WatchInbox background stream.
func (m *Manager) StopWatchInbox() {
	m.watchMu.Lock()
	cancel := m.watchCancel
	m.watchCancel = nil
	m.watchStore = nil
	m.watchMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ApplyInboxItem downloads & verifies part_id=1 via FetchParcel, imports into vault.Store, and ACK-burns the relay item.
func (m *Manager) ApplyInboxItem(ctx context.Context, store *vault.Store, itemID, importMode string) (models.SyncImportSummary, error) {
	cleanMode := strings.ToLower(strings.TrimSpace(importMode))
	if cleanMode == "" {
		cleanMode = "merge"
	}
	if cleanMode != "merge" && cleanMode != "overwrite" {
		return models.SyncImportSummary{}, errors.New("无效的导入模式，请选择安全合并或覆盖导入")
	}

	cli, _, _, err := m.getOrCreateClient(store)
	if err != nil {
		return models.SyncImportSummary{}, FormatSyncError(err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var summary models.SyncImportSummary
	_, err = cli.FetchParcel(fetchCtx, strings.TrimSpace(itemID), func(p envelope.ReceivedParcel) error {
		bundle, decErr := DecodeReceivedParcel(p)
		if decErr != nil {
			return decErr
		}
		defer ZeroSyncBundle(&bundle)
		sum, impErr := store.ImportSyncBundle(bundle, cleanMode)
		if impErr != nil {
			return impErr
		}
		summary = sum
		return nil
	}, client.ReceiveOptions{AppID: AppID})
	if err != nil {
		return models.SyncImportSummary{}, FormatSyncError(err)
	}
	return summary, nil
}

// DismissInboxItem physically burns an unwanted spool item via Ed25519-signed /ack without downloading blobs.
func (m *Manager) DismissInboxItem(ctx context.Context, store *vault.Store, itemID string) error {
	cleanID := strings.TrimSpace(itemID)
	if cleanID == "" {
		return errors.New("收件箱条目 ID 不能为空")
	}
	cli, prof, _, err := m.getOrCreateClient(store)
	if err != nil {
		return FormatSyncError(err)
	}
	ks := store.SyncKeyStore()
	seed, err := ks.LoadOrCreateMasterSeed()
	if err != nil {
		return FormatSyncError(err)
	}
	dk, err := synccrypto.DeriveDeviceKeys(seed[:])
	clear(seed[:])
	if err != nil {
		return FormatSyncError(err)
	}
	defer dk.Zero()
	mk, err := dk.DeriveMailboxKeys(prof.RelayID)
	if err != nil {
		return FormatSyncError(err)
	}
	defer mk.Zero()

	reqCtx, cancel := context.WithTimeout(ctx, defaultControlTimeout)
	defer cancel()

	if err := ackSpoolItemDirect(reqCtx, prof, mk.MailboxID, mk.AuthPriv, cleanID, cli.ClockOffsetSec()); err != nil {
		return FormatSyncError(err)
	}
	if cached, _ := ks.GetCachedManifest(cleanID); cached != nil && cached.Manifest.MsgID != "" {
		_ = ks.MarkSeenMsgID(cached.Manifest.MsgID, cached.Manifest.CreatedAt, time.Now().Unix())
	}
	_ = ks.DeleteCachedManifest(cleanID)
	return nil
}

// RevokeDevice performs 2-phase single-device revocation on VaultSyncKeyStore and the active relay.
func (m *Manager) RevokeDevice(ctx context.Context, store *vault.Store, deviceID string) error {
	cli, _, _, err := m.getOrCreateClient(store)
	if err != nil {
		return FormatSyncError(err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, defaultControlTimeout)
	defer cancel()
	if err := cli.RevokeDevice(reqCtx, strings.TrimSpace(deviceID)); err != nil {
		return FormatSyncError(err)
	}
	return nil
}
