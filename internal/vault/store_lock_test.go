package vault

import (
	"path/filepath"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestVaultPasswordLockUnlockChangeAndRemove(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if st := store.LockState(); st.Locked || st.HasPassword || st.SchemaVersion != vaultSchemaVersion {
		t.Fatalf("unexpected initial lock state: %+v", st)
	}

	srv, err := store.SaveServer(models.ServerSaveRequest{Name: "Secret-Node", Host: "10.0.1.5", Username: "root", Password: "node-password-42"})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	if err := store.SetPassword("", "pass-alpha"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}

	reopened, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Re-open locked vault failed: %v", err)
	}
	if st := reopened.LockState(); !st.Locked || !st.HasPassword || len(reopened.ListServers()) != 0 {
		t.Fatalf("expected locked empty view, got %+v", st)
	}
	// Locked vault must NOT leak fabricated default counts or accept mutations
	if info := reopened.SecurityInfo(); info.ServerCount != 0 || info.KeyCount != 0 || info.ShortcutCount != 0 {
		t.Fatalf("expected zero counts while locked, got %+v", info)
	}
	if len(reopened.ListShortcuts()) != 0 || len(reopened.ListTemplates()) != 0 {
		t.Fatalf("expected empty shortcuts/templates while locked")
	}
	if _, err := reopened.SaveServer(models.ServerSaveRequest{Name: "Blocked", Host: "10.0.0.9"}); err == nil {
		t.Fatal("expected SaveServer to fail while locked")
	}
	if len(reopened.ListServers()) != 0 {
		t.Fatal("locked SaveServer must not mutate RAM")
	}
	if err := reopened.Unlock("wrong-pass"); err == nil || !reopened.IsLocked() {
		t.Fatal("expected wrong password to fail and stay locked")
	}
	if err := reopened.Unlock("pass-alpha"); err != nil || reopened.IsLocked() {
		t.Fatalf("Unlock failed: %v", err)
	}
	if internalSrv, err := reopened.GetServerInternal(srv.ID); err != nil || internalSrv.Password != "node-password-42" {
		t.Fatalf("restored server mismatch: %+v, err=%v", internalSrv, err)
	}
	if err := reopened.SetPassword("bad-cur", "pass-beta"); err == nil {
		t.Fatal("expected wrong current password to fail")
	}
	if err := reopened.SetPassword("pass-alpha", "pass-beta"); err != nil {
		t.Fatalf("change password failed: %v", err)
	}

	reopened2, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("reopen2 failed: %v", err)
	}
	if err := reopened2.Unlock("pass-alpha"); err == nil {
		t.Fatal("old password should fail")
	}
	if err := reopened2.Unlock("pass-beta"); err != nil {
		t.Fatalf("new password failed: %v", err)
	}
	if err := reopened2.SetPassword("pass-beta", ""); err != nil {
		t.Fatalf("remove password failed: %v", err)
	}

	reopened3, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("reopen3 failed: %v", err)
	}
	if reopened3.IsLocked() || reopened3.LockState().HasPassword {
		t.Fatalf("expected unlocked & HasPassword=false, got %+v", reopened3.LockState())
	}
}

func TestUnlockResetsHasPasswordWhenVaultFileReplacedWithNoPasswordMode(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := store.SetPassword("", "pw-123"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}
	_ = store.Close()

	// Reopen in locked state (Mode 0x02).
	locked, err := Open(vaultPath, nil)
	if err != nil || !locked.IsLocked() {
		t.Fatalf("expected locked vault, err=%v", err)
	}
	defer locked.Close()

	// Externally replace vault.gsv with a Mode 0x01 (DPAPI-only, no password) vault.
	otherStore, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("open other: %v", err)
	}
	if err := otherStore.Unlock("pw-123"); err != nil {
		t.Fatalf("unlock other: %v", err)
	}
	if err := otherStore.SetPassword("pw-123", ""); err != nil {
		t.Fatalf("remove password on other: %v", err)
	}
	_ = otherStore.Close()

	// Calling Unlock on the still-locked handle succeeds with nil pwSalt/pwKey and MUST set hasPassword=false.
	if err := locked.Unlock("anything"); err != nil {
		t.Fatalf("Unlock after Mode 0x01 replacement failed: %v", err)
	}
	if locked.LockState().HasPassword {
		t.Fatal("expected HasPassword=false when unlocked from Mode 0x01 blob")
	}
	if _, err := locked.SaveServer(models.ServerSaveRequest{Name: "PostUnlock", Host: "10.0.0.8"}); err != nil {
		t.Fatalf("SaveServer failed after Mode 0x01 unlock: %v", err)
	}
}

func TestVaultManualLockFlushesClearsSecretsAndReunlocks(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")
	store, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// Lock without password must fail cleanly.
	if err := store.Lock(); err == nil {
		t.Fatal("expected Lock() to fail when vault has no password")
	}

	if _, err := store.SaveServer(models.ServerSaveRequest{Name: "LockSrv", Host: "10.0.5.5", Username: "root", Password: "pw"}); err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}
	if err := store.SetPassword("", "manual-lock-pass"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}
	// Record a debounced command right before Lock() -> Lock() must flush dirty state first.
	if _, err := store.RecordCommand("srv-1", "LockSrv", "sess-1", "uptime"); err != nil {
		t.Fatalf("RecordCommand failed: %v", err)
	}

	if err := store.Lock(); err != nil {
		t.Fatalf("Lock failed: %v", err)
	}
	// Idempotent second Lock() succeeds.
	if err := store.Lock(); err != nil {
		t.Fatalf("second Lock failed: %v", err)
	}
	if !store.IsLocked() || !store.LockState().HasPassword {
		t.Fatalf("expected locked state with password, got %+v", store.LockState())
	}
	if len(store.ListServers()) != 0 || len(store.ListHistory("", 10)) != 0 || store.pwKey != nil || store.pwSalt != nil {
		t.Fatal("expected secrets and cached password key cleared on Lock()")
	}
	if _, err := store.SaveServer(models.ServerSaveRequest{Name: "Blocked", Host: "10.0.5.6"}); err == nil {
		t.Fatal("expected mutations rejected after Lock()")
	}

	// Re-unlocking restores both saved server and the flushed debounced command history.
	if err := store.Unlock("manual-lock-pass"); err != nil {
		t.Fatalf("Unlock after Lock failed: %v", err)
	}
	if len(store.ListServers()) != 1 {
		t.Fatalf("expected 1 server restored after unlock, got %d", len(store.ListServers()))
	}
	hist := store.ListHistory("srv-1", 10)
	if len(hist) != 1 || hist[0].Command != "uptime" {
		t.Fatalf("expected flushed history 'uptime' restored after Lock+Unlock, got %+v", hist)
	}
}
