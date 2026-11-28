package syncsvc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/vault"
	"github.com/yellowdking/synctool/sdk/go/client"
	synccrypto "github.com/yellowdking/synctool/sdk/go/crypto"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

const (
	defaultControlTimeout = 20 * time.Second
	defaultOfferWaitMax   = 20 * time.Second
	defaultFlowTTLSec     = 300
	maxTrackedFlows       = 24
)

type relayCertHealth struct {
	expiresAt         int64
	rotateRecommended bool
}

// EventEmitter forwards real-time sync state/inbox events to Wails (when ctx != nil).
type EventEmitter func(eventName string, payload any)

// PreviewStager stages decrypted SyncBundles into the app's bounded TTL RAM preview cache.
type PreviewStager func(bundle models.SyncBundle) models.SyncReceivePreview

// Manager coordinates synctool-v1 Client instances, two-phase Inbox watching,
// interactive Mode A pairing/oneshot/direct flows, and instant teardown on LockVault/shutdown.
type Manager struct {
	mu      sync.Mutex
	emit    EventEmitter
	stageFn PreviewStager

	activeStore    *vault.Store
	activeClient   *client.Client
	activeRelayKey string
	activeTempDir  string

	certMu     sync.RWMutex
	certHealth map[string]relayCertHealth

	watchMu     sync.Mutex
	watchCancel context.CancelFunc
	watchStore  *vault.Store

	flowMu sync.Mutex
	flows  map[string]*flowSession
}

// NewManager creates a new Sync Session Manager.
func NewManager(emit EventEmitter, stageFn PreviewStager) *Manager {
	return &Manager{
		emit:       emit,
		stageFn:    stageFn,
		certHealth: make(map[string]relayCertHealth),
		flows:      make(map[string]*flowSession),
	}
}

func resolveHostLabel() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return truncateRunes(h, 64)
	}
	return defaultDeviceLabel()
}

func effectiveLocalLabel(ks *vault.VaultSyncKeyStore) (string, error) {
	lbl, err := ks.GetLocalLabel()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(lbl) != "" {
		return truncateRunes(lbl, 64), nil
	}
	return resolveHostLabel(), nil
}

func relayCacheKey(prof *envelope.RelayProfile, label string) string {
	if prof == nil {
		return ""
	}
	pins, _ := envelope.NormalizeSPKIPins(prof.SPKIPins, prof.SPKISHA256, prof.SPKIPin)
	return prof.Endpoint + "|" + prof.TLSMode + "|" + strings.Join(pins, ",") + "|" + prof.AccessKey + "|" + label
}

func (m *Manager) recordCertHealth(relayID string, expiresAt int64, rotate bool) {
	if relayID == "" || expiresAt <= 0 {
		return
	}
	m.certMu.Lock()
	m.certHealth[relayID] = relayCertHealth{
		expiresAt:         expiresAt,
		rotateRecommended: rotate,
	}
	m.certMu.Unlock()
}

func (m *Manager) getCertHealth(relayID string) relayCertHealth {
	m.certMu.RLock()
	defer m.certMu.RUnlock()
	return m.certHealth[relayID]
}

func (m *Manager) emitEvent(name string, payload any) {
	if m != nil && m.emit != nil {
		m.emit(name, payload)
	}
}

func (m *Manager) newSDKClient(store *vault.Store, prof *envelope.RelayProfile) (*client.Client, string, error) {
	ks := store.SyncKeyStore()
	label, err := effectiveLocalLabel(ks)
	if err != nil {
		return nil, "", err
	}
	tmpDir := filepath.Join(os.TempDir(), "gssh-synctool-"+uuid.NewString())
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return nil, "", envelope.WrapError(envelope.ErrCodeStorageIO, false, 0, "create synctool temp dir", err)
	}
	cli, err := client.New(client.Config{
		AppID:       AppID,
		DeviceLabel: label,
		Platform:    currentSyncPlatform(),
		Relay:       prof,
		KeyStore:    ks,
		TempDir:     tmpDir,
		OnCertExpiring: func(ev envelope.RelayCertExpiringEvent) {
			m.recordCertHealth(ev.RelayID, ev.ExpiresAt, true)
		},
	})
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, "", err
	}
	return cli, tmpDir, nil
}

func (m *Manager) getOrCreateClient(store *vault.Store) (*client.Client, *envelope.RelayProfile, string, error) {
	ks := store.SyncKeyStore()
	prof, mode, err := ResolveActiveRelay(ks)
	if err != nil {
		return nil, nil, "", err
	}
	label, err := effectiveLocalLabel(ks)
	if err != nil {
		return nil, nil, "", err
	}
	key := relayCacheKey(prof, label)

	m.mu.Lock()
	if m.activeClient != nil && m.activeStore == store && m.activeRelayKey == key {
		cli := m.activeClient
		m.mu.Unlock()
		return cli, prof, mode, nil
	}
	oldCli := m.activeClient
	oldTmp := m.activeTempDir
	m.activeClient = nil
	m.activeStore = nil
	m.activeRelayKey = ""
	m.activeTempDir = ""
	m.mu.Unlock()

	if oldCli != nil {
		oldCli.Close()
	}
	if oldTmp != "" {
		_ = os.RemoveAll(oldTmp)
	}

	cli, tmpDir, err := m.newSDKClient(store, prof)
	if err != nil {
		return nil, nil, "", err
	}

	m.mu.Lock()
	if m.activeClient != nil && m.activeStore == store && m.activeRelayKey == key {
		keep := m.activeClient
		m.mu.Unlock()
		cli.Close()
		_ = os.RemoveAll(tmpDir)
		return keep, prof, mode, nil
	}
	displacedCli := m.activeClient
	displacedTmp := m.activeTempDir
	m.activeStore = store
	m.activeClient = cli
	m.activeRelayKey = key
	m.activeTempDir = tmpDir
	m.mu.Unlock()

	if displacedCli != nil {
		displacedCli.Close()
	}
	if displacedTmp != "" {
		_ = os.RemoveAll(displacedTmp)
	}
	return cli, prof, mode, nil
}

func (m *Manager) resetActiveClient() {
	m.mu.Lock()
	oldCli := m.activeClient
	oldTmp := m.activeTempDir
	m.activeClient = nil
	m.activeStore = nil
	m.activeRelayKey = ""
	m.activeTempDir = ""
	m.mu.Unlock()

	if oldCli != nil {
		oldCli.Close()
	}
	if oldTmp != "" {
		_ = os.RemoveAll(oldTmp)
	}
}

// CloseAllOnLock cancels all active interactive flows and WatchInbox SSE streams,
// closes the active synctool Client (zeroing derived keys & wiping staging files),
// and clears RAM caches on VaultSyncKeyStore.
func (m *Manager) CloseAllOnLock() {
	if m == nil {
		return
	}
	m.StopWatchInbox()
	m.cancelAllFlows("已锁定配置文件，同步已中断")

	m.mu.Lock()
	st := m.activeStore
	m.mu.Unlock()

	m.resetActiveClient()
	if st != nil {
		st.SyncKeyStore().ClearRAMCaches()
	}
}

func toSyncPairedDevice(c envelope.DeviceCard) models.SyncPairedDevice {
	return models.SyncPairedDevice{
		DeviceID: c.DeviceID,
		Label:    c.Label,
		Platform: c.Platform,
		RelayID:  c.RelayID,
		IssuedAt: c.IssuedAt,
		PairedAt: c.PairedAt,
	}
}

// GetOverview returns local device ID/label, active relay status (secret-redacted), and paired devices.
func (m *Manager) GetOverview(store *vault.Store) (models.SyncOverview, error) {
	ks := store.SyncKeyStore()
	prof, mode, err := ResolveActiveRelay(ks)
	if err != nil {
		return models.SyncOverview{}, FormatSyncError(err)
	}
	seed, err := ks.LoadOrCreateMasterSeed()
	if err != nil {
		return models.SyncOverview{}, FormatSyncError(err)
	}
	dk, err := synccrypto.DeriveDeviceKeys(seed[:])
	clear(seed[:])
	if err != nil {
		return models.SyncOverview{}, FormatSyncError(err)
	}
	deviceID := dk.DeviceID
	dk.Zero()

	label, err := effectiveLocalLabel(ks)
	if err != nil {
		return models.SyncOverview{}, FormatSyncError(err)
	}
	cards, err := ks.ListDevices()
	if err != nil {
		return models.SyncOverview{}, FormatSyncError(err)
	}
	devices := make([]models.SyncPairedDevice, 0, len(cards))
	for _, c := range cards {
		devices = append(devices, toSyncPairedDevice(c))
	}
	ch := m.getCertHealth(prof.RelayID)
	return models.SyncOverview{
		LocalDeviceID:    deviceID,
		LocalDeviceLabel: label,
		Relay:            BuildRelayStatus(prof, mode, ch.expiresAt, ch.rotateRecommended),
		PairedDevices:    devices,
	}, nil
}

// SetCustomRelay validates and persists a custom synctool://relay URI (or resets to default when rawURI == "").
func (m *Manager) SetCustomRelay(store *vault.Store, rawURI string) (models.SyncRelayStatus, error) {
	ks := store.SyncKeyStore()
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		if err := ks.SetCustomRelay(nil); err != nil {
			return models.SyncRelayStatus{}, FormatSyncError(err)
		}
		m.StopWatchInbox()
		m.resetActiveClient()
		def, err := BuiltInRelayProfile()
		if err != nil {
			return models.SyncRelayStatus{}, FormatSyncError(err)
		}
		ch := m.getCertHealth(def.RelayID)
		return BuildRelayStatus(def, "default", ch.expiresAt, ch.rotateRecommended), nil
	}

	prof, err := envelope.ParseRelayURI(trimmed)
	if err != nil {
		return models.SyncRelayStatus{}, FormatSyncError(err)
	}
	if err := ks.SetCustomRelay(prof); err != nil {
		return models.SyncRelayStatus{}, FormatSyncError(err)
	}
	m.StopWatchInbox()
	m.resetActiveClient()
	ch := m.getCertHealth(prof.RelayID)
	return BuildRelayStatus(prof, "custom", ch.expiresAt, ch.rotateRecommended), nil
}

// TestRelay probes connectivity, capabilities, RTT, clock skew, and TLS cert expiration on rawURI (or active relay if empty).
func (m *Manager) TestRelay(ctx context.Context, store *vault.Store, rawURI string) (models.SyncPingInfo, error) {
	var (
		prof *envelope.RelayProfile
		err  error
	)
	if trimmed := strings.TrimSpace(rawURI); trimmed != "" {
		prof, err = envelope.ParseRelayURI(trimmed)
		if err != nil {
			return models.SyncPingInfo{}, FormatSyncError(err)
		}
	} else {
		if store == nil || store.IsLocked() {
			return models.SyncPingInfo{}, errors.New("配置文件已锁定，请先输入解锁密码")
		}
		prof, _, err = ResolveActiveRelay(store.SyncKeyStore())
		if err != nil {
			return models.SyncPingInfo{}, FormatSyncError(err)
		}
	}

	tmpCli, err := client.New(client.Config{
		AppID:    AppID,
		Platform: currentSyncPlatform(),
		Relay:    prof,
		KeyStore: envelope.NewMemoryKeyStore(),
	})
	if err != nil {
		return models.SyncPingInfo{}, FormatSyncError(err)
	}
	defer tmpCli.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	pingRes, err := tmpCli.Ping(pingCtx)
	if err != nil {
		return models.SyncPingInfo{}, FormatSyncError(err)
	}
	spoolMode := pingRes.SpoolBackend
	if caps, cErr := tmpCli.Capabilities(pingCtx); cErr == nil && caps != nil {
		if spoolMode == "" {
			spoolMode = caps.Spool.Backend
		}
		if !caps.Spool.Enabled && spoolMode == "" {
			spoolMode = "off"
		}
	}
	if spoolMode == "" {
		spoolMode = "off"
	}
	m.recordCertHealth(prof.RelayID, pingRes.CertExpiresAt, pingRes.CertRotateRecommended)

	return models.SyncPingInfo{
		RTTMs:                 pingRes.RTTMs,
		Profile:               pingRes.Profile,
		SpoolMode:             spoolMode,
		ClockOffsetSec:        pingRes.ClockOffsetSec,
		CertExpiresAt:         formatUnixRFC3339(pingRes.CertExpiresAt),
		CertRotateRecommended: pingRes.CertRotateRecommended,
	}, nil
}

// SetDeviceLabel updates the local device's human-readable label (1..64 UTF-8 chars, or "" to reset to hostname).
func (m *Manager) SetDeviceLabel(store *vault.Store, label string) error {
	trimmed := strings.TrimSpace(label)
	if trimmed != "" && utf8.RuneCountInString(trimmed) > 64 {
		return errors.New("设备名称长度不能超过 64 个字符")
	}
	if err := store.SyncKeyStore().SetLocalLabel(trimmed); err != nil {
		return FormatSyncError(err)
	}
	m.resetActiveClient()
	return nil
}
