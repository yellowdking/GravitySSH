package sshclient

import (
	"io"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func newPipeSFTPClient(t *testing.T, fs *mFS) (*sftp.Client, func()) {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := sftp.NewRequestServer(rwPipe{c2sR, s2cW}, sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs})
	go func() { _ = srv.Serve() }()
	cli, err := sftp.NewClientPipe(s2cR, c2sW)
	if err != nil {
		t.Fatal(err)
	}
	closeFn := func() { _ = c2sW.Close(); _ = srv.Close(); _ = s2cW.Close(); _ = cli.Close() }
	t.Cleanup(closeFn)
	return cli, closeFn
}

func TestSFTPRecoveryFromStaleClient(t *testing.T) {
	const d755 = os.ModeDir | 0o755
	fs := &mFS{m: map[string]*mNode{
		"/": {"/", "", d755}, "/r": {"r", "", d755},
		"/r/f1.txt": {"f1.txt", "", 0o644},
		"/r/dir":    {"dir", "", d755}, "/r/dir/a.txt": {"a.txt", "", 0o644},
		"/r/old": {"old", "", 0o644},
	}}
	deadCli, closeDead := newPipeSFTPClient(t, fs)
	closeDead()

	var dials atomic.Int32
	var lastClose func()
	m := NewManager(nil, nil)
	m.sftpDial = func(string) (*sftp.Client, error) {
		dials.Add(1)
		c, cl := newPipeSFTPClient(t, fs)
		lastClose = cl
		return c, nil
	}
	m.sessions["s1"] = &sessionState{
		sftpClient: deadCli,
		dirCache:   map[string]dirCacheEntry{"/r": {}, "/r/dir": {}},
	}

	if err := m.SFTPDelete("s1", "/r/f1.txt"); err != nil {
		t.Fatalf("delete file after stale client: %v", err)
	}
	if _, ok := fs.m["/r/f1.txt"]; ok || dials.Load() != 1 {
		t.Fatalf("f1.txt delete or redial failed (dials=%d)", dials.Load())
	}

	lastClose()
	if err := m.SFTPDelete("s1", "/r/dir"); err != nil {
		t.Fatalf("delete dir after stale client: %v", err)
	}
	if _, ok := fs.m["/r/dir"]; ok || len(m.sessions["s1"].dirCache) != 0 {
		t.Fatal("dir or dirCache not cleaned")
	}

	lastClose()
	if err := m.SFTPRename("s1", "/r/old", "/r/new"); err != nil {
		t.Fatalf("rename after stale client: %v", err)
	}
	lastClose()
	res, err := m.SFTPList("s1", "/r")
	if err != nil || len(res.Entries) != 1 || res.Entries[0].Name != "new" {
		t.Fatalf("list after stale client: %+v %v", res, err)
	}
}

type probeFS struct {
	*mFS
	lstats    atomic.Int32
	dropAfter func()
	mu        sync.Mutex
	dropped   bool
}

func (p *probeFS) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	p.lstats.Add(1)
	return p.mFS.Lstat(r)
}

func (p *probeFS) Filecmd(r *sftp.Request) error {
	err := p.mFS.Filecmd(r)
	if p.dropAfter != nil && r.Method == "Remove" {
		p.mu.Lock()
		if !p.dropped {
			p.dropped = true
			p.mu.Unlock()
			p.dropAfter()
			return io.EOF
		}
		p.mu.Unlock()
	}
	return err
}

func TestLowRTTDirDeleteAndPostDropVerify(t *testing.T) {
	const d755 = os.ModeDir | 0o755
	nodes := map[string]*mNode{"/": {"/", "", d755}, "/d": {"d", "", d755}}
	for i := 0; i < 24; i++ {
		n := "f" + string(rune('a'+i))
		nodes["/d/"+n] = &mNode{n, "", 0o644}
	}
	pfs := &probeFS{mFS: &mFS{m: nodes}}
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := sftp.NewRequestServer(rwPipe{c2sR, s2cW}, sftp.Handlers{FileGet: pfs, FilePut: pfs, FileCmd: pfs, FileList: pfs})
	go func() { _ = srv.Serve() }()
	cli, err := sftp.NewClientPipe(s2cR, c2sW)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c2sW.Close(); _ = srv.Close(); _ = s2cW.Close(); _ = cli.Close() })

	m := NewManager(nil, nil)
	m.sessions["s1"] = &sessionState{sftpClient: cli, dirCache: map[string]dirCacheEntry{}}
	if err := m.SFTPDelete("s1", "/d"); err != nil {
		t.Fatalf("dir delete failed: %v", err)
	}
	if _, ok := pfs.m["/d"]; ok {
		t.Fatal("/d still present")
	}
	// Only 1 top-level Lstat (zero redundant per-child Lstats for 24 files)
	if got := pfs.lstats.Load(); got != 1 {
		t.Fatalf("expected 1 Lstat call for 24-file dir, got %d", got)
	}

	// Simulate file removed on server right before SFTP channel drops
	fs2 := &mFS{m: map[string]*mNode{"/": {"/", "", d755}, "/x": {"x", "", 0o644}}}
	var closeFirst func()
	pfs2 := &probeFS{mFS: fs2, dropAfter: func() { closeFirst() }}
	c2sR2, c2sW2 := io.Pipe()
	s2cR2, s2cW2 := io.Pipe()
	srv2 := sftp.NewRequestServer(rwPipe{c2sR2, s2cW2}, sftp.Handlers{FileGet: pfs2, FilePut: pfs2, FileCmd: pfs2, FileList: pfs2})
	go func() { _ = srv2.Serve() }()
	cli2, err := sftp.NewClientPipe(s2cR2, c2sW2)
	if err != nil {
		t.Fatal(err)
	}
	closeFirst = func() { _ = c2sW2.Close(); _ = s2cW2.Close(); _ = srv2.Close() }
	t.Cleanup(func() { closeFirst(); _ = cli2.Close() })

	m2 := NewManager(nil, nil)
	m2.sftpDial = func(string) (*sftp.Client, error) { c, _ := newPipeSFTPClient(t, fs2); return c, nil }
	m2.sessions["s2"] = &sessionState{sftpClient: cli2, dirCache: map[string]dirCacheEntry{}}
	if err := m2.SFTPDelete("s2", "/x"); err != nil {
		t.Fatalf("expected nil after post-drop verify, got %v", err)
	}
	if !isSFTPConnError(io.EOF) || isSFTPConnError(os.ErrNotExist) || path.Clean("/a") != "/a" {
		t.Fatal("conn error classifier mismatch")
	}
	_ = time.Millisecond
}
