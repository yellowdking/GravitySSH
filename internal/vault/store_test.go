package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
func (c *countingProtector) Unprotect(b []byte) ([]byte, error) {
	return append([]byte(nil), b...), nil
}
func (c *countingProtector) EngineName() string   { return "counting-mock" }
func (c *countingProtector) IsMachineBound() bool { return false }
func (c *countingProtector) IsUserBound() bool    { return false }

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

func TestMarkServerConnectedAndShortcutUseCoalesceAndBindKey(t *testing.T) {
	cp := &countingProtector{}
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vaultPath, cp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	srv, err := store.SaveServer(models.ServerSaveRequest{Name: "S1", Host: "10.0.0.1", Username: "root", Password: "pw"})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	sc, err := store.SaveShortcut(models.CommandShortcut{Name: "Uptime", Command: "uptime"})
	if err != nil {
		t.Fatalf("SaveShortcut failed: %v", err)
	}

	callsBefore := cp.protectCalls
	store.MarkServerConnected(srv.ID)
	store.IncrementShortcutUse(sc.ID)
	store.IncrementShortcutUse(sc.ID)
	if cp.protectCalls != callsBefore {
		t.Fatalf("expected MarkServerConnected and IncrementShortcutUse to coalesce instead of immediate disk encryption: before=%d after=%d", callsBefore, cp.protectCalls)
	}

	if err := store.BindSSHKeyToServer(srv.ID, "key-123"); err != nil {
		t.Fatalf("BindSSHKeyToServer failed: %v", err)
	}
	boundSrv, err := store.GetServerInternal(srv.ID)
	if err != nil {
		t.Fatalf("GetServerInternal failed: %v", err)
	}
	if boundSrv.SSHKeyID != "key-123" || boundSrv.AuthType != models.AuthBoth || boundSrv.LastConnectedAt == nil {
		t.Fatalf("unexpected bound server state: %+v", boundSrv)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	reopened, err := Open(vaultPath, cp)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer reopened.Close()
	gotSrv, err := reopened.GetServerInternal(srv.ID)
	if err != nil || gotSrv.LastConnectedAt == nil || gotSrv.SSHKeyID != "key-123" {
		t.Fatalf("persisted server mismatch after coalesce+bind: %+v err=%v", gotSrv, err)
	}
	var gotUse int
	for _, item := range reopened.ListShortcuts() {
		if item.ID == sc.ID {
			gotUse = item.UseCount
		}
	}
	if gotUse != 2 {
		t.Fatalf("expected UseCount=2 persisted, got %d", gotUse)
	}
}

func TestVaultOpenRecoversFromDeterministicBackup(t *testing.T) {
	// Case 1: Mode 0x01 primary missing, valid vault.gsv.bak exists (crash during backup-and-swap).
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	bakPath := vaultBackupPath(vaultPath)

	s, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	srv, err := s.SaveServer(models.ServerSaveRequest{Name: "Prod-Recover", Host: "10.9.0.1", Username: "root", Password: "pw1"})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if err := os.Rename(vaultPath, bakPath); err != nil {
		t.Fatalf("simulate missing primary via rename: %v", err)
	}
	recovered, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open failed to recover from %s: %v", bakPath, err)
	}
	gotSrv, err := recovered.GetServerInternal(srv.ID)
	if err != nil || gotSrv.Password != "pw1" {
		t.Fatalf("recovered server mismatch: %+v err=%v", gotSrv, err)
	}
	if _, err := os.Stat(vaultPath); err != nil {
		t.Fatalf("expected primary vault.gsv to be restored after recovery: %v", err)
	}
	_ = recovered.Close()

	// Case 2: Mode 0x02 password-protected primary missing, valid vault.gsv.bak exists.
	pwPath := filepath.Join(t.TempDir(), "vault.gsv")
	pwBak := vaultBackupPath(pwPath)
	s2, err := Open(pwPath, nil)
	if err != nil {
		t.Fatalf("Open pw vault failed: %v", err)
	}
	srv2, err := s2.SaveServer(models.ServerSaveRequest{Name: "Locked-Recover", Host: "10.9.0.2", Username: "admin", Password: "pw2"})
	if err != nil {
		t.Fatalf("SaveServer pw failed: %v", err)
	}
	if err := s2.SetPassword("", "vault-secret-pw"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}
	_ = s2.Close()

	if err := os.Rename(pwPath, pwBak); err != nil {
		t.Fatalf("rename pw vault to bak: %v", err)
	}
	rec2, err := Open(pwPath, nil)
	if err != nil {
		t.Fatalf("Open locked vault backup recovery failed: %v", err)
	}
	if !rec2.IsLocked() || !rec2.LockState().HasPassword {
		t.Fatalf("expected recovered password vault to stay locked")
	}
	if err := rec2.Unlock("vault-secret-pw"); err != nil {
		t.Fatalf("Unlock recovered backup failed: %v", err)
	}
	if got2, err := rec2.GetServerInternal(srv2.ID); err != nil || got2.Password != "pw2" {
		t.Fatalf("recovered locked server mismatch: %+v err=%v", got2, err)
	}
	_ = rec2.Close()

	// Case 3: Corrupted primary with valid vault.gsv.bak recovers cleanly.
	rawValid, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("read valid vault: %v", err)
	}
	if err := os.WriteFile(bakPath, rawValid, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(vaultPath, []byte("corrupted-bytes"), 0o600); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}
	rec3, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("expected Open to recover corrupted primary from backup: %v", err)
	}
	if got3, err := rec3.GetServerInternal(srv.ID); err != nil || got3.Password != "pw1" {
		t.Fatalf("corrupted primary recovery mismatch: %+v err=%v", got3, err)
	}
	_ = rec3.Close()

	// Case 4: Primary missing and backup corrupted must return error (never silently wipe).
	badDir := t.TempDir()
	badVault := filepath.Join(badDir, "vault.gsv")
	if err := os.WriteFile(vaultBackupPath(badVault), []byte("bad-bak"), 0o600); err != nil {
		t.Fatalf("write bad bak: %v", err)
	}
	if _, err := Open(badVault, nil); err == nil {
		t.Fatal("expected error when primary is missing and backup is corrupted")
	}
}

type threadSafeFailingProtector struct {
	mu          sync.Mutex
	failProtect bool
	calls       int
}

func (f *threadSafeFailingProtector) setFail(v bool) {
	f.mu.Lock()
	f.failProtect = v
	f.mu.Unlock()
}

func (f *threadSafeFailingProtector) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *threadSafeFailingProtector) Protect(p []byte) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	fail := f.failProtect
	f.mu.Unlock()
	if fail {
		return nil, os.ErrPermission
	}
	return append([]byte(nil), p...), nil
}
func (f *threadSafeFailingProtector) Unprotect(c []byte) ([]byte, error) {
	return append([]byte(nil), c...), nil
}
func (f *threadSafeFailingProtector) EngineName() string   { return "ts-mock" }
func (f *threadSafeFailingProtector) IsMachineBound() bool { return false }
func (f *threadSafeFailingProtector) IsUserBound() bool    { return false }

func TestVaultFlushRetryBackoffAndClosedRejection(t *testing.T) {
	fp := &threadSafeFailingProtector{}
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vaultPath, fp)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Speed up flush & retry delays for deterministic testing.
	store.mu.Lock()
	store.flushDelay = 15 * time.Millisecond
	store.maxRetryDelay = 40 * time.Millisecond
	store.mu.Unlock()

	// Force initial debounced flush to fail and verify it reschedules and eventually persists on recovery.
	fp.setFail(true)
	callsBefore := fp.callCount()
	if _, err := store.RecordCommand("srv-1", "Node", "sess-1", "uname -a"); err != nil {
		t.Fatalf("RecordCommand failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fp.callCount() >= callsBefore+2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fp.callCount() < callsBefore+2 {
		t.Fatalf("expected debounced flush to retry after write failure, calls=%d", fp.callCount()-callsBefore)
	}

	// Allow disk write to succeed and wait for retry timer to clear dirty flag.
	fp.setFail(false)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.RLock()
		dirty := store.dirty
		failures := store.flushFailures
		store.mu.RUnlock()
		if !dirty && failures == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	store.mu.RLock()
	stillDirty := store.dirty
	store.mu.RUnlock()
	if stillDirty {
		t.Fatal("expected retried debounced flush to persist and clear dirty flag")
	}

	// Verify stale timer #1 waiting on s.mu cannot clobber newer timer #2.
	store.mu.Lock()
	store.flushDelay = 20 * time.Millisecond
	store.startFlushTimerLocked(10 * time.Millisecond) // T1 fires while we hold s.mu
	time.Sleep(30 * time.Millisecond)                  // ensure T1 callback is spawned and blocked on s.mu
	store.flushTimer = nil                             // simulate Flush/saveLocked clearing T1
	store.dirty = true
	store.startFlushTimerLocked(500 * time.Millisecond) // T2 armed
	t2 := store.flushTimer
	store.mu.Unlock()

	time.Sleep(40 * time.Millisecond) // let T1 acquire s.mu and exit cleanly
	store.mu.RLock()
	curTimer := store.flushTimer
	curDirty := store.dirty
	store.mu.RUnlock()
	if curTimer != t2 || !curDirty {
		t.Fatalf("stale timer T1 clobbered T2 or flushed prematurely: sameTimer=%v dirty=%v", curTimer == t2, curDirty)
	}

	// Verify Close() is idempotent and rejects all subsequent mutations & saves.
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close should be idempotent, got %v", err)
	}
	if err := store.Flush(); !errors.Is(err, errVaultClosed) {
		t.Fatalf("expected errVaultClosed on Flush after Close, got %v", err)
	}
	if _, err := store.SaveServer(models.ServerSaveRequest{Name: "PostClose", Host: "10.0.0.9"}); !errors.Is(err, errVaultClosed) {
		t.Fatalf("expected errVaultClosed on SaveServer after Close, got %v", err)
	}
	if err := store.SetPassword("", "should-not-set"); !errors.Is(err, errVaultClosed) {
		t.Fatalf("expected errVaultClosed on SetPassword after Close, got %v", err)
	}
	if _, err := store.RecordCommand("srv-1", "Node", "sess-1", "ls"); !errors.Is(err, errVaultClosed) {
		t.Fatalf("expected errVaultClosed on RecordCommand after Close, got %v", err)
	}
	if _, err := store.SaveShortcut(models.CommandShortcut{Command: "uptime"}); !errors.Is(err, errVaultClosed) {
		t.Fatalf("expected errVaultClosed on SaveShortcut after Close, got %v", err)
	}
	if _, err := store.SaveTemplate(models.BuiltinTemplate{Name: "T", Command: "echo 1"}); !errors.Is(err, errVaultClosed) {
		t.Fatalf("expected errVaultClosed on SaveTemplate after Close, got %v", err)
	}
	if _, err := store.ExportSyncBundle("PC"); !errors.Is(err, errVaultClosed) {
		t.Fatalf("expected errVaultClosed on ExportSyncBundle after Close, got %v", err)
	}
	if store.pwKey != nil || store.pwSalt != nil {
		t.Fatal("expected pwKey/pwSalt to remain nil after Close")
	}
}
