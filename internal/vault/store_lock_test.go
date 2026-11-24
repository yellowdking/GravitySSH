package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/crypto"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestVaultPasswordLockUnlockChangeAndRemove(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "vault.gsv")

	store, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	st := store.LockState()
	if st.Locked || st.HasPassword || st.SchemaVersion != vaultSchemaVersion {
		t.Fatalf("unexpected initial lock state: %+v", st)
	}

	srv, err := store.SaveServer(models.ServerSaveRequest{
		Name:     "Secret-Node",
		Host:     "10.0.1.5",
		Username: "root",
		Password: "node-password-42",
	})
	if err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}

	// Set initial password (empty currentPassword allowed)
	if err := store.SetPassword("", "pass-alpha"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}
	if !store.LockState().HasPassword {
		t.Fatal("expected HasPassword=true after setting password")
	}

	// Re-open vault: must start locked without decrypting
	reopened, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Re-open locked vault failed: %v", err)
	}
	st2 := reopened.LockState()
	if !st2.Locked || !st2.HasPassword {
		t.Fatalf("expected locked=true, hasPassword=true, got %+v", st2)
	}
	if len(reopened.ListServers()) != 0 {
		t.Fatal("expected no decrypted servers while locked")
	}

	// Wrong password must be rejected
	if err := reopened.Unlock("wrong-pass"); err == nil {
		t.Fatal("expected wrong password to fail Unlock")
	}
	if !reopened.IsLocked() {
		t.Fatal("expected vault to remain locked after wrong password")
	}

	// Correct password unlocks and restores data
	if err := reopened.Unlock("pass-alpha"); err != nil {
		t.Fatalf("Unlock with right password failed: %v", err)
	}
	if reopened.IsLocked() {
		t.Fatal("expected vault to be unlocked")
	}
	internalSrv, err := reopened.GetServerInternal(srv.ID)
	if err != nil || internalSrv.Password != "node-password-42" {
		t.Fatalf("restored server mismatch: %+v, err=%v", internalSrv, err)
	}

	// Change password rejects wrong current password
	if err := reopened.SetPassword("bad-cur", "pass-beta"); err == nil {
		t.Fatal("expected SetPassword with wrong current password to fail")
	}

	// Change password succeeds with right current password
	if err := reopened.SetPassword("pass-alpha", "pass-beta"); err != nil {
		t.Fatalf("change password failed: %v", err)
	}

	reopened2, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("reopen after password change failed: %v", err)
	}
	if err := reopened2.Unlock("pass-alpha"); err == nil {
		t.Fatal("old password should no longer work")
	}
	if err := reopened2.Unlock("pass-beta"); err != nil {
		t.Fatalf("new password failed to unlock: %v", err)
	}

	// Remove password (newPassword == "") returns to Mode 0x01 zero-prompt
	if err := reopened2.SetPassword("pass-beta", ""); err != nil {
		t.Fatalf("remove password failed: %v", err)
	}
	reopened3, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("reopen after removing password failed: %v", err)
	}
	if reopened3.IsLocked() || reopened3.LockState().HasPassword {
		t.Fatalf("expected unlocked & HasPassword=false, got %+v", reopened3.LockState())
	}
}

func TestVaultForwardCompatibleSchemaAndLegacyMigration(t *testing.T) {
	// Test forward-compatible JSON decoding (future unknown fields + missing settings)
	vaultPath := filepath.Join(t.TempDir(), "future.gsv")
	enc := crypto.NewEnvelopeCrypto(nil)
	futurePayload := []byte(`{"version":99,"futureFeatureFlag":true,"servers":[{"id":"s1","name":"Future","host":"1.2.3.4","port":22,"username":"root","unknownField":"ok"}]}`)
	sealed, err := enc.Seal(futurePayload)
	if err != nil {
		t.Fatalf("seal failed: %v", err)
	}
	if err := os.WriteFile(vaultPath, sealed, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	st, err := Open(vaultPath, nil)
	if err != nil {
		t.Fatalf("Open future schema failed: %v", err)
	}
	if len(st.ListServers()) != 1 || st.LockState().SchemaVersion != 99 {
		t.Fatalf("unexpected future vault state: servers=%d ver=%d", len(st.ListServers()), st.LockState().SchemaVersion)
	}
	if st.GetSettings().TerminalFontSize <= 0 {
		t.Fatal("expected missing settings fields to default via normalizeVault")
	}

	// Test portable path + legacy migration
	exeDir := filepath.Join(t.TempDir(), "app")
	userCfg := filepath.Join(t.TempDir(), "appdata")
	legacyDir := filepath.Join(userCfg, "GravitySSH")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("mkdir legacy failed: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "vault.gsv")
	if err := os.WriteFile(legacyPath, sealed, 0o600); err != nil {
		t.Fatalf("write legacy failed: %v", err)
	}

	resolved, err := resolveDefaultVaultPath(exeDir, userCfg)
	if err != nil {
		t.Fatalf("resolveDefaultVaultPath failed: %v", err)
	}
	wantPortable := filepath.Join(exeDir, "config", "vault.gsv")
	if resolved != wantPortable {
		t.Fatalf("got path %q, want %q", resolved, wantPortable)
	}
	migrated, err := os.ReadFile(resolved)
	if err != nil || !bytes.Equal(migrated, sealed) {
		t.Fatalf("expected legacy vault auto-migrated to portable path, err=%v", err)
	}

	// Test fallback when exeDir/config is not writable (exeDir points to a regular file)
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	_ = os.WriteFile(blockingFile, []byte("x"), 0o600)
	fallbackResolved, err := resolveDefaultVaultPath(blockingFile, userCfg)
	if err != nil {
		t.Fatalf("fallback resolveDefaultVaultPath failed: %v", err)
	}
	wantFallback := filepath.Join(userCfg, "GravitySSH", "config", "vault.gsv")
	if fallbackResolved != wantFallback {
		t.Fatalf("got fallback %q, want %q", fallbackResolved, wantFallback)
	}
	if _, err := json.Marshal(st.LockState()); err != nil {
		t.Fatalf("marshal LockState failed: %v", err)
	}
}
