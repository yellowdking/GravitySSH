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

	// Windows drive root & backslash traversal checks
	if got, err := safeJoinLocal(`C:\`, "proj/app.conf"); err != nil || got != `C:\proj\app.conf` {
		t.Fatalf("safeJoinLocal on Windows drive root failed: got=%q err=%v", got, err)
	}
	for _, bad := range []string{`..\..\evil.txt`, `D:evil.txt`, "bad\x00name"} {
		if _, err := safeJoinLocal(dest, bad); err == nil {
			t.Fatalf("expected unsafe path %q to be rejected", bad)
		}
	}
}

func TestBuildCompressCommand(t *testing.T) {
	name, cmd, err := buildCompressCommand("/var/www", []string{"site"}, "tar.gz")
	if err != nil || name != "site.tar.gz" || !strings.Contains(cmd, "cd -- '/var/www' && tar -czf 'site.tar.gz' -- 'site'") {
		t.Fatalf("unexpected tar.gz compress cmd: %q %q err=%v", name, cmd, err)
	}
	zName, zCmd, err := buildCompressCommand("/tmp", []string{"my folder"}, "zip")
	if err != nil || zName != "my folder.zip" || !strings.Contains(zCmd, "command -v zip") || !strings.Contains(zCmd, "zip -rq -- 'my folder.zip' 'my folder'") {
		t.Fatalf("unexpected zip compress cmd: %q %q err=%v", zName, zCmd, err)
	}
}

func TestOptionInjectionGuardForDashPrefixedNames(t *testing.T) {
	// 1. SFTPDownloadPacked (tar -cf -)
	dlCmd := buildPackedDownloadCommand("/tmp", "--checkpoint-action=exec=id")
	if dlCmd != "tar -cf - -C '/tmp' -- './--checkpoint-action=exec=id'" {
		t.Fatalf("unsafe packed download cmd: %q", dlCmd)
	}

	// 2. SFTPCompress tar.gz with dash-prefixed single and multiple names
	tName, tCmd, err := buildCompressCommand("/tmp", []string{"--checkpoint=1", "--checkpoint-action=exec=id"}, "tar.gz")
	if err != nil || !strings.HasSuffix(tName, ".tar.gz") {
		t.Fatalf("unexpected tar.gz compress result: %q err=%v", tName, err)
	}
	if !strings.Contains(tCmd, "cd -- '/tmp' && tar -czf ") || !strings.Contains(tCmd, " -- './--checkpoint=1' './--checkpoint-action=exec=id'") {
		t.Fatalf("unsafe tar.gz compress cmd: %q", tCmd)
	}

	singleName, singleCmd, err := buildCompressCommand("/tmp", []string{"-evil"}, "tar.gz")
	if err != nil || singleName != "-evil.tar.gz" {
		t.Fatalf("unexpected single dash tar.gz name: %q err=%v", singleName, err)
	}
	if !strings.Contains(singleCmd, "tar -czf './-evil.tar.gz' -- './-evil'") {
		t.Fatalf("unsafe single dash tar.gz cmd: %q", singleCmd)
	}

	// 3. SFTPCompress zip with dash-prefixed name (-TT option injection)
	zName, zCmd, err := buildCompressCommand("/tmp", []string{"-TT"}, "zip")
	if err != nil || zName != "-TT.zip" {
		t.Fatalf("unexpected zip dash name: %q err=%v", zName, err)
	}
	if !strings.Contains(zCmd, "cd -- '/tmp'") || !strings.Contains(zCmd, "zip -rq -- './-TT.zip' './-TT'") {
		t.Fatalf("unsafe zip compress cmd: %q", zCmd)
	}

	// 4. SFTPExtractArchive with dash-prefixed archive names and dir
	extractCases := []struct {
		dir  string
		base string
		want string
	}{
		{"/tmp", "-evil.zip", "unzip -o './-evil.zip' -d ."},
		{"/tmp", "-evil.gz", "gunzip -kf -- './-evil.gz'"},
		{"/tmp", "-evil.tar.gz", "tar -xzf './-evil.tar.gz'"},
		{"/tmp", "-evil.tar.bz2", "tar -xjf './-evil.tar.bz2'"},
		{"/tmp", "-evil.tar.xz", "tar -xJf './-evil.tar.xz'"},
		{"-dir", "-evil.tar", "cd -- '-dir' && tar -xf './-evil.tar'"},
		{"/tmp", "-evil.7z", "7z x -y -- './-evil.7z'"},
	}
	for _, tc := range extractCases {
		got, err := buildExtractCommand(tc.dir, tc.base)
		if err != nil {
			t.Fatalf("buildExtractCommand(%q, %q) failed: %v", tc.dir, tc.base, err)
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("buildExtractCommand(%q, %q) = %q, want substring %q", tc.dir, tc.base, got, tc.want)
		}
		if strings.HasSuffix(tc.base, ".zip") && strings.Contains(got, "unzip -o --") {
			t.Fatalf("unzip 6.0 does not support --; got %q", got)
		}
	}
}
