package vault

import (
	"encoding/base64"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	synccrypto "github.com/yellowdking/synctool/sdk/go/crypto"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

func cloneRelayProfilePtr(p *envelope.RelayProfile) *envelope.RelayProfile {
	if p == nil {
		return nil
	}
	cp := *p
	if len(p.SPKIPins) > 0 {
		cp.SPKIPins = append([]string(nil), p.SPKIPins...)
	}
	return &cp
}

func cloneVaultSyncState(src models.VaultSyncState) models.VaultSyncState {
	out := models.VaultSyncState{
		MasterSeedB64U:      src.MasterSeedB64U,
		LocalLabel:          src.LocalLabel,
		ReplayWatermarkTS:   src.ReplayWatermarkTS,
		CustomRelay:         cloneRelayProfilePtr(src.CustomRelay),
		Devices:             make(map[string]envelope.DeviceCard, len(src.Devices)),
		PendingTokenDeletes: append([]string(nil), src.PendingTokenDeletes...),
		SeenMsgs:            make(map[string]envelope.SeenMsgEntry, len(src.SeenMsgs)),
	}
	for k, v := range src.Devices {
		out.Devices[k] = v
	}
	for k, v := range src.SeenMsgs {
		out.SeenMsgs[k] = v
	}
	return out
}

func normalizeSyncState(st *models.VaultSyncState) {
	if st == nil {
		return
	}
	st.LocalLabel = strings.TrimSpace(st.LocalLabel)
	if st.CustomRelay != nil {
		_ = st.CustomRelay.SyncPins()
	}
	normDevs := make(map[string]envelope.DeviceCard, len(st.Devices))
	for id, dc := range st.Devices {
		normID := strings.ToLower(strings.TrimSpace(dc.DeviceID))
		if normID == "" {
			normID = strings.ToLower(strings.TrimSpace(id))
		}
		if normID == "" {
			continue
		}
		dc.DeviceID = normID
		normDevs[normID] = dc
	}
	st.Devices = normDevs

	normSeen := make(map[string]envelope.SeenMsgEntry, len(st.SeenMsgs))
	for id, sm := range st.SeenMsgs {
		normID := strings.ToLower(strings.TrimSpace(sm.MsgID))
		if normID == "" {
			normID = strings.ToLower(strings.TrimSpace(id))
		}
		if normID == "" {
			continue
		}
		sm.MsgID = normID
		normSeen[normID] = sm
	}
	st.SeenMsgs = normSeen

	if len(st.PendingTokenDeletes) > 0 {
		cleanPending := make([]string, 0, len(st.PendingTokenDeletes))
		for _, tok := range st.PendingTokenDeletes {
			t := strings.TrimSpace(tok)
			if t != "" && !slices.Contains(cleanPending, t) {
				cleanPending = append(cleanPending, t)
			}
		}
		st.PendingTokenDeletes = cleanPending
	}
}

// VaultSyncKeyStore implements envelope.KeyStore (+ 2-phase revoke & replay watermark extensions)
// backed directly by the DPAPI + AES-256-GCM encrypted vault.Store.
// ManifestCache and TransferCheckpoints live exclusively in RAM and are wiped on vault lock/close.
type VaultSyncKeyStore struct {
	store *Store

	ramMu         sync.Mutex
	manifestCache map[string]envelope.CachedManifestEntry
	checkpoints   map[string]envelope.TransferCheckpoint
}

var _ envelope.KeyStore = (*VaultSyncKeyStore)(nil)

func newVaultSyncKeyStore(s *Store) *VaultSyncKeyStore {
	return &VaultSyncKeyStore{
		store:         s,
		manifestCache: make(map[string]envelope.CachedManifestEntry),
		checkpoints:   make(map[string]envelope.TransferCheckpoint),
	}
}

// SyncKeyStore returns the Store's encrypted VaultSyncKeyStore instance.
func (s *Store) SyncKeyStore() *VaultSyncKeyStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncKS == nil {
		s.syncKS = newVaultSyncKeyStore(s)
	}
	return s.syncKS
}

// ClearRAMCaches immediately wipes in-memory ManifestCache and TransferCheckpoints.
func (ks *VaultSyncKeyStore) ClearRAMCaches() {
	if ks == nil {
		return
	}
	ks.ramMu.Lock()
	clear(ks.manifestCache)
	clear(ks.checkpoints)
	ks.ramMu.Unlock()
}

func keystoreLockedOrClosedErr(closed, locked bool) error {
	if closed {
		return envelope.WrapError(envelope.ErrCodeKeystoreError, false, 0, "vault store is closed", errVaultClosed)
	}
	if locked {
		return envelope.WrapError(envelope.ErrCodeKeystoreError, false, 0, "vault store is locked", errVaultLocked)
	}
	return nil
}

func (ks *VaultSyncKeyStore) checkAccessible() error {
	if ks == nil || ks.store == nil {
		return envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	ks.store.mu.RLock()
	closed := ks.store.closed
	locked := ks.store.locked
	ks.store.mu.RUnlock()
	return keystoreLockedOrClosedErr(closed, locked)
}

func (ks *VaultSyncKeyStore) mutateSync(fn func(st *models.VaultSyncState) (changed bool, err error)) error {
	if ks == nil || ks.store == nil {
		return envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	s := ks.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := keystoreLockedOrClosedErr(s.closed, s.locked); err != nil {
		return err
	}
	if s.data.Sync.Devices == nil {
		s.data.Sync.Devices = make(map[string]envelope.DeviceCard)
	}
	if s.data.Sync.SeenMsgs == nil {
		s.data.Sync.SeenMsgs = make(map[string]envelope.SeenMsgEntry)
	}
	backup := cloneVaultSyncState(s.data.Sync)
	changed, err := fn(&s.data.Sync)
	if err != nil {
		s.data.Sync = backup
		return err
	}
	if !changed {
		return nil
	}
	if saveErr := s.saveLocked(); saveErr != nil {
		s.data.Sync = backup
		return envelope.WrapError(envelope.ErrCodeKeystoreError, false, 0, "persist vault sync state", saveErr)
	}
	return nil
}

// GetCustomRelay returns a cloned copy of VaultData.Sync.CustomRelay (or nil when using built-in default).
func (ks *VaultSyncKeyStore) GetCustomRelay() (*envelope.RelayProfile, error) {
	if ks == nil || ks.store == nil {
		return nil, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return nil, err
	}
	return cloneRelayProfilePtr(ks.store.data.Sync.CustomRelay), nil
}

// SetCustomRelay saves a custom RelayProfile into encrypted VaultData.Sync.CustomRelay,
// or clears it back to nil when prof == nil.
func (ks *VaultSyncKeyStore) SetCustomRelay(prof *envelope.RelayProfile) error {
	var clean *envelope.RelayProfile
	if prof != nil {
		cp := *cloneRelayProfilePtr(prof)
		if err := cp.SyncPins(); err != nil {
			return err
		}
		clean = &cp
	}
	return ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		st.CustomRelay = clean
		return true, nil
	})
}

func (ks *VaultSyncKeyStore) LoadOrCreateMasterSeed() ([32]byte, error) {
	var zero [32]byte
	if ks == nil || ks.store == nil {
		return zero, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	s := ks.store

	s.mu.RLock()
	closed, locked := s.closed, s.locked
	existingB64 := s.data.Sync.MasterSeedB64U
	s.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(closed, locked); err != nil {
		return zero, err
	}
	if existingB64 != "" {
		raw, err := base64.RawURLEncoding.DecodeString(existingB64)
		if err != nil || len(raw) != 32 {
			clear(raw)
			return zero, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "corrupted master seed in vault", nil)
		}
		var out [32]byte
		copy(out[:], raw)
		clear(raw)
		return out, nil
	}

	var out [32]byte
	err := ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		if st.MasterSeedB64U != "" {
			raw, err := base64.RawURLEncoding.DecodeString(st.MasterSeedB64U)
			if err != nil || len(raw) != 32 {
				clear(raw)
				return false, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "corrupted master seed in vault", nil)
			}
			copy(out[:], raw)
			clear(raw)
			return false, nil
		}
		gen, err := synccrypto.GenerateMasterSeed()
		if err != nil {
			return false, envelope.WrapError(envelope.ErrCodeKeystoreError, false, 0, "generate master seed", err)
		}
		st.MasterSeedB64U = base64.RawURLEncoding.EncodeToString(gen[:])
		out = gen
		return true, nil
	})
	if err != nil {
		clear(out[:])
		return zero, err
	}
	return out, nil
}

func (ks *VaultSyncKeyStore) GetLocalLabel() (string, error) {
	if ks == nil || ks.store == nil {
		return "", envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return "", err
	}
	return ks.store.data.Sync.LocalLabel, nil
}

func (ks *VaultSyncKeyStore) SetLocalLabel(label string) error {
	trimmed := strings.TrimSpace(label)
	return ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		if st.LocalLabel == trimmed {
			return false, nil
		}
		st.LocalLabel = trimmed
		return true, nil
	})
}

func (ks *VaultSyncKeyStore) GetRelayProfile(relayIDOrAlias string) (*envelope.RelayProfile, error) {
	custom, err := ks.GetCustomRelay()
	if err != nil || custom == nil {
		return nil, err
	}
	q := strings.TrimSpace(relayIDOrAlias)
	if custom.RelayID == q || (custom.Alias != "" && strings.EqualFold(custom.Alias, q)) || strings.EqualFold(q, "custom") {
		return custom, nil
	}
	return nil, nil
}

func (ks *VaultSyncKeyStore) GetDefaultRelayProfile() (*envelope.RelayProfile, error) {
	return ks.GetCustomRelay()
}

func (ks *VaultSyncKeyStore) ListRelayProfiles() ([]envelope.RelayProfile, error) {
	custom, err := ks.GetCustomRelay()
	if err != nil {
		return nil, err
	}
	if custom == nil {
		return []envelope.RelayProfile{}, nil
	}
	return []envelope.RelayProfile{*custom}, nil
}

func (ks *VaultSyncKeyStore) SaveRelayProfile(profile envelope.RelayProfile) error {
	return ks.SetCustomRelay(&profile)
}

func (ks *VaultSyncKeyStore) ListDevices() ([]envelope.DeviceCard, error) {
	if ks == nil || ks.store == nil {
		return nil, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return nil, err
	}
	out := make([]envelope.DeviceCard, 0, len(ks.store.data.Sync.Devices))
	for _, c := range ks.store.data.Sync.Devices {
		if !c.Revoked {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PairedAt != out[j].PairedAt {
			return out[i].PairedAt < out[j].PairedAt
		}
		return out[i].DeviceID < out[j].DeviceID
	})
	return out, nil
}

// ListAllDevices returns all stored DeviceCards including tombstoned Revoked=true cards awaiting relay token deletion.
func (ks *VaultSyncKeyStore) ListAllDevices() ([]envelope.DeviceCard, error) {
	if ks == nil || ks.store == nil {
		return nil, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return nil, err
	}
	out := make([]envelope.DeviceCard, 0, len(ks.store.data.Sync.Devices))
	for _, c := range ks.store.data.Sync.Devices {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PairedAt != out[j].PairedAt {
			return out[i].PairedAt < out[j].PairedAt
		}
		return out[i].DeviceID < out[j].DeviceID
	})
	return out, nil
}

func (ks *VaultSyncKeyStore) GetDevice(deviceID string) (*envelope.DeviceCard, error) {
	if ks == nil || ks.store == nil {
		return nil, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	id := strings.ToLower(strings.TrimSpace(deviceID))
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return nil, err
	}
	if c, ok := ks.store.data.Sync.Devices[id]; ok {
		cp := c
		return &cp, nil
	}
	return nil, nil
}

func (ks *VaultSyncKeyStore) PutDevice(card envelope.DeviceCard) error {
	card.DeviceID = strings.ToLower(strings.TrimSpace(card.DeviceID))
	if card.DeviceID == "" {
		return envelope.NewError(envelope.ErrCodeBadRequest, false, 0, "empty device_id", nil)
	}
	return ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		st.Devices[card.DeviceID] = card
		return true, nil
	})
}

func (ks *VaultSyncKeyStore) DeleteDevice(deviceID string) error {
	id := strings.ToLower(strings.TrimSpace(deviceID))
	return ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		if _, exists := st.Devices[id]; !exists {
			return false, nil
		}
		delete(st.Devices, id)
		return true, nil
	})
}

// AddPendingTokenDelete queues a superseded or revoked deposit token_id for 2-phase relay DELETE cleanup.
func (ks *VaultSyncKeyStore) AddPendingTokenDelete(tokenID string) error {
	tok := strings.TrimSpace(tokenID)
	if tok == "" {
		return nil
	}
	return ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		if slices.Contains(st.PendingTokenDeletes, tok) {
			return false, nil
		}
		st.PendingTokenDeletes = append(st.PendingTokenDeletes, tok)
		return true, nil
	})
}

// ListPendingTokenDeletes returns queued deposit token_ids awaiting relay DELETE cleanup.
func (ks *VaultSyncKeyStore) ListPendingTokenDeletes() ([]string, error) {
	if ks == nil || ks.store == nil {
		return nil, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return nil, err
	}
	return append([]string(nil), ks.store.data.Sync.PendingTokenDeletes...), nil
}

// RemovePendingTokenDelete removes a cleaned-up deposit token_id from the pending queue.
func (ks *VaultSyncKeyStore) RemovePendingTokenDelete(tokenID string) error {
	tok := strings.TrimSpace(tokenID)
	if tok == "" {
		return nil
	}
	return ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		idx := slices.Index(st.PendingTokenDeletes, tok)
		if idx < 0 {
			return false, nil
		}
		st.PendingTokenDeletes = slices.Delete(st.PendingTokenDeletes, idx, idx+1)
		return true, nil
	})
}

// GetReplayWatermark returns the highest CreatedAt timestamp evicted from SeenMsgs.
func (ks *VaultSyncKeyStore) GetReplayWatermark() (int64, error) {
	if ks == nil || ks.store == nil {
		return 0, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return 0, err
	}
	return ks.store.data.Sync.ReplayWatermarkTS, nil
}

func (ks *VaultSyncKeyStore) HasSeenMsgID(msgID string) (bool, error) {
	if ks == nil || ks.store == nil {
		return false, envelope.NewError(envelope.ErrCodeKeystoreError, false, 0, "vault store is nil", nil)
	}
	id := strings.ToLower(strings.TrimSpace(msgID))
	ks.store.mu.RLock()
	defer ks.store.mu.RUnlock()
	if err := keystoreLockedOrClosedErr(ks.store.closed, ks.store.locked); err != nil {
		return false, err
	}
	_, ok := ks.store.data.Sync.SeenMsgs[id]
	return ok, nil
}

func (ks *VaultSyncKeyStore) MarkSeenMsgID(msgID string, createdAt, seenAt int64) error {
	id := strings.ToLower(strings.TrimSpace(msgID))
	if id == "" {
		return nil
	}
	return ks.mutateSync(func(st *models.VaultSyncState) (bool, error) {
		st.SeenMsgs[id] = envelope.SeenMsgEntry{
			MsgID:     id,
			CreatedAt: createdAt,
			SeenAt:    seenAt,
		}
		if len(st.SeenMsgs) > envelope.DefaultSeenMsgMinEntries {
			cutoff := time.Now().Unix() - int64(envelope.DefaultSeenMsgRetentionDays*86400)
			entries := make([]envelope.SeenMsgEntry, 0, len(st.SeenMsgs))
			for _, e := range st.SeenMsgs {
				entries = append(entries, e)
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].SeenAt < entries[j].SeenAt })
			for _, e := range entries {
				needCapEvict := len(st.SeenMsgs) > envelope.DefaultSeenMsgMaxEntries
				if !needCapEvict && len(st.SeenMsgs) <= envelope.DefaultSeenMsgMinEntries {
					break
				}
				if needCapEvict || e.SeenAt < cutoff {
					if e.CreatedAt > st.ReplayWatermarkTS {
						st.ReplayWatermarkTS = e.CreatedAt
					}
					delete(st.SeenMsgs, e.MsgID)
				}
			}
		}
		return true, nil
	})
}

func (ks *VaultSyncKeyStore) GetCachedManifest(itemID string) (*envelope.CachedManifestEntry, error) {
	if err := ks.checkAccessible(); err != nil {
		return nil, err
	}
	ks.ramMu.Lock()
	defer ks.ramMu.Unlock()
	if e, ok := ks.manifestCache[itemID]; ok {
		cp := e
		return &cp, nil
	}
	return nil, nil
}

func (ks *VaultSyncKeyStore) PutCachedManifest(entry envelope.CachedManifestEntry) error {
	if err := ks.checkAccessible(); err != nil {
		return err
	}
	ks.ramMu.Lock()
	ks.manifestCache[entry.ItemID] = entry
	ks.ramMu.Unlock()
	return nil
}

func (ks *VaultSyncKeyStore) DeleteCachedManifest(itemID string) error {
	if err := ks.checkAccessible(); err != nil {
		return err
	}
	ks.ramMu.Lock()
	delete(ks.manifestCache, itemID)
	ks.ramMu.Unlock()
	return nil
}

func (ks *VaultSyncKeyStore) PruneCachedManifests(activeItemIDs map[string]struct{}) error {
	if err := ks.checkAccessible(); err != nil {
		return err
	}
	ks.ramMu.Lock()
	for id := range ks.manifestCache {
		if _, ok := activeItemIDs[id]; !ok {
			delete(ks.manifestCache, id)
		}
	}
	ks.ramMu.Unlock()
	return nil
}

func syncCheckpointKey(direction, msgID string, partID uint32) string {
	return direction + ":" + strings.ToLower(strings.TrimSpace(msgID)) + ":" + strconv.FormatUint(uint64(partID), 10)
}

func (ks *VaultSyncKeyStore) GetCheckpoint(direction, msgID string, partID uint32) (*envelope.TransferCheckpoint, error) {
	if err := ks.checkAccessible(); err != nil {
		return nil, err
	}
	k := syncCheckpointKey(direction, msgID, partID)
	now := time.Now().Unix()
	ks.ramMu.Lock()
	defer ks.ramMu.Unlock()
	if cp, ok := ks.checkpoints[k]; ok {
		if cp.ExpiresAt > 0 && cp.ExpiresAt < now {
			delete(ks.checkpoints, k)
			return nil, nil
		}
		out := cp
		return &out, nil
	}
	return nil, nil
}

func (ks *VaultSyncKeyStore) PutCheckpoint(cp envelope.TransferCheckpoint) error {
	if err := ks.checkAccessible(); err != nil {
		return err
	}
	cp.Key = syncCheckpointKey(cp.Direction, cp.MsgID, cp.PartID)
	ks.ramMu.Lock()
	ks.checkpoints[cp.Key] = cp
	ks.ramMu.Unlock()
	return nil
}

func (ks *VaultSyncKeyStore) DeleteCheckpoint(direction, msgID string, partID uint32) error {
	if err := ks.checkAccessible(); err != nil {
		return err
	}
	k := syncCheckpointKey(direction, msgID, partID)
	ks.ramMu.Lock()
	delete(ks.checkpoints, k)
	ks.ramMu.Unlock()
	return nil
}
