package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/crypto"
)

func TestVaultForwardCompatibleSchemaAndLegacyMigration(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "future.gsv")
	enc := crypto.NewEnvelopeCrypto(nil)
	sealed, err := enc.Seal([]byte(`{"version":99,"futureFlag":true,"servers":[{"id":"s1","name":"Future","host":"1.2.3.4","port":22,"username":"root","unknownField":"ok"}]}`))
	if err != nil {
		t.Fatalf("seal failed: %v", err)
	}
	_ = os.WriteFile(vaultPath, sealed, 0o600)
	st, err := Open(vaultPath, nil)
	if err != nil || len(st.ListServers()) != 1 || st.LockState().SchemaVersion != 99 || st.GetSettings().TerminalFontSize <= 0 {
		t.Fatalf("future schema open failed: err=%v", err)
	}

	// Migrate from %APPDATA%/GravitySSH/vault.gsv -> <exeDir>/gsshconfig/vault.gsv
	exeDir := filepath.Join(t.TempDir(), "app")
	userCfg := filepath.Join(t.TempDir(), "appdata")
	_ = os.MkdirAll(filepath.Join(userCfg, "GravitySSH"), 0o700)
	_ = os.WriteFile(filepath.Join(userCfg, "GravitySSH", "vault.gsv"), sealed, 0o600)

	resolved, err := resolveDefaultVaultPath(exeDir, userCfg)
	wantPortable := filepath.Join(exeDir, "gsshconfig", "vault.gsv")
	if err != nil || resolved != wantPortable {
		t.Fatalf("got (%q, %v), want %q", resolved, err, wantPortable)
	}
	if migrated, err := os.ReadFile(resolved); err != nil || !bytes.Equal(migrated, sealed) {
		t.Fatalf("expected legacy appdata vault migrated, err=%v", err)
	}

	// Migrate from v1.1.0 <exeDir>/config/vault.gsv -> <exeDir>/gsshconfig/vault.gsv
	exeDir2 := filepath.Join(t.TempDir(), "app2")
	_ = os.MkdirAll(filepath.Join(exeDir2, "config"), 0o700)
	_ = os.WriteFile(filepath.Join(exeDir2, "config", "vault.gsv"), sealed, 0o600)
	resolved2, err := resolveDefaultVaultPath(exeDir2, userCfg)
	wantPortable2 := filepath.Join(exeDir2, "gsshconfig", "vault.gsv")
	if err != nil || resolved2 != wantPortable2 {
		t.Fatalf("got (%q, %v), want %q", resolved2, err, wantPortable2)
	}
	if migrated2, err := os.ReadFile(resolved2); err != nil || !bytes.Equal(migrated2, sealed) {
		t.Fatalf("expected v1.1.0 config/vault.gsv migrated to gsshconfig, err=%v", err)
	}

	// Fallback when exeDir is not writable
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	_ = os.WriteFile(blockingFile, []byte("x"), 0o600)
	fallbackResolved, err := resolveDefaultVaultPath(blockingFile, userCfg)
	wantFallback := filepath.Join(userCfg, "GravitySSH", "gsshconfig", "vault.gsv")
	if err != nil || fallbackResolved != wantFallback {
		t.Fatalf("got fallback (%q, %v), want %q", fallbackResolved, err, wantFallback)
	}
	if _, err := json.Marshal(st.LockState()); err != nil {
		t.Fatalf("marshal LockState failed: %v", err)
	}
}

func TestMigrateFirstExistingValidatesMagicAndReturnsErrors(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.gsv")
	validV1 := filepath.Join(dir, "legacy_v1.gsv")
	target := filepath.Join(dir, "dest", "vault.gsv")
	_ = os.MkdirAll(filepath.Dir(target), 0o700)
	_ = os.WriteFile(garbage, []byte("NOT-A-VAULT-HEADER"), 0o600)
	v1Payload := []byte("GSV1\x00\x00\x00\x04test-payload")
	_ = os.WriteFile(validV1, v1Payload, 0o600)

	// Must skip garbage candidate without GSV1/GSV2 magic and migrate validV1 atomically.
	if err := migrateFirstExisting([]string{garbage, validV1}, target); err != nil {
		t.Fatalf("migrateFirstExisting failed: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, v1Payload) {
		t.Fatalf("migrated payload mismatch: got=%q err=%v", got, err)
	}

	// Write failure to non-existent parent dir must return error instead of swallowing it.
	badTarget := filepath.Join(dir, "nonexistent-parent", "vault.gsv")
	if err := migrateFirstExisting([]string{validV1}, badTarget); err == nil {
		t.Fatal("expected migrateFirstExisting to return error when target directory is missing")
	}
}
