package sshclient

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

type memRWNode struct {
	name   string
	mode   os.FileMode
	data   []byte
	target string // non-empty when symlink
}

func (n *memRWNode) Name() string       { return n.name }
func (n *memRWNode) Size() int64        { return int64(len(n.data)) }
func (n *memRWNode) Mode() os.FileMode  { return n.mode }
func (n *memRWNode) ModTime() time.Time { return time.Time{} }
func (n *memRWNode) IsDir() bool        { return n.mode.IsDir() }
func (n *memRWNode) Sys() any           { return nil }

type memRWFS struct {
	mu               sync.Mutex
	nodes            map[string]*memRWNode
	reportZeroSz     bool
	failRename       bool
	failMidRead      bool
	failMidWrite     bool
	resolveRealPath  bool
}

type memWriterAt struct {
	fs   *memRWFS
	path string
}

func (w *memWriterAt) WriteAt(p []byte, off int64) (int, error) {
	w.fs.mu.Lock()
	defer w.fs.mu.Unlock()
	if w.fs.failMidWrite && off+int64(len(p)) > 16 {
		return 0, errors.New("simulated disk full write error")
	}
	targetPath := w.path
	if n, ok := w.fs.nodes[targetPath]; ok && n.mode&os.ModeSymlink != 0 && n.target != "" {
		targetPath = n.target
	}
	n, ok := w.fs.nodes[targetPath]
	if !ok {
		n = &memRWNode{name: path.Base(targetPath), mode: 0o644}
		w.fs.nodes[targetPath] = n
	}
	end := int(off) + len(p)
	if end > len(n.data) {
		grown := make([]byte, end)
		copy(grown, n.data)
		n.data = grown
	}
	copy(n.data[off:], p)
	return len(p), nil
}

func (f *memRWFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[path.Clean(r.Filepath)]
	if !ok {
		return nil, os.ErrNotExist
	}
	if n.mode&os.ModeSymlink != 0 && n.target != "" {
		n, ok = f.nodes[n.target]
		if !ok {
			return nil, os.ErrNotExist
		}
	}
	cp := append([]byte(nil), n.data...)
	if f.failMidRead {
		return &failingReaderAt{data: cp}, nil
	}
	return bytes.NewReader(cp), nil
}

func (f *memRWFS) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := path.Clean(r.Filepath)
	if existing, ok := f.nodes[p]; ok && existing.mode&os.ModeSymlink != 0 && existing.target != "" {
		// Opening a symlink for writing follows the symlink on POSIX.
		tgt := existing.target
		prevMode := os.FileMode(0o644)
		if tn, tok := f.nodes[tgt]; tok {
			prevMode = tn.mode
		}
		f.nodes[tgt] = &memRWNode{name: path.Base(tgt), mode: prevMode}
		return &memWriterAt{fs: f, path: tgt}, nil
	}
	f.nodes[p] = &memRWNode{name: path.Base(p), mode: 0o644}
	return &memWriterAt{fs: f, path: p}, nil
}

func (f *memRWFS) RealPath(p string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clean := path.Clean(p)
	if f.resolveRealPath {
		if n, ok := f.nodes[clean]; ok && n.mode&os.ModeSymlink != 0 && n.target != "" {
			return n.target, nil
		}
	}
	return clean, nil
}

func (f *memRWFS) Filecmd(r *sftp.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := path.Clean(r.Filepath)
	switch r.Method {
	case "Setstat":
		if n, ok := f.nodes[p]; ok && r.AttrFlags().Permissions {
			n.mode = (n.mode &^ os.ModePerm) | r.Attributes().FileMode().Perm()
		}
		return nil
	case "Remove":
		delete(f.nodes, p)
		return nil
	case "Rename":
		return f.renameLocked(p, path.Clean(r.Target))
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
}

func (f *memRWFS) PosixRename(r *sftp.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renameLocked(path.Clean(r.Filepath), path.Clean(r.Target))
}

func (f *memRWFS) renameLocked(oldP, newP string) error {
	if f.failRename {
		return os.ErrPermission
	}
	n, ok := f.nodes[oldP]
	if !ok {
		return os.ErrNotExist
	}
	delete(f.nodes, oldP)
	n.name = path.Base(newP)
	f.nodes[newP] = n
	return nil
}

type zeroSizeFileInfo struct{ os.FileInfo }

func (z zeroSizeFileInfo) Size() int64 { return 0 }

func (f *memRWFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := path.Clean(r.Filepath)
	n, ok := f.nodes[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	if r.Method == "Stat" {
		if n.mode&os.ModeSymlink != 0 && n.target != "" {
			n, ok = f.nodes[n.target]
			if !ok {
				return nil, os.ErrNotExist
			}
		}
		var fi os.FileInfo = n
		if f.reportZeroSz && !n.IsDir() {
			fi = zeroSizeFileInfo{n}
		}
		return fiList{fi}, nil
	}
	return fiList{}, nil
}

func (f *memRWFS) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[path.Clean(r.Filepath)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return fiList{n}, nil
}

func newPipeConcurrentSFTPClient(t *testing.T, fs *memRWFS) *sftp.Client {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	srv := sftp.NewRequestServer(rwPipe{c2sR, s2cW}, sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs})
	go func() { _ = srv.Serve() }()
	cli, err := sftp.NewClientPipe(
		s2cR, c2sW,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(sftpMaxConcurrentRequestsPerFile),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c2sW.Close(); _ = srv.Close(); _ = s2cW.Close(); _ = cli.Close() })
	return cli
}

type captureSink struct {
	mu     sync.Mutex
	events []models.TransferProgress
}

func (c *captureSink) Emit(eventName string, data ...any) {
	if eventName == "sftp:progress" && len(data) == 1 {
		if p, ok := data[0].(models.TransferProgress); ok {
			c.mu.Lock()
			c.events = append(c.events, p)
			c.mu.Unlock()
		}
	}
}

func TestSFTPPipelinedUploadDownloadAndEditorLimits(t *testing.T) {
	fs := &memRWFS{nodes: map[string]*memRWNode{
		"/":    {name: "/", mode: os.ModeDir | 0o755},
		"/dir": {name: "dir", mode: os.ModeDir | 0o755},
	}}
	cli := newPipeConcurrentSFTPClient(t, fs)
	sink := &captureSink{}
	m := NewManager(nil, sink)
	m.sessions["s1"] = &sessionState{
		sftpClient: cli,
		stopCh:     make(chan struct{}),
		dirCache:   map[string]dirCacheEntry{},
	}

	tmpDir := t.TempDir()
	payload := bytes.Repeat([]byte("GravitySSH-pipelined-chunk-0123456789\n"), 8000) // ~304 KB
	srcLocal := filepath.Join(tmpDir, "big.bin")
	if err := os.WriteFile(srcLocal, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.SFTPUpload("s1", srcLocal, "/dir"); err != nil {
		t.Fatalf("SFTPUpload failed: %v", err)
	}
	fs.mu.Lock()
	uploadedBig := append([]byte(nil), fs.nodes["/dir/big.bin"].data...)
	fs.mu.Unlock()
	if !bytes.Equal(uploadedBig, payload) {
		t.Fatalf("uploaded content mismatch: got %d bytes, want %d", len(uploadedBig), len(payload))
	}

	// Verify progressReader implements Size() int64 so pkg/sftp (*File).ReadFrom enables concurrent writes.
	var pr any = &progressReader{r: bytes.NewReader(payload), size: int64(len(payload))}
	sizer, ok := pr.(interface{ Size() int64 })
	if !ok || sizer.Size() != int64(len(payload)) {
		t.Fatalf("progressReader must implement Size() int64 returning %d, ok=%v", len(payload), ok)
	}

	dstLocal := filepath.Join(tmpDir, "downloaded.bin")
	if err := m.SFTPDownload("s1", "/dir/big.bin", dstLocal); err != nil {
		t.Fatalf("SFTPDownload failed: %v", err)
	}
	gotDown, err := os.ReadFile(dstLocal)
	if err != nil || !bytes.Equal(gotDown, payload) {
		t.Fatalf("downloaded content mismatch: err=%v len=%d", err, len(gotDown))
	}

	// Editor: atomic temp+rename write and fallback-in-place write.
	if err := m.SFTPWriteTextFile("s1", "/dir/conf.ini", "k=v1\n"); err != nil {
		t.Fatalf("SFTPWriteTextFile temp+rename failed: %v", err)
	}
	fs.mu.Lock()
	confV1 := string(fs.nodes["/dir/conf.ini"].data)
	fs.failRename = true
	fs.mu.Unlock()
	if confV1 != "k=v1\n" {
		t.Fatalf("unexpected conf.ini data: %q", confV1)
	}
	if err := m.SFTPWriteTextFile("s1", "/dir/conf.ini", "k=v2\n"); err != nil {
		t.Fatalf("SFTPWriteTextFile fallback in-place failed: %v", err)
	}
	fs.mu.Lock()
	confV2 := string(fs.nodes["/dir/conf.ini"].data)
	fs.failRename = false
	fs.nodes["/dir/virtual.log"] = &memRWNode{
		name: "virtual.log",
		mode: 0o644,
		data: bytes.Repeat([]byte("x"), maxEditableFileSize+10),
	}
	fs.reportZeroSz = true
	fs.mu.Unlock()
	if confV2 != "k=v2\n" {
		t.Fatalf("unexpected fallback conf.ini data: %q", confV2)
	}

	// Editor: reject len(raw) > maxEditableFileSize even when Stat reports size 0.
	if _, err := m.SFTPReadTextFile("s1", "/dir/virtual.log"); err == nil || !strings.Contains(err.Error(), "1 MB") {
		t.Fatalf("expected >1MB read rejection when Stat reports 0, got %v", err)
	}
}

type failingReaderAt struct {
	data []byte
}

func (f *failingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off == 0 && len(p) > 0 {
		n := copy(p, f.data[:min(len(p), 16)])
		return n, nil
	}
	return 0, errors.New("simulated read error")
}

func TestSFTPDownloadCleanupOnErrorAndOpenFileEvent(t *testing.T) {
	fs := &memRWFS{
		nodes: map[string]*memRWNode{
			"/":          {name: "/", mode: os.ModeDir | 0o755},
			"/large.bin": {name: "large.bin", mode: 0o644, data: bytes.Repeat([]byte("A"), 128*1024)},
		},
		failMidRead: true,
	}
	cli := newPipeConcurrentSFTPClient(t, fs)

	sink := &captureSink{}
	m := NewManager(nil, sink)
	m.sessions["s1"] = &sessionState{sftpClient: cli, stopCh: make(chan struct{}), dirCache: map[string]dirCacheEntry{}}

	tmpDir := t.TempDir()
	// 1. OpenFile failure (target is an existing directory) must emit an error event.
	existingDir := filepath.Join(tmpDir, "already-a-dir")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.SFTPDownload("s1", "/large.bin", existingDir); err == nil {
		t.Fatal("expected error when downloading to directory path")
	}
	sink.mu.Lock()
	gotOpenErrEvent := len(sink.events) > 0 && sink.events[len(sink.events)-1].Status == "error"
	sink.mu.Unlock()
	if !gotOpenErrEvent {
		t.Fatal("expected sftp:progress error event on os.OpenFile failure")
	}

	// 2. Mid-transfer read failure must remove the truncated local file.
	partialPath := filepath.Join(tmpDir, "partial.bin")
	if err := m.SFTPDownload("s1", "/large.bin", partialPath); err == nil {
		t.Fatal("expected mid-transfer download error")
	}
	if _, statErr := os.Stat(partialPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected truncated file %q to be removed on failure, statErr=%v", partialPath, statErr)
	}
}

func TestSFTPUploadAtomicPreservesExistingAndInvalidatesCacheOnError(t *testing.T) {
	fs := &memRWFS{
		nodes: map[string]*memRWNode{
			"/":             {name: "/", mode: os.ModeDir | 0o755},
			"/dir":          {name: "dir", mode: os.ModeDir | 0o755},
			"/dir/app.conf": {name: "app.conf", mode: 0o600, data: []byte("original-good-data")},
		},
		failMidWrite: true,
	}
	cli := newPipeConcurrentSFTPClient(t, fs)
	sink := &captureSink{}
	m := NewManager(nil, sink)
	sess := &sessionState{
		sftpClient: cli,
		stopCh:     make(chan struct{}),
		dirCache:   map[string]dirCacheEntry{"/dir": {fetchedAt: time.Now()}},
	}
	m.sessions["s1"] = sess

	localFile := filepath.Join(t.TempDir(), "app.conf")
	if err := os.WriteFile(localFile, bytes.Repeat([]byte("NEW-DATA-"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.SFTPUpload("s1", localFile, "/dir"); err == nil {
		t.Fatal("expected mid-upload write error")
	}

	fs.mu.Lock()
	orig := string(fs.nodes["/dir/app.conf"].data)
	leftoverTmp := ""
	for k := range fs.nodes {
		if strings.Contains(k, ".gssh-tmp-") {
			leftoverTmp = k
		}
	}
	fs.mu.Unlock()

	if orig != "original-good-data" {
		t.Fatalf("expected existing target preserved on failed atomic upload, got %q", orig)
	}
	if leftoverTmp != "" {
		t.Fatalf("expected failed upload temp file cleaned up, found %q", leftoverTmp)
	}
	sess.mu.Lock()
	_, stillCached := sess.dirCache["/dir"]
	sess.mu.Unlock()
	if stillCached {
		t.Fatal("expected dirCache for /dir to be invalidated even on upload error")
	}

	// Verify rename-failure fallback works cleanly without leaving temp files.
	fs.mu.Lock()
	fs.failMidWrite = false
	fs.failRename = true
	fs.mu.Unlock()
	if err := m.SFTPUpload("s1", localFile, "/dir"); err != nil {
		t.Fatalf("expected in-place fallback upload to succeed when rename fails: %v", err)
	}
	fs.mu.Lock()
	gotFallback := string(fs.nodes["/dir/app.conf"].data)
	for k := range fs.nodes {
		if strings.Contains(k, ".gssh-tmp-") {
			t.Fatalf("leftover temp file after rename fallback: %q", k)
		}
	}
	fs.mu.Unlock()
	if !strings.HasPrefix(gotFallback, "NEW-DATA-") {
		t.Fatalf("unexpected fallback upload data: %q", gotFallback[:min(len(gotFallback), 32)])
	}
}

func TestSFTPWriteTextFilePreservesSymlink(t *testing.T) {
	fs := &memRWFS{
		nodes: map[string]*memRWNode{
			"/":                       {name: "/", mode: os.ModeDir | 0o755},
			"/sites-available":        {name: "sites-available", mode: os.ModeDir | 0o755},
			"/sites-enabled":          {name: "sites-enabled", mode: os.ModeDir | 0o755},
			"/sites-available/site":   {name: "site", mode: 0o640, data: []byte("old-v1\n")},
			"/sites-enabled/site":     {name: "site", mode: os.ModeSymlink | 0o777, target: "/sites-available/site"},
		},
		resolveRealPath: true,
	}
	cli := newPipeConcurrentSFTPClient(t, fs)
	m := NewManager(nil, nil)
	sess := &sessionState{
		sftpClient: cli,
		stopCh:     make(chan struct{}),
		dirCache: map[string]dirCacheEntry{
			"/sites-enabled":   {fetchedAt: time.Now()},
			"/sites-available": {fetchedAt: time.Now()},
		},
	}
	m.sessions["s1"] = sess

	// 1. Server resolves RealPath -> atomically updates target and keeps symlink intact.
	if err := m.SFTPWriteTextFile("s1", "/sites-enabled/site", "new-v2\n"); err != nil {
		t.Fatalf("SFTPWriteTextFile via symlink failed: %v", err)
	}
	fs.mu.Lock()
	linkNode := fs.nodes["/sites-enabled/site"]
	targetNode := fs.nodes["/sites-available/site"]
	fs.mu.Unlock()
	if linkNode == nil || linkNode.mode&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink to be preserved, got %+v", linkNode)
	}
	if targetNode == nil || string(targetNode.data) != "new-v2\n" || targetNode.mode.Perm() != 0o640 {
		t.Fatalf("expected symlink target updated with mode 0640, got %+v", targetNode)
	}
	sess.mu.Lock()
	_, cachedEnabled := sess.dirCache["/sites-enabled"]
	_, cachedAvail := sess.dirCache["/sites-available"]
	sess.mu.Unlock()
	if cachedEnabled || cachedAvail {
		t.Fatal("expected both symlink dir and target dir caches to be invalidated")
	}

	// 2. Server does NOT resolve RealPath -> must NOT rename over symlink; writes in-place through link.
	fs.mu.Lock()
	fs.resolveRealPath = false
	fs.mu.Unlock()
	if err := m.SFTPWriteTextFile("s1", "/sites-enabled/site", "new-v3\n"); err != nil {
		t.Fatalf("SFTPWriteTextFile via unresolved symlink failed: %v", err)
	}
	fs.mu.Lock()
	linkNode2 := fs.nodes["/sites-enabled/site"]
	targetNode2 := fs.nodes["/sites-available/site"]
	fs.mu.Unlock()
	if linkNode2 == nil || linkNode2.mode&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink to remain a symlink when RealPath returns unchanged path, got %+v", linkNode2)
	}
	if targetNode2 == nil || string(targetNode2.data) != "new-v3\n" {
		t.Fatalf("expected in-place write to follow symlink to target, got %+v", targetNode2)
	}
}
