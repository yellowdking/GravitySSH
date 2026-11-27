package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
)

func TestVaultEncryptedPersistenceAndSecretIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	vaultPath := filepath.Join(tmpDir, "vault.gsv")

	store, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open vault failed: %v", err)
	}

	// Import an SSH key
	privPEM, _, err := sshkey.GenerateKeyPair("ed25519", "prod@srv", "key-secret-pass")
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	meta, err := store.ImportSSHKey("Prod Key", privPEM, "key-secret-pass", "production")
	if err != nil {
		t.Fatalf("ImportSSHKey failed: %v", err)
	}
	if !meta.HasPassphrase {
		t.Fatal("expected HasPassphrase=true")
	}

	// Save a server with password, proxy password, and jump host
	srv, err := store.SaveServer(models.ServerSaveRequest{
		Name:     "HK-Node",
		Group:    "Prod",
		Host:     "10.0.0.10",
		Port:     2222,
		Username: "ubuntu",
		AuthType: models.AuthBoth,
		Password: "super-secret-password-999",
		SSHKeyID: meta.ID,
		Proxy: models.ProxySaveReq{
			Type:     models.ProxySOCKS5,
			Host:     "127.0.0.1",
			Port:     1080,
			Username: "proxyuser",
			Password: "proxy-secret-password",
		},
	})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	if !srv.HasPassword || !srv.Proxy.HasPassword {
		t.Fatal("expected HasPassword flags to be true")
	}

	// Save a shortcut
	sc, err := store.SaveShortcut(models.CommandShortcut{
		Name:        "重启Nginx",
		Command:     "sudo nginx -t && sudo systemctl reload nginx",
		Category:    "运维",
		AutoExecute: true,
	})
	if err != nil {
		t.Fatalf("SaveShortcut failed: %v", err)
	}

	// Record a command and close store to flush debounced history writes
	if _, err := store.RecordCommand(srv.ID, srv.Name, "sess-1", "docker compose up -d"); err != nil {
		t.Fatalf("RecordCommand failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close vault failed: %v", err)
	}

	// Verify raw file on disk contains NO plaintext secrets (keys, passwords, shortcuts)
	diskBytes, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("read vault file failed: %v", err)
	}
	for _, needle := range []string{
		"BEGIN OPENSSH PRIVATE KEY",
		"super-secret-password-999",
		"proxy-secret-password",
		"key-secret-pass",
		"sudo nginx -t",
		"docker compose up -d",
	} {
		if bytes.Contains(diskBytes, []byte(needle)) {
			t.Fatalf("plaintext secret %q leaked into encrypted vault file on disk!", needle)
		}
	}

	// Re-open vault from disk and verify everything loads back cleanly
	store2, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Re-open vault failed: %v", err)
	}

	keys := store2.ListSSHKeys()
	if len(keys) != 1 || keys[0].ID != meta.ID || keys[0].UsageCount != 1 {
		t.Fatalf("unexpected keys after reload: %+v", keys)
	}
	internalKey, err := store2.GetSSHKeyInternal(meta.ID)
	if err != nil || internalKey.Passphrase != "key-secret-pass" {
		t.Fatalf("internal key reload mismatch: %v", err)
	}

	// Update server keeping existing password
	updated, err := store2.SaveServer(models.ServerSaveRequest{
		ID:                   srv.ID,
		Name:                 "HK-Node-Renamed",
		Host:                 "10.0.0.10",
		Port:                 2222,
		Username:             "ubuntu",
		AuthType:             models.AuthBoth,
		KeepExistingPassword: true,
		SSHKeyID:             meta.ID,
		Proxy: models.ProxySaveReq{
			Type:                 models.ProxySOCKS5,
			Host:                 "127.0.0.1",
			Port:                 1080,
			Username:             "proxyuser",
			KeepExistingPassword: true,
		},
	})
	if err != nil {
		t.Fatalf("update server failed: %v", err)
	}
	if !updated.HasPassword || !updated.Proxy.HasPassword {
		t.Fatal("KeepExistingPassword failed to preserve secrets")
	}
	internalSrv, err := store2.GetServerInternal(srv.ID)
	if err != nil || internalSrv.Password != "super-secret-password-999" || internalSrv.Proxy.Password != "proxy-secret-password" {
		t.Fatalf("preserved passwords mismatch")
	}

	// Verify shortcut and history persisted
	foundSc := false
	for _, item := range store2.ListShortcuts() {
		if item.ID == sc.ID && item.Command == "sudo nginx -t && sudo systemctl reload nginx" {
			foundSc = true
		}
	}
	if !foundSc {
		t.Fatal("shortcut not found after reload")
	}
	hist := store2.ListHistory(srv.ID, 10)
	if len(hist) != 1 || hist[0].Command != "docker compose up -d" {
		t.Fatalf("unexpected history: %+v", hist)
	}
	_ = store2.Close()
}

type failingProtector struct {
	failProtect bool
}

func (f *failingProtector) Protect(p []byte) ([]byte, error) {
	if f.failProtect {
		return nil, os.ErrPermission
	}
	return append([]byte(nil), p...), nil
}
func (f *failingProtector) Unprotect(c []byte) ([]byte, error) { return append([]byte(nil), c...), nil }
func (f *failingProtector) EngineName() string                 { return "test-mock" }
func (f *failingProtector) IsMachineBound() bool               { return false }
func (f *failingProtector) IsUserBound() bool                  { return false }

func TestVaultMutationRollbackOnSaveFailure(t *testing.T) {
	fp := &failingProtector{}
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vaultPath, fp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	srv, err := store.SaveServer(models.ServerSaveRequest{Name: "S1", Host: "10.0.0.1", Username: "root"})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}

	// Force saveLocked() to fail and verify RAM is rolled back cleanly
	fp.failProtect = true
	if _, err := store.SaveServer(models.ServerSaveRequest{Name: "S2", Host: "10.0.0.2", Username: "root"}); err == nil {
		t.Fatal("expected SaveServer to fail when protector fails")
	}
	if len(store.ListServers()) != 1 {
		t.Fatalf("expected rollback to 1 server, got %d", len(store.ListServers()))
	}
	if err := store.DeleteServer(srv.ID); err == nil {
		t.Fatal("expected DeleteServer to fail")
	}
	if len(store.ListServers()) != 1 {
		t.Fatalf("expected DeleteServer rollback to keep server, got %d", len(store.ListServers()))
	}

	// Verify UTF-8 rune slicing in SaveShortcut auto-name
	fp.failProtect = false
	chineseCmd := "echo 一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十"
	sc, err := store.SaveShortcut(models.CommandShortcut{Command: chineseCmd})
	if err != nil {
		t.Fatalf("SaveShortcut failed: %v", err)
	}
	if !bytes.Equal([]byte(sc.Name), []byte(string([]rune(chineseCmd)[:32])+"...")) {
		t.Fatalf("expected clean UTF-8 rune truncation, got %q", sc.Name)
	}
}

type countingProtector struct {
	protectCalls int
}

func (c *countingProtector) Protect(p []byte) ([]byte, error) {
	c.protectCalls++
	return append([]byte(nil), p...), nil
}
func (c *countingProtector) Unprotect(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }
func (c *countingProtector) EngineName() string                 { return "counting-mock" }
func (c *countingProtector) IsMachineBound() bool               { return false }
func (c *countingProtector) IsUserBound() bool                  { return false }

func TestRecordCommandCoalescingAndCachedPasswordSave(t *testing.T) {
	cp := &countingProtector{}
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vaultPath, cp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := store.SetPassword("", "fast-pw-123"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}

	callsBefore := cp.protectCalls
	for i := 0; i < 10; i++ {
		if _, err := store.RecordCommand("srv-1", "Node-1", "sess-1", "ls -la"); err != nil {
			t.Fatalf("RecordCommand #%d failed: %v", i, err)
		}
	}
	if cp.protectCalls != callsBefore {
		t.Fatalf("expected RecordCommand writes to coalesce instead of writing immediately: before=%d after=%d", callsBefore, cp.protectCalls)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if cp.protectCalls != callsBefore+1 {
		t.Fatalf("expected exactly 1 coalesced flush on Close, got %d extra writes", cp.protectCalls-callsBefore)
	}

	reopened, err := Open(vaultPath, cp)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer reopened.Close()
	if err := reopened.Unlock("fast-pw-123"); err != nil {
		t.Fatalf("Unlock failed after cached-key save: %v", err)
	}
	if hist := reopened.ListHistory("srv-1", 20); len(hist) != 10 {
		t.Fatalf("expected 10 persisted history entries, got %d", len(hist))
	}
}
