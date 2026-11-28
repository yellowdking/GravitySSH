package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/crypto"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"github.com/yellowdking/synctool/sdk/go/envelope"
)

func TestVaultSchemaV6LegacyRelayMigrationAndSecretIsolation(t *testing.T) {
	enc := crypto.NewEnvelopeCrypto(nil)

	// 1. Legacy v5 vault with bare gssh-sync-v2 URL -> must clear SyncRelayURL and leave CustomRelay nil.
	barePath := filepath.Join(t.TempDir(), "legacy_bare.gsv")
	bareRaw := []byte(`{"version":5,"settings":{"syncRelayUrl":"https://43.155.231.14:54843"}}`)
	bareSealed, err := enc.Seal(bareRaw)
	if err != nil {
		t.Fatalf("seal bare legacy vault: %v", err)
	}
	if err := os.WriteFile(barePath, bareSealed, 0o600); err != nil {
		t.Fatal(err)
	}
	stBare, err := Open(barePath, nil)
	if err != nil {
		t.Fatalf("Open bare legacy vault: %v", err)
	}
	defer stBare.Close()
	if stBare.LockState().SchemaVersion != 6 {
		t.Fatalf("expected schemaVersion=6, got %d", stBare.LockState().SchemaVersion)
	}
	if got := stBare.GetSettings().SyncRelayURL; got != "" {
		t.Fatalf("expected Settings.SyncRelayURL cleared, got %q", got)
	}
	if custom, err := stBare.SyncKeyStore().GetCustomRelay(); err != nil || custom != nil {
		t.Fatalf("expected nil CustomRelay for legacy bare URL, got %+v err=%v", custom, err)
	}

	// 2. Legacy v5 vault with valid synctool://relay URI -> migrates to Sync.CustomRelay and clears Settings.SyncRelayURL.
	validPin := strings.Repeat("a1", 32)
	validKey := "custom_access_key_with_at_least_32_characters_xyz"
	validURI := fmt.Sprintf("synctool://relay?v=1&u=https%%3A%%2F%%2F198.51.100.7%%3A54843&pin=%s#k=%s", validPin, validKey)
	uriPath := filepath.Join(t.TempDir(), "legacy_uri.gsv")
	uriRaw, _ := json.Marshal(map[string]any{
		"version":  5,
		"settings": map[string]any{"syncRelayUrl": validURI},
	})
	uriSealed, err := enc.Seal(uriRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uriPath, uriSealed, 0o600); err != nil {
		t.Fatal(err)
	}
	stURI, err := Open(uriPath, nil)
	if err != nil {
		t.Fatalf("Open valid URI legacy vault: %v", err)
	}
	defer stURI.Close()
	if stURI.GetSettings().SyncRelayURL != "" {
		t.Fatalf("expected Settings.SyncRelayURL cleared after migration")
	}
	migrated, err := stURI.SyncKeyStore().GetCustomRelay()
	if err != nil || migrated == nil || migrated.Endpoint != "https://198.51.100.7:54843" || migrated.AccessKey != validKey || migrated.SPKISHA256 != validPin {
		t.Fatalf("unexpected migrated CustomRelay: %+v err=%v", migrated, err)
	}

	// SaveSettings must never persist SyncRelayURL in plaintext settings.
	cfg := stURI.GetSettings()
	cfg.SyncRelayURL = validURI
	savedCfg, err := stURI.SaveSettings(cfg)
	if err != nil || savedCfg.SyncRelayURL != "" || stURI.GetSettings().SyncRelayURL != "" {
		t.Fatalf("SaveSettings must strip SyncRelayURL: saved=%+v err=%v", savedCfg, err)
	}
}

func TestVaultSyncKeyStorePersistenceOverwriteSafetyAndLockWipe(t *testing.T) {
	vp := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vp, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	ks := store.SyncKeyStore()

	seed1, err := ks.LoadOrCreateMasterSeed()
	if err != nil {
		t.Fatalf("LoadOrCreateMasterSeed failed: %v", err)
	}
	seed2, err := ks.LoadOrCreateMasterSeed()
	if err != nil || seed1 != seed2 {
		t.Fatalf("expected deterministic master seed across calls")
	}
	if err := ks.SetLocalLabel("  Office-Workstation  "); err != nil {
		t.Fatalf("SetLocalLabel failed: %v", err)
	}

	customProf, err := envelope.ParseRelayURI("synctool://relay?v=1&u=https%3A%2F%2F203.0.113.9%3A54843&pin=" + strings.Repeat("b2", 32) + "#k=" + strings.Repeat("K", 43))
	if err != nil {
		t.Fatalf("ParseRelayURI failed: %v", err)
	}
	if err := ks.SaveRelayProfile(*customProf); err != nil {
		t.Fatalf("SaveRelayProfile failed: %v", err)
	}

	card := envelope.DeviceCard{
		DeviceID:             strings.Repeat("ab", 16),
		Label:                "Pixel-9-Pro",
		Platform:             "android",
		SignPK:               strings.Repeat("S", 43),
		KEMPK:                strings.Repeat("M", 43),
		IssuedAt:             1700000000,
		CardSig:              strings.Repeat("C", 86),
		RelayID:              customProf.RelayID,
		MailboxID:            strings.Repeat("ef", 16),
		OutboundDepositToken: "super_secret_deposit_token_0123456789abcdef",
		InboundTokenID:       "dt_1122334455667788",
		InboundTokenHash:     strings.Repeat("9", 64),
		PairedAt:             1700000100,
	}
	if err := ks.PutDevice(card); err != nil {
		t.Fatalf("PutDevice failed: %v", err)
	}
	if err := ks.AddPendingTokenDelete("dt_revoked_01"); err != nil {
		t.Fatalf("AddPendingTokenDelete failed: %v", err)
	}
	if err := ks.MarkSeenMsgID(strings.Repeat("12", 16), 1700000200, time.Now().Unix()); err != nil {
		t.Fatalf("MarkSeenMsgID failed: %v", err)
	}

	// RAM-only ManifestCache & Checkpoints must NOT be written into vault.gsv JSON.
	if err := ks.PutCachedManifest(envelope.CachedManifestEntry{
		ItemID: "item-ram-only",
		Manifest: envelope.Manifest{
			EnvelopeVersion: 1,
			MsgID:           strings.Repeat("34", 16),
			AppID:           "com.yellowdking.gravityssh",
			Schema:          "gravityssh.vault.v1",
			InlinePayload:   map[string]any{"ramMarker": "RAM_ONLY_SENTINEL"},
		},
	}); err != nil {
		t.Fatalf("PutCachedManifest failed: %v", err)
	}
	if err := ks.PutCheckpoint(envelope.TransferCheckpoint{
		Direction:   "recv",
		MsgID:       strings.Repeat("34", 16),
		PartID:      1,
		NextTxIdx:   3,
		UploadToken: "RAM_UPLOAD_TOKEN_SENTINEL",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("PutCheckpoint failed: %v", err)
	}

	// Concurrent race exercise on VaultSyncKeyStore.
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _ = ks.GetLocalLabel()
			_, _ = ks.ListDevices()
			_, _ = ks.HasSeenMsgID(strings.Repeat("12", 16))
			_, _ = ks.GetCachedManifest("item-ram-only")
			_ = ks.PutCheckpoint(envelope.TransferCheckpoint{
				Direction: "recv",
				MsgID:     fmt.Sprintf("%032x", idx+100),
				PartID:    1,
				NextTxIdx: idx,
				ExpiresAt: time.Now().Add(time.Minute).Unix(),
			})
		}(g)
	}
	wg.Wait()

	// ExportSyncBundle must NEVER export VaultData.Sync.
	exported, err := store.ExportSyncBundle("PC")
	if err != nil {
		t.Fatalf("ExportSyncBundle failed: %v", err)
	}
	expJSON, _ := json.Marshal(exported)
	for _, secret := range []string{"super_secret_deposit_token", strings.Repeat("K", 43), "dt_revoked_01"} {
		if bytes.Contains(expJSON, []byte(secret)) {
			t.Fatalf("ExportSyncBundle leaked sync secret %q!", secret)
		}
	}

	// ImportSyncBundle("overwrite") MUST preserve VaultData.Sync completely intact!
	overwriteBundle := models.SyncBundle{
		SchemaVersion:  1,
		SourcePlatform: "android",
		SourceDevice:   "Phone",
		Servers:        []models.ServerConfig{{ID: "ow-1", Name: "OnlySrv", Host: "10.9.9.9", Port: 22, Username: "root"}},
	}
	if _, err := store.ImportSyncBundle(overwriteBundle, "overwrite"); err != nil {
		t.Fatalf("ImportSyncBundle overwrite failed: %v", err)
	}
	gotSeedAfterOW, err := ks.LoadOrCreateMasterSeed()
	if err != nil || gotSeedAfterOW != seed1 {
		t.Fatalf("overwrite wiped MasterSeed!")
	}
	devsAfterOW, err := ks.ListDevices()
	if err != nil || len(devsAfterOW) != 1 || devsAfterOW[0].OutboundDepositToken != card.OutboundDepositToken {
		t.Fatalf("overwrite wiped paired devices: %+v err=%v", devsAfterOW, err)
	}
	relAfterOW, err := ks.GetCustomRelay()
	if err != nil || relAfterOW == nil || relAfterOW.AccessKey != customProf.AccessKey {
		t.Fatalf("overwrite wiped CustomRelay: %+v err=%v", relAfterOW, err)
	}
	pendAfterOW, err := ks.ListPendingTokenDeletes()
	if err != nil || len(pendAfterOW) != 1 || pendAfterOW[0] != "dt_revoked_01" {
		t.Fatalf("overwrite wiped PendingTokenDeletes: %+v err=%v", pendAfterOW, err)
	}
	seenAfterOW, err := ks.HasSeenMsgID(strings.Repeat("12", 16))
	if err != nil || !seenAfterOW {
		t.Fatalf("overwrite wiped SeenMsgs")
	}

	// Verify disk file is encrypted and contains zero plaintext sync secrets.
	diskBytes, err := os.ReadFile(vp)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{
		"super_secret_deposit_token",
		strings.Repeat("K", 43),
		"Pixel-9-Pro",
		"RAM_ONLY_SENTINEL",
		"RAM_UPLOAD_TOKEN_SENTINEL",
	} {
		if bytes.Contains(diskBytes, []byte(needle)) {
			t.Fatalf("plaintext sync secret %q leaked to disk!", needle)
		}
	}

	// Set password -> Lock() -> verify all in-memory secrets & RAM caches are wiped and KeyStore rejects calls!
	if err := store.SetPassword("", "sync-lock-pw"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}
	if err := store.Lock(); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	store.mu.RLock()
	if store.data.Sync.MasterSeedB64U != "" || store.data.Sync.CustomRelay != nil || len(store.data.Sync.Devices) != 0 {
		store.mu.RUnlock()
		t.Fatalf("clearVaultDataSecrets failed to zero Sync secrets on Lock: %+v", store.data.Sync)
	}
	store.mu.RUnlock()

	if _, err := ks.LoadOrCreateMasterSeed(); !envelope.IsCode(err, envelope.ErrCodeKeystoreError) {
		t.Fatalf("expected ERR_KEYSTORE_ERROR while locked, got %v", err)
	}
	if _, err := ks.ListDevices(); !envelope.IsCode(err, envelope.ErrCodeKeystoreError) {
		t.Fatalf("expected ERR_KEYSTORE_ERROR on ListDevices while locked, got %v", err)
	}
	if _, err := ks.GetCachedManifest("item-ram-only"); !envelope.IsCode(err, envelope.ErrCodeKeystoreError) {
		t.Fatalf("expected ERR_KEYSTORE_ERROR on GetCachedManifest while locked, got %v", err)
	}

	// Unlock() restores persisted Sync state, while RAM-only caches were wiped cleanly!
	if err := store.Unlock("sync-lock-pw"); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
	if cm, err := ks.GetCachedManifest("item-ram-only"); err != nil || cm != nil {
		t.Fatalf("expected RAM ManifestCache wiped across Lock/Unlock, got %+v err=%v", cm, err)
	}
	if cp, err := ks.GetCheckpoint("recv", strings.Repeat("34", 16), 1); err != nil || cp != nil {
		t.Fatalf("expected RAM Checkpoints wiped across Lock/Unlock, got %+v err=%v", cp, err)
	}
	restoredSeed, err := ks.LoadOrCreateMasterSeed()
	if err != nil || restoredSeed != seed1 {
		t.Fatalf("expected MasterSeed restored on Unlock, err=%v", err)
	}
	restoredLbl, _ := ks.GetLocalLabel()
	if restoredLbl != "Office-Workstation" {
		t.Fatalf("expected LocalLabel restored, got %q", restoredLbl)
	}
	if err := ks.RemovePendingTokenDelete("dt_revoked_01"); err != nil {
		t.Fatalf("RemovePendingTokenDelete failed: %v", err)
	}
	if list, _ := ks.ListPendingTokenDeletes(); len(list) != 0 {
		t.Fatalf("expected empty PendingTokenDeletes after remove, got %+v", list)
	}
	_ = store.Close()
}

func TestSeenMsgsPruningAdvancesReplayWatermark(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ks := store.SyncKeyStore()

	oldSeen := time.Now().Unix() - int64(40*86400)
	// Seed 4,096 old entries directly so we test pruning deterministically in <5ms.
	store.mu.Lock()
	for i := 0; i < envelope.DefaultSeenMsgMinEntries; i++ {
		id := fmt.Sprintf("%032x", i+1)
		store.data.Sync.SeenMsgs[id] = envelope.SeenMsgEntry{
			MsgID:     id,
			CreatedAt: int64(1000 + i),
			SeenAt:    oldSeen,
		}
	}
	store.mu.Unlock()

	// Adding 1 fresh entry pushes len > 4096 and evicts the oldest expired entry, advancing ReplayWatermarkTS.
	freshID := strings.Repeat("ff", 16)
	now := time.Now().Unix()
	if err := ks.MarkSeenMsgID(freshID, now, now); err != nil {
		t.Fatalf("MarkSeenMsgID failed: %v", err)
	}
	wm, err := ks.GetReplayWatermark()
	if err != nil || wm < 1000 {
		t.Fatalf("expected ReplayWatermarkTS >= 1000 after eviction, got %d err=%v", wm, err)
	}
	if ok, _ := ks.HasSeenMsgID(freshID); !ok {
		t.Fatal("expected fresh msg_id retained")
	}
}

func TestDeleteSSHKeyDowngradesAuthBothAndMacAppBundlePathGuard(t *testing.T) {
	// WIN-P1-2: DeleteSSHKey downgrades AuthBoth -> AuthPassword when Password != "".
	store, err := Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	pemStr, _, err := sshkey.GenerateKeyPair("ed25519", "k@test", "")
	if err != nil {
		t.Fatal(err)
	}
	keyMeta, err := store.ImportSSHKey("MyKey", pemStr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := store.SaveServer(models.ServerSaveRequest{
		Name:     "DualAuth",
		Host:     "10.0.0.50",
		Port:     22,
		Username: "root",
		AuthType: models.AuthBoth,
		Password: "keep-my-password",
		SSHKeyID: keyMeta.ID,
		JumpHost: models.JumpSaveReq{
			Mode:     models.JumpCustom,
			Host:     "10.0.0.1",
			Port:     22,
			Username: "bastion",
			AuthType: models.AuthBoth,
			Password: "keep-jump-password",
			SSHKeyID: keyMeta.ID,
		},
	})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	if err := store.DeleteSSHKey(keyMeta.ID); err != nil {
		t.Fatalf("DeleteSSHKey failed: %v", err)
	}
	gotSrv, err := store.GetServerInternal(srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSrv.SSHKeyID != "" || gotSrv.AuthType != models.AuthPassword || gotSrv.Password != "keep-my-password" {
		t.Fatalf("expected Server AuthBoth downgraded to AuthPassword with password intact, got %+v", gotSrv)
	}
	if gotSrv.JumpHost.SSHKeyID != "" || gotSrv.JumpHost.AuthType != models.AuthPassword || gotSrv.JumpHost.Password != "keep-jump-password" {
		t.Fatalf("expected JumpHost AuthBoth downgraded to AuthPassword with password intact, got %+v", gotSrv.JumpHost)
	}

	// WIN-P1-3: macOS .app/Contents/MacOS must skip portable exeDir/gsshconfig to preserve Gatekeeper signature.
	macBundleExeDir := filepath.Join(t.TempDir(), "GravitySSH.app", "Contents", "MacOS")
	if err := os.MkdirAll(macBundleExeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	macAppSupport := filepath.Join(t.TempDir(), "Library", "Application Support")
	resolvedMac, err := resolveDefaultVaultPathForOS("darwin", macBundleExeDir, macAppSupport)
	wantMac := filepath.Join(macAppSupport, "GravitySSH", "gsshconfig", "vault.gsv")
	if err != nil || resolvedMac != wantMac {
		t.Fatalf("expected macOS .app bundle to resolve to %q, got (%q, %v)", wantMac, resolvedMac, err)
	}
	if _, statErr := os.Stat(filepath.Join(macBundleExeDir, "gsshconfig")); !os.IsNotExist(statErr) {
		t.Fatal("must NEVER create gsshconfig inside GravitySSH.app/Contents/MacOS!")
	}
}
