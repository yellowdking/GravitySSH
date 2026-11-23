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

	sc, err := store.SaveShortcut(models.CommandShortcut{
		Name:        "重启Nginx",
		Command:     "sudo nginx -t && sudo systemctl reload nginx",
		Category:    "运维",
		AutoExecute: true,
	})
	if err != nil {
		t.Fatalf("SaveShortcut failed: %v", err)
	}

	if _, err := store.RecordCommand(srv.ID, srv.Name, "sess-1", "docker compose up -d"); err != nil {
		t.Fatalf("RecordCommand failed: %v", err)
	}

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
}
