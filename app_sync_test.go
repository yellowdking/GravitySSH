package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
)

func TestApplySyncReceiveCloneZeroAndKeepOnError(t *testing.T) {
	app, store := newTestAppWithVault(t)
	if err := store.SetPassword("", "vault-pw"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}

	pemStr, parsed, err := sshkey.GenerateKeyPair("ed25519", "sync@test", "")
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	bundle := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		SourcePlatform: "android",
		SourceDevice:   "Pixel",
		Servers: []models.ServerConfig{
			{ID: "srv-sync-1", Name: "SyncNode", Host: "10.9.0.1", Port: 22, Username: "root", Password: "top-secret-password"},
		},
		SSHKeys: []models.SSHKeyRecord{
			{
				ID:            "key-sync-1",
				Name:          "SyncKey",
				KeyType:       parsed.KeyType,
				Fingerprint:   parsed.Fingerprint,
				PublicKey:     parsed.PublicKey,
				PrivateKeyPEM: pemStr,
			},
		},
	}
	preview := app.buildSyncPreview(bundle)
	if preview.PreviewID == "" || preview.ServerCount != 1 || preview.KeyCount != 1 {
		t.Fatalf("unexpected buildSyncPreview: %+v", preview)
	}

	// Invalid mode fails ImportSyncBundle while keeping cached preview intact for retry.
	if _, err := app.ApplySyncReceive(preview.PreviewID, "unsupported-mode"); err == nil {
		t.Fatal("expected invalid mode error")
	}
	peeked, ok := app.syncCache.peekClone(preview.PreviewID)
	if !ok || len(peeked.Servers) != 1 || peeked.Servers[0].Password != "top-secret-password" ||
		len(peeked.SSHKeys) != 1 || peeked.SSHKeys[0].PrivateKeyPEM != pemStr {
		t.Fatalf("cached preview corrupted after error: ok=%v bundle=%+v", ok, peeked)
	}
	zeroSyncBundle(&peeked)

	// Valid apply succeeds and deletes preview from cache.
	sum, err := app.ApplySyncReceive(preview.PreviewID, "merge")
	if err != nil {
		t.Fatalf("ApplySyncReceive retry failed: %v", err)
	}
	if sum.ServersAdded != 1 || sum.KeysAdded != 1 {
		t.Fatalf("unexpected import summary: %+v", sum)
	}
	if _, ok := app.syncCache.peekClone(preview.PreviewID); ok {
		t.Fatal("expected preview removed from cache after successful apply")
	}
	srv, err := store.GetServerInternal("srv-sync-1")
	if err != nil || srv.Password != "top-secret-password" {
		t.Fatalf("imported server mismatch: %+v err=%v", srv, err)
	}
}

func TestAppSyncOverviewCustomRelayRedactionAndDeviceLabel(t *testing.T) {
	app, _ := newTestAppWithVault(t)

	ov0, err := app.GetSyncOverview()
	if err != nil {
		t.Fatalf("GetSyncOverview failed: %v", err)
	}
	if len(ov0.LocalDeviceID) != 32 || ov0.Relay.Mode != "default" || ov0.Relay.Endpoint != "https://43.155.231.14:54843" {
		t.Fatalf("unexpected initial overview: %+v", ov0)
	}

	if err := app.SetSyncDeviceLabel("My-Win11-Workstation"); err != nil {
		t.Fatalf("SetSyncDeviceLabel failed: %v", err)
	}
	if err := app.SetSyncDeviceLabel(strings.Repeat("汉", 65)); err == nil {
		t.Fatal("expected >64 rune label to be rejected")
	}

	// Reject bare URLs and leaked ?k= query secrets.
	if _, err := app.SetCustomSyncRelay("https://198.51.100.10:54843"); err == nil {
		t.Fatal("expected bare HTTPS URL without synctool://relay scheme to be rejected")
	}
	pin := strings.Repeat("c3", 32)
	secretKey := "SecretRelayAccessKey_NeverExposeToFrontend_987654"
	if _, err := app.SetCustomSyncRelay("synctool://relay?v=1&u=https%3A%2F%2F198.51.100.10%3A54843&pin=" + pin + "&k=" + secretKey); err == nil {
		t.Fatal("expected ?k= in query string to be rejected")
	}

	validURI := "synctool://relay?v=1&u=https%3A%2F%2F198.51.100.10%3A54843&pin=" + pin + "#k=" + secretKey
	relStatus, err := app.SetCustomSyncRelay(validURI)
	if err != nil {
		t.Fatalf("SetCustomSyncRelay valid failed: %v", err)
	}
	rawDTO, _ := json.Marshal(relStatus)
	if strings.Contains(string(rawDTO), secretKey) || relStatus.Mode != "custom" || !strings.Contains(relStatus.RedactedURI, "#[REDACTED]") {
		t.Fatalf("SyncRelayStatus leaked secret or wrong mode: %s", string(rawDTO))
	}

	ov1, err := app.GetSyncOverview()
	if err != nil || ov1.LocalDeviceLabel != "My-Win11-Workstation" || ov1.Relay.Mode != "custom" {
		t.Fatalf("unexpected updated overview: %+v err=%v", ov1, err)
	}
	rawOV, _ := json.Marshal(ov1)
	if strings.Contains(string(rawOV), secretKey) {
		t.Fatalf("SyncOverview leaked AccessKey: %s", string(rawOV))
	}

	// Reset custom relay back to default with "".
	resetStatus, err := app.SetCustomSyncRelay("")
	if err != nil || resetStatus.Mode != "default" {
		t.Fatalf("reset custom relay failed: %+v err=%v", resetStatus, err)
	}
}

func TestLockVaultCancelsShortcutsSyncSessionsAndGatesSessionRPCs(t *testing.T) {
	app, store := newTestAppWithVault(t)

	// LockVault must reject when no vault password is configured.
	if err := app.LockVault(); err == nil {
		t.Fatal("expected LockVault to fail when no password is set")
	}

	// ImportSSHKeyFromPath validation checks
	tmpDir := t.TempDir()
	if _, err := app.ImportSSHKeyFromFile("k", "", ""); err == nil {
		t.Fatal("expected ImportSSHKeyFromFile with nil ctx to fail cleanly")
	}
	pemStr, parsed, err := sshkey.GenerateKeyPair("ed25519", "file@import", "pass-123")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(tmpDir, "prod_bastion.pem")
	if err := os.WriteFile(keyPath, []byte(pemStr), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := app.importSSHKeyFromPath(store, keyPath, "", "pass-123", "from-file")
	if err != nil || meta == nil || meta.Fingerprint != parsed.Fingerprint {
		t.Fatalf("importSSHKeyFromPath failed: meta=%+v err=%v", meta, err)
	}

	if err := store.SetPassword("", "lock-pass-123"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}

	// Simulate an active multi-line paced shortcut runner + staged sync preview.
	var writes atomic.Int32
	runner := app.getShortcutRunner("sess-live")
	_ = runner.run(
		nil,
		nil,
		"echo 1\necho 2\necho 3\necho 4\necho 5",
		true,
		func(string) error {
			writes.Add(1)
			return nil
		},
	)
	pv := app.buildSyncPreview(sampleTestBundle())

	// LockVault must stop shortcut runners, clear sync previews/sessions, close SSH sessions, and lock store!
	if err := app.LockVault(); err != nil {
		t.Fatalf("LockVault failed: %v", err)
	}
	writesAtLock := writes.Load()
	time.Sleep(80 * time.Millisecond)
	if writes.Load() != writesAtLock {
		t.Fatalf("shortcut runner continued writing after LockVault: before=%d after=%d", writesAtLock, writes.Load())
	}
	if _, ok := app.syncCache.peekClone(pv.PreviewID); ok {
		t.Fatal("expected pending sync preview wiped on LockVault")
	}

	// WIN-P1-1: Every active session/SFTP/Sync RPC must reject while locked (except DisconnectSession).
	if err := app.DisconnectSession("sess-live"); err != nil {
		t.Fatalf("DisconnectSession must remain callable while locked, got %v", err)
	}
	lockedChecks := []struct {
		name string
		fn   func() error
	}{
		{"WriteTerminal", func() error { return app.WriteTerminal("s1", "id\r") }},
		{"ResizeTerminal", func() error { return app.ResizeTerminal("s1", 80, 24) }},
		{"SetSessionCWD", func() error { return app.SetSessionCWD("s1", "/root") }},
		{"GetSessionMetrics", func() error { _, e := app.GetSessionMetrics("s1"); return e }},
		{"SFTPList", func() error { _, e := app.SFTPList("s1", "/"); return e }},
		{"SFTPMkdir", func() error { return app.SFTPMkdir("s1", "/tmp/a") }},
		{"SFTPRename", func() error { return app.SFTPRename("s1", "/a", "/b") }},
		{"SFTPDelete", func() error { return app.SFTPDelete("s1", "/tmp/a") }},
		{"SFTPAlignPermissions", func() error { _, e := app.SFTPAlignPermissions("s1", "/tmp/a"); return e }},
		{"SFTPExtractArchive", func() error { _, e := app.SFTPExtractArchive("s1", "/tmp/a.tgz"); return e }},
		{"SFTPReadTextFile", func() error { _, e := app.SFTPReadTextFile("s1", "/etc/hosts"); return e }},
		{"SFTPWriteTextFile", func() error { return app.SFTPWriteTextFile("s1", "/tmp/a", "x") }},
		{"SFTPCompress", func() error { _, e := app.SFTPCompress("s1", "/tmp", []string{"a"}, "tgz"); return e }},
		{"SFTPUploadFiles", func() error { _, e := app.SFTPUploadFiles("s1", []string{"a"}, "/tmp"); return e }},
		{"GetSyncOverview", func() error { _, e := app.GetSyncOverview(); return e }},
		{"ListSyncInbox", func() error { _, e := app.ListSyncInbox(); return e }},
		{"StartSyncOneShotSend", func() error { _, e := app.StartSyncOneShotSend(1, false); return e }},
	}
	for _, tc := range lockedChecks {
		if err := tc.fn(); err == nil {
			t.Fatalf("expected %s to fail while vault is locked", tc.name)
		}
	}

	// Re-unlocking restores imported key cleanly.
	if err := app.UnlockVault("lock-pass-123"); err != nil {
		t.Fatalf("UnlockVault failed: %v", err)
	}
	keys, err := app.ListSSHKeys()
	if err != nil || len(keys) != 1 || keys[0].Fingerprint != parsed.Fingerprint {
		t.Fatalf("expected key restored after unlock, got %+v err=%v", keys, err)
	}
}
