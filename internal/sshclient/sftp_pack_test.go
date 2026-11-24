package sshclient

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnpackTarStreamAndPathTraversalGuard(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("hello gravityssh")
	_ = tw.WriteHeader(&tar.Header{Name: "proj/", Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: "proj/app.conf", Typeflag: tar.TypeReg, Size: int64(len(content)), Mode: 0o644})
	_, _ = tw.Write(content)
	_ = tw.Close()

	dest := t.TempDir()
	n, err := unpackTarStream(&buf, dest)
	if err != nil || n != 1 {
		t.Fatalf("unpackTarStream failed: n=%d err=%v", n, err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "proj", "app.conf"))
	if err != nil || string(got) != "hello gravityssh" {
		t.Fatalf("unpacked content mismatch: %q err=%v", got, err)
	}

	// Reject path traversal
	var evil bytes.Buffer
	ew := tar.NewWriter(&evil)
	_ = ew.WriteHeader(&tar.Header{Name: "../../evil.txt", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644})
	_, _ = ew.Write([]byte("x"))
	_ = ew.Close()
	if _, err := unpackTarStream(&evil, dest); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestBuildCompressCommand(t *testing.T) {
	name, cmd, err := buildCompressCommand("/var/www", []string{"site"}, "tar.gz")
	if err != nil || name != "site.tar.gz" || !strings.Contains(cmd, "tar -czf 'site.tar.gz' 'site'") {
		t.Fatalf("unexpected tar.gz compress cmd: %q %q err=%v", name, cmd, err)
	}
	zName, zCmd, err := buildCompressCommand("/tmp", []string{"my folder"}, "zip")
	if err != nil || zName != "my folder.zip" || !strings.Contains(zCmd, "command -v zip") || !strings.Contains(zCmd, "zip -rq 'my folder.zip' 'my folder'") {
		t.Fatalf("unexpected zip compress cmd: %q %q err=%v", zName, zCmd, err)
	}
}
