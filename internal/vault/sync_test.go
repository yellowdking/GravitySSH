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
	if err != nil || exported.SchemaVersion != 1 || exported.SourcePlatform != "windows" || len(exported.Servers) != 2 || len(exported.SSHKeys) != 1 {
		t.Fatalf("unexpected exported bundle: %+v err=%v", exported, err)
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
