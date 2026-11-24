package sshclient

import (
	"os"
	"strings"
	"testing"
)

func TestPickDominantMode(t *testing.T) {
	mode := pickDominantMode(map[os.FileMode]int{
		0o644: 3,
		0o755: 1,
		0o600: 1,
	}, 0o644)
	if mode != 0o644 {
		t.Fatalf("expected 0644, got %04o", mode)
	}

	// Tie prefers fallback
	modeTie := pickDominantMode(map[os.FileMode]int{
		0o644: 2,
		0o640: 2,
	}, 0o644)
	if modeTie != 0o644 {
		t.Fatalf("expected tie-breaker 0644, got %04o", modeTie)
	}

	// Empty uses fallback
	empty := pickDominantMode(nil, 0o755)
	if empty != 0o755 {
		t.Fatalf("expected fallback 0755, got %04o", empty)
	}
}

func TestBuildExtractCommand(t *testing.T) {
	zipCmd, err := buildExtractCommand("/var/www", "app bundle.zip")
	if err != nil {
		t.Fatalf("zip build failed: %v", err)
	}
	if !strings.Contains(zipCmd, "command -v unzip") || !strings.Contains(zipCmd, "apt-get") || !strings.Contains(zipCmd, "unzip -o 'app bundle.zip' -d .") {
		t.Fatalf("unexpected zip extract cmd: %s", zipCmd)
	}

	tgzCmd, err := buildExtractCommand("/opt/data", "backup.tar.gz")
	if err != nil || !strings.Contains(tgzCmd, "tar -xzf 'backup.tar.gz'") {
		t.Fatalf("unexpected tar.gz cmd: %q, err=%v", tgzCmd, err)
	}

	sevenCmd, err := buildExtractCommand("/tmp", "pack.7z")
	if err != nil || !strings.Contains(sevenCmd, "command -v 7z") || !strings.Contains(sevenCmd, "7z x -y 'pack.7z'") {
		t.Fatalf("unexpected 7z cmd: %q, err=%v", sevenCmd, err)
	}

	gzCmd, err := buildExtractCommand("/var/log", "app.log.gz")
	if err != nil || !strings.Contains(gzCmd, "gunzip -kf 'app.log.gz'") {
		t.Fatalf("unexpected gz cmd: %q, err=%v", gzCmd, err)
	}

	if _, err := buildExtractCommand("/tmp", "readme.txt"); err == nil {
		t.Fatal("expected unsupported extension to fail")
	}
}

func TestUpdateCWDFromCommand(t *testing.T) {
	m := NewManager(nil, nil)
	sess := &sessionState{
		currentDir: "/home/ubuntu",
	}
	sess.info.HomeDir = "/home/ubuntu"

	m.updateCWDFromCommand(sess, "cd /var/log/nginx")
	if sess.currentDir != "/var/log/nginx" {
		t.Fatalf("expected /var/log/nginx, got %q", sess.currentDir)
	}

	m.updateCWDFromCommand(sess, "cd ../apt && ls -la")
	if sess.currentDir != "/var/log/apt" {
		t.Fatalf("expected /var/log/apt, got %q", sess.currentDir)
	}

	m.updateCWDFromCommand(sess, "cd ~/projects")
	if sess.currentDir != "/home/ubuntu/projects" {
		t.Fatalf("expected /home/ubuntu/projects, got %q", sess.currentDir)
	}

	m.updateCWDFromCommand(sess, "cd")
	if sess.currentDir != "/home/ubuntu" {
		t.Fatalf("expected /home/ubuntu, got %q", sess.currentDir)
	}
}
