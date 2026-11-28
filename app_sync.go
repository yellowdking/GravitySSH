package main

import (
	"errors"
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func (a *App) cancelAllSyncOnLock() {
	if a.syncMgr != nil {
		a.syncMgr.CloseAllOnLock()
	}
	if a.syncCache != nil {
		a.syncCache.clear("")
	}
}

func (a *App) buildSyncPreview(bundle models.SyncBundle) models.SyncReceivePreview {
	if a.syncCache == nil {
		zeroSyncBundle(&bundle)
		return models.SyncReceivePreview{}
	}
	sc := len(bundle.Servers)
	kc := len(bundle.SSHKeys)
	shc := len(bundle.Shortcuts)
	tc := len(bundle.Templates)
	ver := bundle.SchemaVersion
	exp := bundle.ExportedAt
	plat := bundle.SourcePlatform
	dev := bundle.SourceDevice
	id := a.syncCache.put(bundle)
	if id == "" {
		return models.SyncReceivePreview{}
	}
	return models.SyncReceivePreview{
		PreviewID:      id,
		SchemaVersion:  ver,
		ExportedAt:     exp,
		SourcePlatform: plat,
		SourceDevice:   dev,
		ServerCount:    sc,
		KeyCount:       kc,
		ShortcutCount:  shc,
		TemplateCount:  tc,
	}
}

// GetSyncOverview returns local device sync identity, active relay status (secret-redacted), and paired devices.
func (a *App) GetSyncOverview() (models.SyncOverview, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncOverview{}, err
	}
	return a.syncMgr.GetOverview(store)
}

// SetCustomSyncRelay validates and saves a custom synctool://relay URI inside encrypted VaultData.Sync
// (or resets back to the built-in default relay when rawURI is empty).
func (a *App) SetCustomSyncRelay(rawURI string) (models.SyncRelayStatus, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncRelayStatus{}, err
	}
	return a.syncMgr.SetCustomRelay(store, rawURI)
}

// TestSyncRelay checks TLS 1.3 + SPKI pin + HMAC connectivity and capabilities for rawURI (or active relay when empty).
func (a *App) TestSyncRelay(rawURI string) (models.SyncPingInfo, error) {
	if strings.TrimSpace(rawURI) != "" {
		if err := a.ensureReady(); err != nil {
			return models.SyncPingInfo{}, err
		}
		return a.syncMgr.TestRelay(a.bgContext(), nil, rawURI)
	}
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncPingInfo{}, err
	}
	return a.syncMgr.TestRelay(a.bgContext(), store, "")
}

// SetSyncDeviceLabel updates the human-readable device label advertised during pairing and sync.
func (a *App) SetSyncDeviceLabel(label string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return a.syncMgr.SetDeviceLabel(store, label)
}

// ListSyncInbox lists Phase-1 decrypted Manifest summaries from the Mode B mailbox without downloading blobs.
func (a *App) ListSyncInbox() ([]models.SyncInboxItem, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return nil, err
	}
	return a.syncMgr.ListInbox(a.bgContext(), store)
}

// StartWatchSyncInbox starts background real-time SSE/Long-Poll inbox watching ("sync:inbox_updated").
func (a *App) StartWatchSyncInbox() error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return a.syncMgr.StartWatchInbox(a.bgContext(), store)
}

// StopWatchSyncInbox stops any running background inbox watcher.
func (a *App) StopWatchSyncInbox() {
	if a.syncMgr != nil {
		a.syncMgr.StopWatchInbox()
	}
}

// SendSyncToDevice sends an encrypted 0-RTT vault parcel to a paired device ("auto" | "spool" | "pipe").
func (a *App) SendSyncToDevice(deviceID string, deliveryMode string) (models.SyncDirectSendResult, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncDirectSendResult{}, err
	}
	return a.syncMgr.SendToDevice(a.bgContext(), store, deviceID, deliveryMode)
}

// ApplyInboxSync fetches part_id=1 for itemID, imports it into the vault, and sends a signed ACK burn.
func (a *App) ApplyInboxSync(itemID string, importMode string) (models.SyncImportSummary, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncImportSummary{}, err
	}
	return a.syncMgr.ApplyInboxItem(a.bgContext(), store, itemID, importMode)
}

// DismissInboxItem physically burns an unwanted spool item on the relay via signed ACK.
func (a *App) DismissInboxItem(itemID string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return a.syncMgr.DismissInboxItem(a.bgContext(), store, itemID)
}

// RevokeSyncDevice revokes a paired device locally and deletes its deposit token on the relay.
func (a *App) RevokeSyncDevice(deviceID string) error {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return err
	}
	return a.syncMgr.RevokeDevice(a.bgContext(), store, deviceID)
}

// StartSyncDeviceBind initiates bidirectional device pairing (mode=1 QR or mode=2 8-char+SAS).
func (a *App) StartSyncDeviceBind(mode int, includeRelay bool) (models.SyncFlowSession, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	return a.syncMgr.StartDeviceBind(a.bgContext(), store, mode, includeRelay)
}

// AcceptSyncDeviceBind joins a pairing offer as Responder (synctool://pair URI or 8-char room code).
func (a *App) AcceptSyncDeviceBind(codeOrURI string) (models.SyncFlowSession, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	return a.syncMgr.AcceptDeviceBind(a.bgContext(), store, codeOrURI)
}

// StartSyncOneShotSend initiates a zero-disk streaming one-shot transfer (mode=1 QR or mode=2 8-char+SAS).
func (a *App) StartSyncOneShotSend(mode int, includeRelay bool) (models.SyncFlowSession, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	return a.syncMgr.StartOneShotSend(a.bgContext(), store, mode, includeRelay)
}

// StartSyncReceive joins a Mode A room (oneshot m=1/2 or paired direct m=0) and stages a receive preview.
func (a *App) StartSyncReceive(codeOrURI string) (models.SyncFlowSession, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncFlowSession{}, err
	}
	return a.syncMgr.StartReceive(a.bgContext(), store, codeOrURI)
}

// GetSyncFlowStatus polls the current status of an interactive sync flow session.
func (a *App) GetSyncFlowStatus(sessionID string) (models.SyncFlowStatus, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.SyncFlowStatus{}, err
	}
	return a.syncMgr.GetFlowStatus(sessionID)
}

// SubmitSyncSASInput submits the 6-digit SAS code entered by the Initiator on m=2 flows.
func (a *App) SubmitSyncSASInput(sessionID string, sasCode string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.syncMgr.SubmitSASInput(sessionID, sasCode)
}

// ConfirmSyncSAS submits the Responder's mandatory visual SAS confirmation decision on m=2 flows.
func (a *App) ConfirmSyncSAS(sessionID string, approved bool) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return a.syncMgr.ConfirmSAS(sessionID, approved)
}

// CancelSyncFlow cancels an active interactive sync flow and closes its relay room.
func (a *App) CancelSyncFlow(sessionID string) error {
	if a.syncMgr == nil {
		return nil
	}
	return a.syncMgr.CancelFlow(sessionID)
}

// ApplySyncReceive imports a RAM-staged SyncReceivePreview ("merge" | "overwrite") and clears it on success.
func (a *App) ApplySyncReceive(previewID string, mode string) (models.SyncImportSummary, error) {
	store, _, _, err := a.unlockedDeps()
	if err != nil {
		return models.SyncImportSummary{}, err
	}
	if a.syncCache == nil {
		return models.SyncImportSummary{}, errors.New("同步缓存未初始化")
	}
	bundle, ok := a.syncCache.peekClone(previewID)
	if !ok {
		return models.SyncImportSummary{}, errors.New("预览已过期或已导入，请重新接收")
	}
	defer zeroSyncBundle(&bundle)

	summary, err := store.ImportSyncBundle(bundle, mode)
	if err != nil {
		return models.SyncImportSummary{}, err
	}
	a.syncCache.clear(previewID)
	return summary, nil
}

// ClearSyncPreview discards a pending decrypted sync preview from memory (or all when previewID is empty).
func (a *App) ClearSyncPreview(previewID string) {
	if a.syncCache != nil {
		a.syncCache.clear(previewID)
	}
}
