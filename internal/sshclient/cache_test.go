package sshclient

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

func makeOpenSSHHash(salt []byte, target string) string {
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(target))
	return "|1|" + base64.StdEncoding.EncodeToString(salt) + "|" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestSSHCacheClearHostPlainBracketCommaAndHashed(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	oldPath := khPath + ".old"

	salt1 := []byte("01234567890123456789")
	salt2 := []byte("abcdefghijklmnopqrst")
	hashedTarget := makeOpenSSHHash(salt1, "10.0.0.5")
	hashedTargetPort := makeOpenSSHHash(salt2, "[10.0.0.5]:2222")
	hashedOther := makeOpenSSHHash(salt1, "192.168.1.99")

	initial := strings.Join([]string{
		"# user comment line",
		"",
		"10.0.0.5 ssh-ed25519 AAAAC3NzaPlain",
		"[10.0.0.5]:2222 ssh-ed25519 AAAAC3NzaPort2222",
		"[10.0.0.5]:3333 ssh-ed25519 AAAAC3NzaPort3333",
		"app.internal,10.0.0.5 ssh-rsa AAAAB3CommaKeep",
		"10.0.0.5,[10.0.0.5]:2222 ssh-ed25519 AAAAC3NzaCommaDropAll",
		hashedTarget + " ssh-ed25519 AAAAC3NzaHashPlain",
		hashedTargetPort + " ssh-ed25519 AAAAC3NzaHashPort",
		hashedOther + " ssh-ed25519 AAAAC3NzaHashOther",
		"5.6.7.8 ssh-ed25519 AAAAC3NzaKeepOther",
	}, "\n") + "\n"

	if err := os.WriteFile(khPath, []byte(initial), 0600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("10.0.0.5 ssh-ed25519 AAAAOld\n8.8.8.8 ssh-ed25519 AAAAKeepOld\n"), 0600); err != nil {
		t.Fatalf("write known_hosts.old: %v", err)
	}

	if got := CountKnownHosts(khPath); got != 9 {
		t.Fatalf("CountKnownHosts = %d, want 9", got)
	}

	removed, err := ClearKnownHostsHost(khPath, "10.0.0.5", 2222)
	if err != nil {
		t.Fatalf("ClearKnownHostsHost error: %v", err)
	}
	// 6 matching entries in known_hosts + 1 in known_hosts.old
	if removed != 7 {
		t.Fatalf("removed = %d, want 7", removed)
	}

	rawAfter, err := os.ReadFile(khPath)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	after := string(rawAfter)

	for _, want := range []string{
		"# user comment line",
		"[10.0.0.5]:3333 ssh-ed25519 AAAAC3NzaPort3333",
		"app.internal ssh-rsa AAAAB3CommaKeep",
		hashedOther + " ssh-ed25519 AAAAC3NzaHashOther",
		"5.6.7.8 ssh-ed25519 AAAAC3NzaKeepOther",
	} {
		if !strings.Contains(after, want) {
			t.Errorf("expected known_hosts to preserve %q, got:\n%s", want, after)
		}
	}

	for _, gone := range []string{
		"AAAAC3NzaPlain",
		"AAAAC3NzaPort2222",
		"AAAAC3NzaCommaDropAll",
		"AAAAC3NzaHashPlain",
		"AAAAC3NzaHashPort",
		"app.internal,10.0.0.5",
	} {
		if strings.Contains(after, gone) {
			t.Errorf("expected known_hosts to remove %q, got:\n%s", gone, after)
		}
	}

	rawOld, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("read known_hosts.old: %v", err)
	}
	if strings.Contains(string(rawOld), "10.0.0.5") || !strings.Contains(string(rawOld), "8.8.8.8") {
		t.Fatalf("unexpected known_hosts.old content:\n%s", string(rawOld))
	}
}

func TestSSHCacheClearHostPortZeroMatchesAllPorts(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	content := "1.2.3.4 ssh-ed25519 AAAA1\n[1.2.3.4]:2222 ssh-ed25519 AAAA2\n9.9.9.9 ssh-ed25519 AAAA3\n"
	if err := os.WriteFile(khPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	removed, err := ClearKnownHostsHost(khPath, "1.2.3.4", 0)
	if err != nil {
		t.Fatalf("ClearKnownHostsHost error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	after, _ := os.ReadFile(khPath)
	if strings.TrimSpace(string(after)) != "9.9.9.9 ssh-ed25519 AAAA3" {
		t.Fatalf("unexpected content: %q", string(after))
	}
}

func TestSSHCacheClearAllCreatesOneTimeBackup(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	original := "# header\n1.1.1.1 ssh-ed25519 AAAA1\n2.2.2.2 ssh-ed25519 AAAA2\n"
	if err := os.WriteFile(khPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	removed, err := ClearAllKnownHosts(khPath)
	if err != nil {
		t.Fatalf("ClearAllKnownHosts error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if got := CountKnownHosts(khPath); got != 0 {
		t.Fatalf("CountKnownHosts after clear = %d, want 0", got)
	}

	bakBytes, err := os.ReadFile(khPath + ".gssh.bak")
	if err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	if string(bakBytes) != original {
		t.Fatalf("backup mismatch: got %q", string(bakBytes))
	}

	// Second clear with new content should NOT overwrite the original .gssh.bak.
	if err := os.WriteFile(khPath, []byte("3.3.3.3 ssh-ed25519 AAAA3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClearAllKnownHosts(khPath); err != nil {
		t.Fatalf("second ClearAllKnownHosts error: %v", err)
	}
	bakBytes2, _ := os.ReadFile(khPath + ".gssh.bak")
	if string(bakBytes2) != original {
		t.Fatalf("backup was overwritten on second clear")
	}
}

func TestSSHCacheManagerDirCacheAndMetricsReset(t *testing.T) {
	mgr := NewManager(nil, nil)
	mgr.sessions["s1"] = &sessionState{
		info: models.SessionInfo{SessionID: "s1", Host: "10.0.0.5", Port: 22},
		dirCache: map[string]dirCacheEntry{
			"/root": {fetchedAt: time.Now()},
			"/tmp":  {fetchedAt: time.Now()},
		},
		prevCPU:      &cpuSample{total: 100, idle: 50},
		prevNet:      &netSample{rx: 10, tx: 20},
		prevSampleAt: time.Now(),
	}
	mgr.sessions["s2"] = &sessionState{
		info: models.SessionInfo{SessionID: "s2", Host: "10.0.0.6", Port: 22},
		dirCache: map[string]dirCacheEntry{
			"/var": {fetchedAt: time.Now()},
		},
	}

	info := mgr.GetSSHCacheInfo()
	if info.DirCacheCount != 3 {
		t.Fatalf("DirCacheCount = %d, want 3", info.DirCacheCount)
	}

	if cleared := mgr.clearSessionCaches("10.0.0.5"); cleared != 2 {
		t.Fatalf("cleared for 10.0.0.5 = %d, want 2", cleared)
	}
	if mgr.sessions["s1"].prevCPU != nil || mgr.sessions["s1"].prevNet != nil || !mgr.sessions["s1"].prevSampleAt.IsZero() {
		t.Fatalf("expected s1 cached metrics to be reset")
	}
	if len(mgr.sessions["s2"].dirCache) != 1 {
		t.Fatalf("expected s2 dirCache untouched")
	}

	if cleared := mgr.clearSessionCaches(""); cleared != 1 {
		t.Fatalf("cleared all remaining = %d, want 1", cleared)
	}
}
