package sshclient

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func TestPickDominantMode(t *testing.T) {
	if pickDominantMode(map[os.FileMode]int{0o644: 3, 0o755: 1}, 0o644) != 0o644 ||
		pickDominantMode(map[os.FileMode]int{0o644: 2, 0o640: 2}, 0o644) != 0o644 ||
		pickDominantMode(nil, 0o755) != 0o755 {
		t.Fatal("pickDominantMode mismatch")
	}
}

func TestBuildExtractCommand(t *testing.T) {
	for _, tc := range [][3]string{
		{"/w", "a b.zip", "unzip -o 'a b.zip' -d ."},
		{"/d", "b.tar.gz", "tar -xzf 'b.tar.gz'"},
		{"/t", "p.7z", "7z x -y -- 'p.7z'"},
		{"/l", "a.gz", "gunzip -kf -- 'a.gz'"},
	} {
		if c, err := buildExtractCommand(tc[0], tc[1]); err != nil || !strings.Contains(c, tc[2]) {
			t.Fatalf("%s: %q %v", tc[1], c, err)
		}
	}
	if _, err := buildExtractCommand("/t", "r.txt"); err == nil {
		t.Fatal("expected error")
	}
	zipCmd, err := buildExtractCommand("/w", "pkg.zip")
	if err != nil || !strings.Contains(zipCmd, "sudo -n") || !strings.Contains(zipCmd, "DEBIAN_FRONTEND=noninteractive") {
		t.Fatalf("expected sudo -n and DEBIAN_FRONTEND=noninteractive in auto-install snippet: %q err=%v", zipCmd, err)
	}
}

func TestUpdateCWDFromCommand(t *testing.T) {
	m, s := NewManager(nil, nil), &sessionState{
		currentDir: "/home/u",
		dirCache:   map[string]dirCacheEntry{"/home/u": {fetchedAt: time.Now()}},
	}
	s.info.HomeDir = "/home/u"
	for _, tc := range [][2]string{{"cd /var/log/nginx", "/var/log/nginx"}, {"cd ../apt && ls", "/var/log/apt"}, {"cd ~/p", "/home/u/p"}, {"cd", "/home/u"}} {
		if m.updateCWDFromCommand(s, tc[0]); s.currentDir != tc[1] {
			t.Fatalf("%q: %q", tc[0], s.currentDir)
		}
	}
	// Read-only commands must NOT wipe dirCache.
	if len(s.dirCache) != 1 {
		t.Fatalf("expected read-only commands to preserve dirCache, got %d entries", len(s.dirCache))
	}
	// Mutating command MUST wipe dirCache and bump dirCacheGen.
	genBefore := s.dirCacheGen
	m.updateCWDFromCommand(s, "touch newfile.txt")
	if len(s.dirCache) != 0 || s.dirCacheGen <= genBefore {
		t.Fatalf("expected mutating command to clear dirCache and bump gen (len=%d gen=%d->%d)", len(s.dirCache), genBefore, s.dirCacheGen)
	}
	// Stale generation write must be rejected.
	putDirCacheIfGen(s, "/home/u", nil, genBefore)
	if len(s.dirCache) != 0 {
		t.Fatal("stale dirCacheGen write should have been ignored")
	}

	// Mutating command must invalidate only CWD while preserving unrelated dirs.
	if dirCacheTTL != 20*time.Second {
		t.Fatalf("expected dirCacheTTL=20s, got %v", dirCacheTTL)
	}
	s.dirCache["/home/u"] = dirCacheEntry{fetchedAt: time.Now()}
	s.dirCache["/etc"] = dirCacheEntry{fetchedAt: time.Now()}
	m.updateCWDFromCommand(s, "rm -f temp.txt")
	if _, ok := s.dirCache["/home/u"]; ok {
		t.Fatal("expected CWD /home/u to be invalidated")
	}
	if _, ok := s.dirCache["/etc"]; !ok {
		t.Fatal("expected unrelated /etc cache entry to be preserved")
	}
}

func TestMarkDisconnectedAndKeepAliveGuard(t *testing.T) {
	m := NewManager(nil, nil)
	stopCh := make(chan struct{})
	s := &sessionState{
		info:         models.SessionInfo{SessionID: "s1", Status: "connected", SFTPReady: true},
		stopCh:       stopCh,
		recorder:     NewCommandRecorder(nil),
		dirCache:     map[string]dirCacheEntry{"/root": {fetchedAt: time.Now()}},
		prevCPU:      &cpuSample{total: 10, idle: 5},
		prevNet:      &netSample{rx: 1, tx: 2},
		prevSampleAt: time.Now(),
	}
	m.sessions["s1"] = s

	var inFlight atomic.Bool
	inFlight.Store(true)
	if err := sendKeepAliveProbe(nil, stopCh, time.Second, &inFlight); !errors.Is(err, errProbeInFlight) {
		t.Fatalf("expected errProbeInFlight when probe already running, got %v", err)
	}

	m.markDisconnected(s, "done")
	if _, ok := m.sessions["s1"]; ok {
		t.Fatal("expected disconnected session to be removed from manager")
	}
	if s.dirCache != nil || s.recorder != nil || s.prevCPU != nil || s.prevNet != nil {
		t.Fatal("expected runtime state cleaned up on disconnect")
	}
	if err := sendKeepAliveProbe(nil, stopCh, time.Second, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on closed stopCh, got %v", err)
	}
}

type mNode struct {
	name, tgt string
	mode      os.FileMode
}

func (n *mNode) Name() string       { return n.name }
func (n *mNode) Size() int64        { return 0 }
func (n *mNode) Mode() os.FileMode  { return n.mode }
func (n *mNode) ModTime() time.Time { return time.Time{} }
func (n *mNode) IsDir() bool        { return n.mode.IsDir() }
func (n *mNode) Sys() any           { return nil }

type fiList []os.FileInfo

func (l fiList) ListAt(dst []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[off:])
	if off+int64(n) >= int64(len(l)) {
		return n, io.EOF
	}
	return n, nil
}

type mFS struct {
	mu       sync.Mutex
	m        map[string]*mNode
	spurious bool
}

func (f *mFS) Fileread(*sftp.Request) (io.ReaderAt, error)  { return nil, os.ErrPermission }
func (f *mFS) Filewrite(*sftp.Request) (io.WriterAt, error) { return nil, os.ErrPermission }
func (f *mFS) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.m[path.Clean(r.Filepath)]; ok {
		return fiList{n}, nil
	}
	return nil, os.ErrNotExist
}
func (f *mFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := path.Clean(r.Filepath)
	n, ok := f.m[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	if r.Method == "Stat" {
		if n.mode&os.ModeSymlink != 0 {
			if tn, ok := f.m[n.tgt]; ok {
				return fiList{tn}, nil
			}
			return nil, os.ErrNotExist
		}
		return fiList{n}, nil
	}
	var out fiList
	for k, v := range f.m {
		if k != p && path.Dir(k) == p {
			out = append(out, v)
		}
	}
	return out, nil
}
func (f *mFS) Filecmd(r *sftp.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := path.Clean(r.Filepath)
	n, ok := f.m[p]
	if !ok {
		return os.ErrNotExist
	}
	switch r.Method {
	case "Setstat":
		if r.AttrFlags().Permissions {
			n.mode = (n.mode &^ os.ModePerm) | r.Attributes().FileMode().Perm()
		}
	case "Remove":
		if n.IsDir() {
			return os.ErrInvalid
		}
		delete(f.m, p)
	case "Rmdir":
		for k := range f.m {
			if strings.HasPrefix(k, p+"/") {
				return errors.New("not empty")
			}
		}
		delete(f.m, p)
	case "Rename":
		return f.mv(p, path.Clean(r.Target), false)
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
	return nil
}
func (f *mFS) PosixRename(r *sftp.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mv(path.Clean(r.Filepath), path.Clean(r.Target), true)
}
func (f *mFS) mv(oldP, newP string, overwrite bool) error {
	n, ok := f.m[oldP]
	if !ok {
		return os.ErrNotExist
	}
	if _, exists := f.m[newP]; !overwrite && exists {
		return errors.New("exists")
	}
	delete(f.m, oldP)
	n.name, f.m[newP] = path.Base(newP), n
	for k, v := range f.m {
		if strings.HasPrefix(k, oldP+"/") {
			delete(f.m, k)
			f.m[newP+strings.TrimPrefix(k, oldP)] = v
		}
	}
	if f.spurious {
		f.spurious = false
		return os.ErrNotExist
	}
	return nil
}

type rwPipe struct {
	io.Reader
	io.WriteCloser
}

func TestSFTPDeleteAndRename(t *testing.T) {
	const d755, d555, lnk = os.ModeDir | 0o755, os.ModeDir | 0o555, os.ModeSymlink | 0o777
	fs := &mFS{m: map[string]*mNode{
		"/": {"/", "", d755}, "/r": {"r", "", d755},
		"/r/sdk": {"sdk", "", d755}, "/r/sdk/sub": {"sub", "", d555},
		"/r/sdk/sub/a": {"a", "", 0o644}, "/r/sdk/brk": {"brk", "/no", lnk},
		"/r/keep": {"keep", "", d755}, "/r/keep/f": {"f", "", 0o644},
		"/r/lnk": {"lnk", "/r/keep", lnk}, "/r/geo": {"geo", "", d755},
		"/r/geo/d": {"d", "", 0o644}, "/r/a": {"a", "", 0o644}, "/r/b": {"b", "", 0o644},
	}}
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := sftp.NewRequestServer(rwPipe{c2sR, s2cW}, sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs})
	go func() { _ = srv.Serve() }()
	cli, err := sftp.NewClientPipe(s2cR, c2sW)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c2sW.Close(); _ = srv.Close(); _ = s2cW.Close(); _ = cli.Close() })
	m := NewManager(nil, nil)
	m.sessions["s1"] = &sessionState{sftpClient: cli, dirCache: map[string]dirCacheEntry{}}
	has := func(p string) bool { _, ok := fs.m[p]; return ok }

	if m.SFTPDelete("s1", "/") == nil || !errors.Is(m.SFTPDelete("s1", "/r/miss"), os.ErrNotExist) {
		t.Fatal("root/missing delete check failed")
	}
	// nested dir with read-only subdir + broken symlink (the metatube-sdk case)
	if err := m.SFTPDelete("s1", "/r/sdk"); err != nil || has("/r/sdk") || has("/r/sdk/brk") {
		t.Fatalf("nested delete: %v", err)
	}
	if err := m.SFTPDelete("s1", "/r/lnk"); err != nil || has("/r/lnk") || !has("/r/keep/f") {
		t.Fatalf("symlink delete: %v", err)
	}
	for _, tc := range []struct {
		old, dst, chk string
		sp            bool
	}{
		{"/r/geo", "/r/geo2", "/r/geo2/d", false},
		{"/r/a", "/r/b", "/r/b", false}, // overwrite
		{"/r/b", "/r/c", "/r/c", true},  // server moved but replied ENOENT
	} {
		fs.spurious = tc.sp
		if err := m.SFTPRename("s1", tc.old, tc.dst); err != nil || has(tc.old) || !has(tc.chk) {
			t.Fatalf("rename %s->%s: %v", tc.old, tc.dst, err)
		}
	}
	if err := m.SFTPRename("s1", "/r/miss", "/r/c"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing rename: %v", err)
	}
}

func TestSFTPAlignPermissionsRejectsSymlinkAndFiltersNonRegularSiblings(t *testing.T) {
	const (
		d755 = os.ModeDir | 0o755
		lnk  = os.ModeSymlink | 0o777
		sock = os.ModeSocket | 0o666
	)
	fs := &mFS{m: map[string]*mNode{
		"/":          {"/", "", d755},
		"/d":         {"d", "", d755},
		"/d/secret":  {"secret", "", 0o600},
		"/d/link":    {"link", "/d/secret", lnk},
		"/d/target":  {"target", "", 0o777},
		"/d/reg1":    {"reg1", "", 0o640},
		"/d/sock1":   {"sock1", "", sock},
		"/d/sock2":   {"sock2", "", sock},
		"/d/sock3":   {"sock3", "", sock},
		"/d/otherln": {"otherln", "/d/secret", lnk},
	}}
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := sftp.NewRequestServer(rwPipe{c2sR, s2cW}, sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs})
	go func() { _ = srv.Serve() }()
	cli, err := sftp.NewClientPipe(s2cR, c2sW)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c2sW.Close(); _ = srv.Close(); _ = s2cW.Close(); _ = cli.Close() })

	m := NewManager(nil, nil)
	m.sessions["s1"] = &sessionState{sftpClient: cli, dirCache: map[string]dirCacheEntry{}}

	// 1. Symlink target must be rejected and its pointed-to 0600 file untouched.
	if _, err := m.SFTPAlignPermissions("s1", "/d/link"); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("expected symlink rejection error, got: %v", err)
	}
	if got := fs.m["/d/secret"].mode.Perm(); got != 0o600 {
		t.Fatalf("expected symlink target /d/secret mode untouched (0600), got %04o", got)
	}

	// 2. Non-regular special file (socket) must be rejected.
	if _, err := m.SFTPAlignPermissions("s1", "/d/sock1"); err == nil {
		t.Fatal("expected socket target to be rejected by SFTPAlignPermissions")
	}

	// 3. Regular file sibling voting must ignore sockets (0666) and symlinks (0777).
	// Siblings are: reg1 (0640) vs 3 sockets (0666) and 2 symlinks (0777) and secret (0600).
	// Between 0600 (1) and 0640 (1), tie-breaker picks smaller mode 0600 (NOT 0666 or 0777!).
	fs.m["/d/reg2"] = &mNode{name: "reg2", mode: 0o640}
	modeStr, err := m.SFTPAlignPermissions("s1", "/d/target")
	if err != nil {
		t.Fatalf("SFTPAlignPermissions on regular file failed: %v", err)
	}
	if modeStr != "0640" || fs.m["/d/target"].mode.Perm() != 0o640 {
		t.Fatalf("expected target aligned to regular siblings 0640 (ignoring 3 sockets at 0666), got %s (%04o)", modeStr, fs.m["/d/target"].mode.Perm())
	}
}
