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
