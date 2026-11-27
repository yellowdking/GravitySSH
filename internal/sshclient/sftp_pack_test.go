package sshclient

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

	// Drive/root, backslash traversal, NTFS ADS & invalid Windows filename char checks
	if runtime.GOOS == "windows" {
		if got, err := safeJoinLocal(`C:\`, "proj/app.conf"); err != nil || got != `C:\proj\app.conf` {
			t.Fatalf("safeJoinLocal on Windows drive root failed: got=%q err=%v", got, err)
		}
	} else {
		if got, err := safeJoinLocal("/", "proj/app.conf"); err != nil || got != "/proj/app.conf" {
			t.Fatalf("safeJoinLocal on POSIX root failed: got=%q err=%v", got, err)
		}
	}
	for _, bad := range []string{
		`..\..\evil.txt`,
		`D:evil.txt`,
		"bad\x00name",
		"dir/sha256:abc",
		"file.txt:Zone.Identifier",
		"dir/a*b.txt",
		"dir/a?b.txt",
		`dir/a"b.txt`,
		"dir/a<b.txt",
		"dir/a>b.txt",
		"dir/a|b.txt",
		"dir/ctrl\x07.txt",
	} {
		if _, err := safeJoinLocal(dest, bad); err == nil {
			t.Fatalf("expected unsafe path %q to be rejected", bad)
		}
	}

	// Partial file cleanup on truncated tar copy error
	var trunc bytes.Buffer
	twTrunc := tar.NewWriter(&trunc)
	_ = twTrunc.WriteHeader(&tar.Header{Name: "partial/big.bin", Typeflag: tar.TypeReg, Size: 1024, Mode: 0o644})
	_, _ = twTrunc.Write([]byte("only-16-bytes---"))
	// Intentionally do not finish writing 1024 bytes or close tar writer -> io.ErrUnexpectedEOF mid-copy
	if _, err := unpackTarStream(&trunc, dest); err == nil {
		t.Fatal("expected truncated tar stream to fail")
	}
	if _, statErr := os.Stat(filepath.Join(dest, "partial", "big.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("expected partial unpacked file to be removed on copy error, statErr=%v", statErr)
	}
}

func TestBuildCompressCommand(t *testing.T) {
	name, cmd, err := buildCompressCommand("/var/www", []string{"site"}, "tar.gz")
	if err != nil || name != "site.tar.gz" || !strings.Contains(cmd, "cd -- '/var/www' && tar -czf 'site.tar.gz' -- 'site'") {
		t.Fatalf("unexpected tar.gz compress cmd: %q %q err=%v", name, cmd, err)
	}
	zName, zCmd, err := buildCompressCommand("/tmp", []string{"my folder"}, "zip")
	if err != nil || zName != "my folder.zip" || !strings.Contains(zCmd, "command -v zip") || !strings.Contains(zCmd, "; fi && zip -rq -- 'my folder.zip' 'my folder'") {
		t.Fatalf("unexpected zip compress cmd: %q %q err=%v", zName, zCmd, err)
	}
}

func TestOptionInjectionGuardForDashPrefixedNames(t *testing.T) {
	// 1. SFTPDownloadPacked (tar -czf - with symlinked-dir dereference base/.)
	dlCmd := buildPackedDownloadCommand("/tmp", "--checkpoint-action=exec=id")
	for _, want := range []string{
		`sh -c 'cd -- "$1" && if [ -d "$2" ]; then exec tar -czf - -- "$2/."; else exec tar -czf - -- "$2"; fi' _`,
		`'/tmp'`,
		`'./--checkpoint-action=exec=id'`,
	} {
		if !strings.Contains(dlCmd, want) {
			t.Fatalf("unsafe packed download cmd: %q (missing %q)", dlCmd, want)
		}
	}
	// Colon-containing target also receives ./ prefix
	colonDlCmd := buildPackedDownloadCommand("/srv", "snap:12:30")
	if !strings.Contains(colonDlCmd, "'./snap:12:30'") {
		t.Fatalf("expected ./ prefix on colon packed download target, got: %q", colonDlCmd)
	}

	// 2. SFTPCompress tar.gz with dash-prefixed and colon-containing names
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

	colonName, colonCmd, err := buildCompressCommand("/tmp", []string{"backup-12:30"}, "tar.gz")
	if err != nil || colonName != "backup-12:30.tar.gz" {
		t.Fatalf("unexpected colon tar.gz name: %q err=%v", colonName, err)
	}
	if !strings.Contains(colonCmd, "tar -czf './backup-12:30.tar.gz' -- './backup-12:30'") {
		t.Fatalf("expected ./ prefix for colon tar archive name, got: %q", colonCmd)
	}

	// 3. SFTPCompress zip with dash-prefixed name (-TT option injection)
	zName, zCmd, err := buildCompressCommand("/tmp", []string{"-TT"}, "zip")
	if err != nil || zName != "-TT.zip" {
		t.Fatalf("unexpected zip dash name: %q err=%v", zName, err)
	}
	if !strings.Contains(zCmd, "cd -- '/tmp'") || !strings.Contains(zCmd, "; fi && zip -rq -- './-TT.zip' './-TT'") {
		t.Fatalf("unsafe zip compress cmd: %q", zCmd)
	}

	// 4. SFTPExtractArchive with dash-prefixed and colon-containing archive names and dir
	extractCases := []struct {
		dir  string
		base string
		want string
	}{
		{"/tmp", "-evil.zip", "; fi && unzip -oq './-evil.zip' -d ."},
		{"/tmp", "-evil.gz", "gunzip -kf -- './-evil.gz'"},
		{"/tmp", "-evil.tar.gz", "tar -xzf './-evil.tar.gz'"},
		{"/tmp", "backup-12:30.tar.gz", "tar -xzf './backup-12:30.tar.gz'"},
		{"/tmp", "host:file.tar", "tar -xf './host:file.tar'"},
		{"/tmp", "-evil.tar.bz2", "tar -xjf './-evil.tar.bz2'"},
		{"/tmp", "-evil.tar.xz", "tar -xJf './-evil.tar.xz'"},
		{"-dir", "-evil.tar", "cd -- '-dir' && tar -xf './-evil.tar'"},
		{"/tmp", "-evil.7z", "; fi && 7z x -y -bso0 -- './-evil.7z'"},
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

func TestSFTPCompressEmptyDirAndExtractTreeCacheInvalidation(t *testing.T) {
	var (
		cmdMu  sync.Mutex
		ranCmd string
	)
	cli := startMockSSHSFTPServer(t, &mFS{m: map[string]*mNode{}}, func(cmd string) (string, uint32) {
		cmdMu.Lock()
		ranCmd = cmd
		cmdMu.Unlock()
		return "", 0
	})
	m := NewManager(nil, nil)
	s := &sessionState{
		dialRes: &DialResult{Client: cli},
		stopCh:  make(chan struct{}),
		dirCache: map[string]dirCacheEntry{
			"/":           {entries: nil},
			"/work":       {entries: nil},
			"/work/sub":   {entries: nil},
			"/work/sub/a": {entries: nil},
			"/other":      {entries: nil},
		},
	}
	m.sessions["s1"] = s

	if _, err := m.SFTPCompress("s1", "", []string{"foo"}, "tar.gz"); err != nil {
		t.Fatalf("SFTPCompress with empty dir failed: %v", err)
	}
	cmdMu.Lock()
	gotCmd := ranCmd
	cmdMu.Unlock()
	if !strings.Contains(gotCmd, "cd -- '/' &&") {
		t.Fatalf("expected empty remoteDir to resolve to '/', got cmd: %q", gotCmd)
	}
	s.mu.Lock()
	_, rootCached := s.dirCache["/"]
	s.mu.Unlock()
	if rootCached {
		t.Fatal("expected '/' cache entry invalidated after SFTPCompress with empty remoteDir")
	}

	if _, err := m.SFTPExtractArchive("s1", "/work/pkg.tar.gz"); err != nil {
		t.Fatalf("SFTPExtractArchive failed: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stale := range []string{"/work", "/work/sub", "/work/sub/a"} {
		if _, ok := s.dirCache[stale]; ok {
			t.Fatalf("expected tree cache entry %q invalidated after extract", stale)
		}
	}
	if _, ok := s.dirCache["/other"]; !ok {
		t.Fatal("expected unrelated /other cache entry preserved")
	}
}
