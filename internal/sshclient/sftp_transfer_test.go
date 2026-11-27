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
	name string
	mode os.FileMode
	data []byte
}

func (n *memRWNode) Name() string       { return n.name }
func (n *memRWNode) Size() int64        { return int64(len(n.data)) }
func (n *memRWNode) Mode() os.FileMode  { return n.mode }
func (n *memRWNode) ModTime() time.Time { return time.Time{} }
func (n *memRWNode) IsDir() bool        { return n.mode.IsDir() }
func (n *memRWNode) Sys() any           { return nil }

type memRWFS struct {
	mu           sync.Mutex
	nodes        map[string]*memRWNode
	reportZeroSz bool
	failRename   bool
	failMidRead  bool
}

type memWriterAt struct {
	fs   *memRWFS
	path string
}

func (w *memWriterAt) WriteAt(p []byte, off int64) (int, error) {
	w.fs.mu.Lock()
	defer w.fs.mu.Unlock()
	n, ok := w.fs.nodes[w.path]
	if !ok {
		n = &memRWNode{name: path.Base(w.path), mode: 0o644}
		w.fs.nodes[w.path] = n
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
	f.nodes[p] = &memRWNode{name: path.Base(p), mode: 0o644}
	return &memWriterAt{fs: f, path: p}, nil
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
	if !bytes.Equal(fs.nodes["/dir/big.bin"].data, payload) {
		t.Fatalf("uploaded content mismatch: got %d bytes, want %d", len(fs.nodes["/dir/big.bin"].data), len(payload))
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
	if string(fs.nodes["/dir/conf.ini"].data) != "k=v1\n" {
		t.Fatalf("unexpected conf.ini data: %q", string(fs.nodes["/dir/conf.ini"].data))
	}
	fs.failRename = true
	if err := m.SFTPWriteTextFile("s1", "/dir/conf.ini", "k=v2\n"); err != nil {
		t.Fatalf("SFTPWriteTextFile fallback in-place failed: %v", err)
	}
	if string(fs.nodes["/dir/conf.ini"].data) != "k=v2\n" {
		t.Fatalf("unexpected fallback conf.ini data: %q", string(fs.nodes["/dir/conf.ini"].data))
	}
	fs.failRename = false

	// Editor: reject len(raw) > maxEditableFileSize even when Stat reports size 0.
	fs.nodes["/dir/virtual.log"] = &memRWNode{
		name: "virtual.log",
		mode: 0o644,
		data: bytes.Repeat([]byte("x"), maxEditableFileSize+10),
	}
	fs.reportZeroSz = true
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
