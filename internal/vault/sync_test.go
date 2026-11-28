package vault

import (
	"path/filepath"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
)

func TestVaultSyncExportAndMerge(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	pem1, parsed1, err := sshkey.GenerateKeyPair("ed25519", "local@pc", "")
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	localKey, err := store.ImportSSHKey("LocalKey", pem1, "", "local")
	if err != nil {
		t.Fatalf("ImportSSHKey failed: %v", err)
	}
	localSrv, err := store.SaveServer(models.ServerSaveRequest{
		Name: "Existing-Web", Host: "10.0.0.1", Port: 22, Username: "ubuntu",
		AuthType: models.AuthKey, SSHKeyID: localKey.ID,
	})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	localOnlySrv, err := store.SaveServer(models.ServerSaveRequest{
		Name: "Local-Only-DB", Host: "10.0.0.99", Port: 22, Username: "postgres",
		AuthType: models.AuthPassword, Password: "db-secret",
	})
	if err != nil {
		t.Fatalf("SaveServer local-only failed: %v", err)
	}

	exported, err := store.ExportSyncBundle("WIN-WORKSTATION")
	if err != nil || exported.SchemaVersion != 1 || exported.SourcePlatform != currentSyncPlatform() || len(exported.Servers) != 2 || len(exported.SSHKeys) != 1 {
		t.Fatalf("unexpected exported bundle: %+v err=%v", exported, err)
	}
	if syncPlatformForGOOS("darwin") != "macos" || syncPlatformForGOOS("windows") != "windows" {
		t.Fatalf("unexpected syncPlatformForGOOS mapping")
	}

	incoming := models.SyncBundle{
		SchemaVersion: 1, SourcePlatform: "android", SourceDevice: "Pixel-9",
		SSHKeys: []models.SSHKeyRecord{{
			ID: "android-key-id", Name: "LocalKey-Synced", KeyType: parsed1.KeyType,
			Fingerprint: parsed1.Fingerprint, PublicKey: parsed1.PublicKey, PrivateKeyPEM: pem1,
		}},
		Servers: []models.ServerConfig{
			{ID: "android-srv-1", Name: "Existing-Web-Updated", Host: "10.0.0.1", Port: 22, Username: "ubuntu", AuthType: models.AuthKey, SSHKeyID: "android-key-id"},
			{ID: "android-srv-2", Name: "HK-New", Host: "10.0.0.2", Port: 2222, Username: "root", AuthType: models.AuthKey, SSHKeyID: "android-key-id"},
		},
		Shortcuts: []models.CommandShortcut{{ID: "sc-new", Name: "Nginx Test", Command: "nginx -t", Category: "Ops", AutoExecute: true}},
		Templates: []models.BuiltinTemplate{{ID: "tpl-custom-1", Name: "Custom Backup", Category: "Ops", Command: "tar -czf /tmp/a.tgz /etc"}},
	}

	sum, err := store.ImportSyncBundle(incoming, "merge")
	if err != nil {
		t.Fatalf("ImportSyncBundle merge failed: %v", err)
	}
	if sum.KeysUpdated != 1 || sum.KeysAdded != 0 || sum.ServersUpdated != 1 || sum.ServersAdded != 1 || sum.ShortcutsAdded != 1 || sum.TemplatesAdded != 1 {
		t.Fatalf("unexpected merge summary: %+v", sum)
	}
	servers := store.ListServers()
	if len(servers) != 3 {
		t.Fatalf("expected 3 servers after merge, got %d", len(servers))
	}
	var foundLocalOnly, foundNew bool
	for _, s := range servers {
		if s.ID == localOnlySrv.ID {
			foundLocalOnly = true
		}
		if s.ID == localSrv.ID && s.Name != "Existing-Web-Updated" {
			t.Fatalf("expected Existing-Web updated in place, got %+v", s)
		}
		if s.Host == "10.0.0.2" {
			foundNew = true
			if s.SSHKeyID != localKey.ID {
				t.Fatalf("expected remapped sshKeyId %s, got %s", localKey.ID, s.SSHKeyID)
			}
		}
	}
	if !foundLocalOnly || !foundNew {
		t.Fatalf("missing servers after merge: localOnly=%v new=%v", foundLocalOnly, foundNew)
	}
}

func TestVaultSyncOverwritePreservesSettingsAndPassword(t *testing.T) {
	vp := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vp, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := store.SetPassword("", "my-vault-pass"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}
	cfg := store.GetSettings()
	cfg.TerminalFontSize = 19
	if _, err := store.SaveSettings(cfg); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	if _, err := store.SaveServer(models.ServerSaveRequest{Name: "Old", Host: "1.1.1.1", Port: 22, Username: "root"}); err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}

	incoming := models.SyncBundle{
		SchemaVersion: 1, SourcePlatform: "android", SourceDevice: "Phone",
		Servers:   []models.ServerConfig{{ID: "s-only", Name: "NewOnly", Host: "2.2.2.2", Port: 22, Username: "admin"}},
		SSHKeys:   []models.SSHKeyRecord{},
		Shortcuts: []models.CommandShortcut{},
		Templates: []models.BuiltinTemplate{},
	}
	sum, err := store.ImportSyncBundle(incoming, "overwrite")
	if err != nil || sum.Mode != "overwrite" || sum.ServersAdded != 1 || len(store.ListServers()) != 1 {
		t.Fatalf("unexpected overwrite result: %+v err=%v", sum, err)
	}

	store2, err := Open(vp, nil)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	if !store2.IsLocked() {
		t.Fatal("expected vault to remain password-protected")
	}
	if _, err := store2.ExportSyncBundle("PC"); err == nil {
		t.Fatal("expected ExportSyncBundle to fail while locked")
	}
	if err := store2.Unlock("my-vault-pass"); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
	if store2.GetSettings().TerminalFontSize != 19 {
		t.Fatalf("expected local settings preserved, got fontSize=%d", store2.GetSettings().TerminalFontSize)
	}
	if len(store2.ListServers()) != 1 || store2.ListServers()[0].Host != "2.2.2.2" {
		t.Fatalf("unexpected persisted servers: %+v", store2.ListServers())
	}
}

func TestVaultSyncMergePreservesSecretsAndPreventsHijack(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	srv, err := store.SaveServer(models.ServerSaveRequest{
		Name: "Prod-API", Host: "10.0.1.1", Port: 22, Username: "ubuntu",
		AuthType: models.AuthPassword, Password: "local-password",
		Proxy: models.ProxySaveReq{Type: models.ProxySOCKS5, Host: "127.0.0.1", Port: 1080, Password: "local-proxy-pass"},
		JumpHost: models.JumpSaveReq{Mode: models.JumpCustom, Host: "10.0.1.254", Port: 22, Username: "jump", Password: "local-jump-pass"},
	})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	store.MarkServerConnected(srv.ID)
	sc, err := store.SaveShortcut(models.CommandShortcut{Name: "Status", Command: "uptime"})
	if err != nil {
		t.Fatalf("SaveShortcut failed: %v", err)
	}
	store.IncrementShortcutUse(sc.ID)
	store.IncrementShortcutUse(sc.ID)

	// Incoming merge has empty passwords on MATCHING proxy/jump endpoints, blank-host server,
	// shortcut with UseCount=0, and custom templates trying to hijack preset tpl-bbr by Name AND by ID.
	incoming := models.SyncBundle{
		SchemaVersion: 1, SourcePlatform: "android", SourceDevice: "Phone",
		Servers: []models.ServerConfig{
			{
				ID: "remote-1", Name: "Prod-API-Synced", Host: "10.0.1.1", Port: 22, Username: "ubuntu", AuthType: models.AuthPassword,
				Proxy:    models.ProxyConfig{Type: models.ProxySOCKS5, Host: "127.0.0.1", Port: 1080},
				JumpHost: models.JumpHostConfig{Mode: models.JumpCustom, Host: "10.0.1.254", Port: 22, Username: "jump"},
			},
			{ID: "blank-host", Name: "Invalid", Host: "   ", Port: 22, Username: "root"},
		},
		Shortcuts: []models.CommandShortcut{
			{ID: "remote-sc", Name: "Status", Command: "uptime", UseCount: 0},
		},
		Templates: []models.BuiltinTemplate{
			{ID: "custom-hijack-name", Name: "开启 BBR 网络加速", Category: "自定义", Command: "echo evil-name", PresetID: ""},
			{ID: "tpl-bbr", Name: "Hijack By ID", Category: "自定义", Command: "echo evil-id", PresetID: ""},
			{ID: "custom-forged-preset", Name: "Forged Preset", Category: "自定义", Command: "echo forged", PresetID: "tpl-bbr"},
		},
	}

	sum, err := store.ImportSyncBundle(incoming, "merge")
	if err != nil {
		t.Fatalf("ImportSyncBundle failed: %v", err)
	}
	if sum.ServersAdded != 0 || sum.ServersUpdated != 1 {
		t.Fatalf("expected blank-host skipped and 1 server updated, got %+v", sum)
	}
	gotSrv, err := store.GetServerInternal(srv.ID)
	if err != nil {
		t.Fatalf("GetServerInternal failed: %v", err)
	}
	if gotSrv.Password != "local-password" || gotSrv.Proxy.Password != "local-proxy-pass" || gotSrv.JumpHost.Password != "local-jump-pass" {
		t.Fatalf("local passwords were wiped on matching merge: %+v", gotSrv)
	}
	if gotSrv.LastConnectedAt == nil || gotSrv.CreatedAt.IsZero() {
		t.Fatalf("expected LastConnectedAt and CreatedAt preserved")
	}

	// Verify shortcut UseCount preserved
	for _, item := range store.ListShortcuts() {
		if item.ID == sc.ID && item.UseCount != 2 {
			t.Fatalf("expected UseCount=2 preserved, got %d", item.UseCount)
		}
	}

	// Verify preset tpl-bbr was NOT overwritten by custom template via matching Name, matching ID, or forged PresetID
	for _, tpl := range store.ListTemplates() {
		if tpl.ID == "tpl-bbr" && (tpl.Command == "echo evil-name" || tpl.Command == "echo evil-id" || tpl.Command == "echo forged") {
			t.Fatalf("custom template hijacked preset tpl-bbr: %+v", tpl)
		}
		if tpl.ID == "tpl-bbr" && tpl.PresetID != "tpl-bbr" {
			t.Fatalf("preset tpl-bbr lost its PresetID: %+v", tpl)
		}
	}
}

func TestVaultSyncMergeDoesNotLeakProxyOrJumpSecretsOnMismatchAndOverwriteSanitizesPresets(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	srv1, err := store.SaveServer(models.ServerSaveRequest{
		Name: "Corp-A", Host: "10.1.0.1", Port: 22, Username: "ops",
		AuthType: models.AuthPassword, Password: "srv1-secret",
		Proxy:    models.ProxySaveReq{Type: models.ProxySOCKS5, Host: "corp-proxy.internal", Port: 1080, Username: "alice", Password: "corp-proxy-secret"},
		JumpHost: models.JumpSaveReq{Mode: models.JumpCustom, Host: "corp-bastion.internal", Port: 22, Username: "alice", Password: "corp-jump-secret"},
	})
	if err != nil {
		t.Fatalf("SaveServer srv1 failed: %v", err)
	}
	srv2, err := store.SaveServer(models.ServerSaveRequest{
		Name: "Corp-B", Host: "10.1.0.2", Port: 22, Username: "ops",
		AuthType: models.AuthPassword, Password: "srv2-secret",
		Proxy:    models.ProxySaveReq{Type: models.ProxySOCKS5, Host: "corp-proxy.internal", Port: 1080, Username: "alice", Password: "corp-proxy-secret-2"},
		JumpHost: models.JumpSaveReq{Mode: models.JumpCustom, Host: "corp-bastion.internal", Port: 22, Username: "alice", Password: "corp-jump-secret-2"},
	})
	if err != nil {
		t.Fatalf("SaveServer srv2 failed: %v", err)
	}

	// Merge: srv1 switches to DIFFERENT proxy & jump hosts (empty password); srv2 DISABLES proxy & jump.
	incoming := models.SyncBundle{
		SchemaVersion: 1, SourcePlatform: "android", SourceDevice: "Phone",
		Servers: []models.ServerConfig{
			{
				ID: srv1.ID, Name: "Corp-A", Host: "10.1.0.1", Port: 22, Username: "ops", AuthType: models.AuthPassword,
				Proxy:    models.ProxyConfig{Type: models.ProxySOCKS5, Host: "other-proxy.example", Port: 1080, Username: "alice"},
				JumpHost: models.JumpHostConfig{Mode: models.JumpCustom, Host: "other-bastion.example", Port: 22, Username: "alice"},
			},
			{
				ID: srv2.ID, Name: "Corp-B", Host: "10.1.0.2", Port: 22, Username: "ops", AuthType: models.AuthPassword,
				Proxy:    models.ProxyConfig{Type: models.ProxyNone},
				JumpHost: models.JumpHostConfig{Mode: models.JumpNone},
			},
		},
	}
	if _, err := store.ImportSyncBundle(incoming, "merge"); err != nil {
		t.Fatalf("ImportSyncBundle merge failed: %v", err)
	}

	got1, _ := store.GetServerInternal(srv1.ID)
	if got1.Proxy.Password != "" {
		t.Fatalf("proxy password leaked to different proxy host: %q", got1.Proxy.Password)
	}
	if got1.JumpHost.Password != "" {
		t.Fatalf("jump password leaked to different bastion host: %q", got1.JumpHost.Password)
	}
	if got1.Password != "srv1-secret" {
		t.Fatalf("expected matching server endpoint password preserved, got %q", got1.Password)
	}

	got2, _ := store.GetServerInternal(srv2.ID)
	if got2.Proxy.Password != "" || got2.JumpHost.Password != "" {
		t.Fatalf("disabled proxy/jump retained stale secrets: proxy=%q jump=%q", got2.Proxy.Password, got2.JumpHost.Password)
	}

	// Overwrite mode: custom template claiming builtin preset ID, invalid presetId, and duplicate presetId must be sanitized.
	overwriteBundle := models.SyncBundle{
		SchemaVersion: 1, SourcePlatform: "android", SourceDevice: "Phone",
		Templates: []models.BuiltinTemplate{
			{ID: "tpl-bbr", Name: "Valid BBR", Category: "网络优化", Command: "sysctl -p", PresetID: "tpl-bbr"},
			{ID: "tpl-bbr", Name: "Duplicate BBR", Category: "自定义", Command: "echo dup", PresetID: "tpl-bbr"},
			{ID: "tpl-swap", Name: "Custom With Preset ID", Category: "自定义", Command: "echo hijack", PresetID: ""},
			{ID: "custom-1", Name: "Unknown Preset", Category: "自定义", Command: "echo x", PresetID: "tpl-does-not-exist"},
		},
	}
	if _, err := store.ImportSyncBundle(overwriteBundle, "overwrite"); err != nil {
		t.Fatalf("ImportSyncBundle overwrite failed: %v", err)
	}
	bbrPresetCount := 0
	for _, tpl := range store.ListTemplates() {
		if tpl.PresetID == "tpl-bbr" {
			bbrPresetCount++
			if tpl.ID != "tpl-bbr" || tpl.Command != "sysctl -p" {
				t.Fatalf("unexpected tpl-bbr preset after overwrite: %+v", tpl)
			}
		}
		if tpl.Name == "Custom With Preset ID" && (tpl.ID == "tpl-swap" || tpl.PresetID != "") {
			t.Fatalf("custom template kept builtin preset ID in overwrite: %+v", tpl)
		}
		if tpl.Name == "Unknown Preset" && tpl.PresetID != "" {
			t.Fatalf("unknown presetId was not stripped in overwrite: %+v", tpl)
		}
	}
	if bbrPresetCount != 1 {
		t.Fatalf("expected exactly 1 deduplicated tpl-bbr preset, got %d", bbrPresetCount)
	}
	// Reloading built-in presets must restore tpl-swap without colliding with the sanitized custom template!
	res, err := store.LoadPresetTemplates()
	if err != nil {
		t.Fatalf("LoadPresetTemplates failed: %v", err)
	}
	if res.Added != 5 || res.Skipped != 1 {
		t.Fatalf("expected 5 missing presets added and 1 (tpl-bbr) skipped, got added=%d skipped=%d", res.Added, res.Skipped)
	}
}

func TestSaveServerMultiHopJumpCycleAndPortNormalization(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	a, err := store.SaveServer(models.ServerSaveRequest{
		Name: "A", Host: "10.0.0.1", Username: "root",
		Proxy:            models.ProxySaveReq{Type: "SOCKS5", Host: "127.0.0.1", Port: 0},
		JumpHost:         models.JumpSaveReq{Mode: "CUSTOM", Host: "10.0.0.254", Port: 99999, Username: "jmp", AuthType: "PASSWORD"},
		KeepAliveSeconds: 0,
	})
	if err != nil {
		t.Fatalf("SaveServer A failed: %v", err)
	}
	if a.Proxy.Type != models.ProxySOCKS5 || a.Proxy.Port != 1080 {
		t.Fatalf("expected normalized socks5 port 1080, got %+v", a.Proxy)
	}
	if a.JumpHost.Mode != models.JumpCustom || a.JumpHost.Port != 22 || a.JumpHost.AuthType != models.AuthPassword {
		t.Fatalf("expected normalized custom jump port 22 and authType password, got %+v", a.JumpHost)
	}
	if a.KeepAliveSeconds != 0 {
		t.Fatalf("expected KeepAliveSeconds=0 preserved, got %d", a.KeepAliveSeconds)
	}

	b, err := store.SaveServer(models.ServerSaveRequest{
		Name: "B", Host: "10.0.0.2", Username: "root",
		JumpHost: models.JumpSaveReq{Mode: models.JumpServer, ServerID: a.ID},
	})
	if err != nil {
		t.Fatalf("SaveServer B->A failed: %v", err)
	}
	c, err := store.SaveServer(models.ServerSaveRequest{
		Name: "C", Host: "10.0.0.3", Username: "root",
		JumpHost: models.JumpSaveReq{Mode: models.JumpServer, ServerID: b.ID},
	})
	if err != nil {
		t.Fatalf("SaveServer C->B failed: %v", err)
	}

	// Updating A to jump through C creates a 3-hop cycle A->C->B->A and MUST be rejected.
	if _, err := store.SaveServer(models.ServerSaveRequest{
		ID: a.ID, Name: "A", Host: "10.0.0.1", Username: "root",
		JumpHost: models.JumpSaveReq{Mode: models.JumpServer, ServerID: c.ID},
	}); err == nil {
		t.Fatal("expected 3-hop jump cycle A->C->B->A to be rejected")
	}
}

func TestVaultSyncSanitizesKeysJumpCyclesAndKeepAliveZero(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "vault.gsv"), nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	pem1, _, err := sshkey.GenerateKeyPair("ed25519", "local@pc", "")
	if err != nil {
		t.Fatal(err)
	}
	localKey, err := store.ImportSSHKey("LocalKey", pem1, "", "local")
	if err != nil {
		t.Fatal(err)
	}
	_, evilInfo, err := sshkey.GenerateKeyPair("ed25519", "evil@pc", "")
	if err != nil {
		t.Fatal(err)
	}

	// Merge:
	// 1) Metadata-only update with different PublicKey MUST NOT overwrite localKey.PublicKey while keeping local PrivateKeyPEM.
	// 2) Invalid PEM key and blank key MUST be rejected.
	// 3) Imported 2-hop jump cycle (R1->R2->R1) and self-loop (R3->R3) MUST be broken cleanly.
	// 4) Android keepAliveSeconds=0 MUST be preserved (not overwritten to 30), negative clamped to default.
	bundle := models.SyncBundle{
		SchemaVersion: 1, SourcePlatform: "android", SourceDevice: "Pixel",
		SSHKeys: []models.SSHKeyRecord{
			{ID: localKey.ID, Name: "LocalKey-Renamed", PublicKey: evilInfo.PublicKey, Comment: "safe\x00\ncmt"},
			{ID: "bad-pem", Name: "Corrupt", PrivateKeyPEM: "NOT-A-KEY", PublicKey: evilInfo.PublicKey},
			{ID: "  ", Name: "Blank"},
		},
		Servers: []models.ServerConfig{
			{
				ID: " r1 ", Name: "R1", Host: "10.2.0.1", Port: 0, Username: "ops", AuthType: "PASSWORD",
				KeepAliveSeconds: 0,
				Proxy:            models.ProxyConfig{Type: "HTTP", Host: "proxy.local", Port: -1},
				JumpHost:         models.JumpHostConfig{Mode: "SERVER", ServerID: "r2"},
			},
			{
				ID: "r2", Name: "R2", Host: "10.2.0.2", Port: 22, Username: "ops",
				KeepAliveSeconds: -5,
				JumpHost:         models.JumpHostConfig{Mode: models.JumpServer, ServerID: "r1"},
			},
			{
				ID: "r3", Name: "R3", Host: "10.2.0.3", Port: 22, Username: "ops",
				JumpHost: models.JumpHostConfig{Mode: models.JumpServer, ServerID: "r3"},
			},
		},
	}

	sum, err := store.ImportSyncBundle(bundle, "merge")
	if err != nil {
		t.Fatalf("ImportSyncBundle merge failed: %v", err)
	}
	if sum.KeysAdded != 0 || sum.KeysUpdated != 1 {
		t.Fatalf("expected only 1 key updated and invalid/blank keys dropped, got %+v", sum)
	}
	gotKey, err := store.GetSSHKeyInternal(localKey.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotKey.PublicKey != localKey.PublicKey || gotKey.Fingerprint != localKey.Fingerprint {
		t.Fatalf("incoming metadata overwrote local public key/fingerprint: got=%+v want=%+v", gotKey, localKey)
	}
	if gotKey.Comment != "safecmt" {
		t.Fatalf("expected sanitized comment 'safecmt', got %q", gotKey.Comment)
	}

	r1, err := store.GetServerInternal("r1")
	if err != nil {
		t.Fatalf("expected trimmed ID 'r1' to exist: %v", err)
	}
	if r1.Port != 22 || r1.AuthType != models.AuthPassword || r1.Proxy.Type != models.ProxyHTTP || r1.Proxy.Port != 8080 {
		t.Fatalf("unexpected r1 normalization: %+v", r1)
	}
	if r1.KeepAliveSeconds != 0 {
		t.Fatalf("expected Android keepAliveSeconds=0 preserved on import, got %d", r1.KeepAliveSeconds)
	}
	r2, _ := store.GetServerInternal("r2")
	if r2.KeepAliveSeconds <= 0 {
		t.Fatalf("expected negative keepAliveSeconds normalized to positive default, got %d", r2.KeepAliveSeconds)
	}
	if r1.JumpHost.Mode == models.JumpServer && r2.JumpHost.Mode == models.JumpServer &&
		r1.JumpHost.ServerID == "r2" && r2.JumpHost.ServerID == "r1" {
		t.Fatal("expected 2-hop jump cycle r1<->r2 to be broken on merge")
	}
	r3, _ := store.GetServerInternal("r3")
	if r3.JumpHost.Mode != models.JumpNone || r3.JumpHost.ServerID != "" {
		t.Fatalf("expected self-referencing jump r3->r3 cleared, got %+v", r3.JumpHost)
	}

	// Overwrite mode must also reject invalid keys and break jump cycles.
	owSum, err := store.ImportSyncBundle(bundle, "overwrite")
	if err != nil {
		t.Fatalf("ImportSyncBundle overwrite failed: %v", err)
	}
	if owSum.KeysAdded != 0 || len(store.ListSSHKeys()) != 0 {
		t.Fatalf("expected overwrite to reject private-key-less/invalid/blank keys, got %+v", owSum)
	}
	owR1, _ := store.GetServerInternal("r1")
	owR2, _ := store.GetServerInternal("r2")
	if owR1.JumpHost.Mode == models.JumpServer && owR2.JumpHost.Mode == models.JumpServer {
		t.Fatal("expected 2-hop jump cycle broken in overwrite mode")
	}
}
