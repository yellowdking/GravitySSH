package sshclient

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"golang.org/x/crypto/ssh"
)

func TestDirCacheCap64LRUEvictionAndExpiredPurge(t *testing.T) {
	s := &sessionState{
		dirCache: make(map[string]dirCacheEntry),
	}

	// Insert 64 entries: /dir-00 .. /dir-63
	for i := 0; i < maxDirCacheEntries; i++ {
		dir := fmt.Sprintf("/dir-%02d", i)
		putDirCacheIfGen(s, dir, []models.SFTPFileEntry{{Name: "f"}}, 0)
	}
	s.mu.Lock()
	if len(s.dirCache) != maxDirCacheEntries {
		s.mu.Unlock()
		t.Fatalf("expected %d cached dirs, got %d", maxDirCacheEntries, len(s.dirCache))
	}
	// Touch /dir-00 so it becomes MRU; /dir-01 becomes LRU victim.
	if _, ok := peekCacheLocked(s, "/dir-00"); !ok {
		s.mu.Unlock()
		t.Fatal("expected /dir-00 cache hit")
	}
	s.mu.Unlock()

	// Insert 16 more dirs (/dir-64 .. /dir-79) -> cap stays strictly at 64.
	for i := maxDirCacheEntries; i < maxDirCacheEntries+16; i++ {
		dir := fmt.Sprintf("/dir-%02d", i)
		putDirCacheIfGen(s, dir, []models.SFTPFileEntry{{Name: "f"}}, 0)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.dirCache) != maxDirCacheEntries {
		t.Fatalf("dirCache exceeded cap %d: got %d", maxDirCacheEntries, len(s.dirCache))
	}
	if _, ok := s.dirCache["/dir-00"]; !ok {
		t.Fatal("expected recently accessed /dir-00 to survive LRU eviction")
	}
	if _, ok := s.dirCache["/dir-01"]; ok {
		t.Fatal("expected least-recently-used /dir-01 to be evicted")
	}
	if _, ok := s.dirCache["/dir-79"]; !ok {
		t.Fatal("expected newest /dir-79 to remain cached")
	}

	// Expired entries are purged before evicting live entries.
	stale := s.dirCache["/dir-79"]
	stale.fetchedAt = time.Now().Add(-(dirCacheTTL + time.Second))
	s.dirCache["/dir-79"] = stale
	s.mu.Unlock()

	putDirCacheIfGen(s, "/dir-fresh", []models.SFTPFileEntry{{Name: "fresh"}}, 0)

	s.mu.Lock()
	if _, ok := s.dirCache["/dir-79"]; ok {
		t.Fatal("expected expired /dir-79 to be purged")
	}
	if _, ok := s.dirCache["/dir-00"]; !ok {
		t.Fatal("expected live /dir-00 preserved when expired entry freed a slot")
	}
	if len(s.dirCache) > maxDirCacheEntries {
		t.Fatalf("dirCache exceeded cap after purge: %d", len(s.dirCache))
	}
}

func TestGetSFTPSingleFlightAndBoundedTimeout(t *testing.T) {
	cli := startMockSSHSFTPServer(t, &mFS{m: map[string]*mNode{}}, nil)

	// 1. Single-flight deduplication: 10 concurrent getSFTP calls share 1 dial.
	m := NewManager(nil, nil)
	var dialCount atomic.Int32
	gate := make(chan struct{})
	inDial := make(chan struct{}, 1)
	m.sftpDial = func(sessionID string) (*sftp.Client, error) {
		dialCount.Add(1)
		select {
		case inDial <- struct{}{}:
		default:
		}
		<-gate
		return newSFTPClient(cli)
	}
	m.sessions["s1"] = &sessionState{
		stopCh:   make(chan struct{}),
		dirCache: make(map[string]dirCacheEntry),
	}

	const workers = 10
	results := make([]*sftp.Client, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = m.getSFTP("s1")
		}(i)
	}

	<-inDial
	// Brief wait so other goroutines join the in-flight call.
	time.Sleep(30 * time.Millisecond)
	close(gate)
	wg.Wait()

	if got := dialCount.Load(); got != 1 {
		t.Fatalf("expected single-flight SFTP init to dial once, got %d", got)
	}
	for i := 0; i < workers; i++ {
		if errs[i] != nil || results[i] == nil {
			t.Fatalf("worker %d failed: client=%v err=%v", i, results[i], errs[i])
		}
		if results[i] != results[0] {
			t.Fatalf("worker %d received distinct SFTP client instance", i)
		}
	}
	_ = results[0].Close()

	// 2. Bounded timeout on stalled SFTP subsystem init.
	prevTimeout := sftpInitTimeout
	sftpInitTimeout = 150 * time.Millisecond
	t.Cleanup(func() { sftpInitTimeout = prevTimeout })

	stallCli, _, _ := startStallingSSHServer(t, false, true, false, nil, nil)
	m2 := NewManager(nil, nil)
	m2.sessions["s-stall"] = &sessionState{
		dialRes:  &DialResult{Client: stallCli},
		stopCh:   make(chan struct{}),
		dirCache: make(map[string]dirCacheEntry),
	}

	start := time.Now()
	var wg2 sync.WaitGroup
	errCh := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			_, err := m2.getSFTP("s-stall")
			errCh <- err
		}()
	}
	wg2.Wait()
	close(errCh)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("timed single-flight SFTP init hung for %v", elapsed)
	}
	for err := range errCh {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded from stalled SFTP init, got: %v", err)
		}
	}
	m2.sessions["s-stall"].mu.Lock()
	stillInFlight := m2.sessions["s-stall"].sftpInit != nil
	m2.sessions["s-stall"].mu.Unlock()
	if stillInFlight {
		t.Fatal("expected sftpInit flight to be cleared after timeout")
	}
}

func TestBoundedBufferAndRunSSHExecOutputCap(t *testing.T) {
	headBuf := newBoundedBuffer(16, false)
	for _, chunk := range []string{"0123456789", "abcdefghij", "KLMNOPQRST"} {
		n, err := headBuf.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("headBuf.Write(%q) = (%d, %v)", chunk, n, err)
		}
	}
	if got := headBuf.String(); got != "0123456789abcdef" {
		t.Fatalf("headBuf mismatch: got %q", got)
	}

	tailBuf := newBoundedBuffer(10, true)
	for _, chunk := range []string{"012345", "6789abc", "DEF"} {
		n, err := tailBuf.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("tailBuf.Write(%q) = (%d, %v)", chunk, n, err)
		}
	}
	if got := tailBuf.String(); got != "6789abcDEF" {
		t.Fatalf("tailBuf mismatch: got %q", got)
	}

	// End-to-end runSSHExec capping >256 KiB remote output cleanly.
	bigOut := strings.Repeat("X", maxSSHExecOutputBytes+128*1024) + "TAIL_MARKER"
	cli := startMockSSHSFTPServer(t, &mFS{m: map[string]*mNode{}}, func(cmd string) (string, uint32) {
		return bigOut, 0
	})
	outCombined, err := runSSHExec(context.Background(), cli, "noisy-cmd", 5*time.Second, true)
	if err != nil {
		t.Fatalf("runSSHExec combined failed: %v", err)
	}
	if len(outCombined) != maxSSHExecOutputBytes {
		t.Fatalf("expected combined output capped at %d bytes, got %d", maxSSHExecOutputBytes, len(outCombined))
	}
	if !strings.HasSuffix(string(outCombined), "TAIL_MARKER") {
		t.Fatal("expected combined bounded output to retain tail marker")
	}

	outHead, err := runSSHExec(context.Background(), cli, "noisy-cmd", 5*time.Second, false)
	if err != nil {
		t.Fatalf("runSSHExec stdout failed: %v", err)
	}
	if len(outHead) != maxSSHExecOutputBytes {
		t.Fatalf("expected stdout capped at %d bytes, got %d", maxSSHExecOutputBytes, len(outHead))
	}
}

type bufWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *bufWriteCloser) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *bufWriteCloser) Close() error { return nil }

func TestWriteInputReleasesWriteMuBeforeOnCommand(t *testing.T) {
	m := NewManager(nil, nil)
	stdin := &bufWriteCloser{}
	var nestedErr error
	callbackDone := make(chan struct{})

	sess := &sessionState{
		stdin: stdin,
	}
	sess.recorder = NewCommandRecorder(func(cmd string) {
		if cmd == "echo first" {
			// Re-entering WriteInput inside onCommand would deadlock if writeMu were still held.
			nestedErr = m.WriteInput("s1", "nested")
			close(callbackDone)
		}
	})
	m.sessions["s1"] = sess

	done := make(chan error, 1)
	go func() {
		done <- m.WriteInput("s1", "echo first\r")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WriteInput failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteInput deadlocked: writeMu was held across onCommand callback")
	}
	<-callbackDone
	if nestedErr != nil {
		t.Fatalf("re-entrant WriteInput from onCommand failed: %v", nestedErr)
	}
	stdin.mu.Lock()
	got := stdin.buf.String()
	stdin.mu.Unlock()
	if got != "echo first\rnested" {
		t.Fatalf("unexpected stdin stream: %q", got)
	}
}

type termDataSink struct {
	mu     sync.Mutex
	events [][]byte
}

func (s *termDataSink) Emit(eventName string, data ...any) {
	if eventName != "terminal:data" || len(data) != 1 {
		return
	}
	m, ok := data[0].(map[string]string)
	if !ok {
		return
	}
	dec, err := base64.StdEncoding.DecodeString(m["data"])
	if err != nil {
		return
	}
	s.mu.Lock()
	s.events = append(s.events, dec)
	s.mu.Unlock()
}

func TestPumpOutputCoalescingPreservesExactBytes(t *testing.T) {
	prevWin := pumpBatchWindow
	pumpBatchWindow = 15 * time.Millisecond
	t.Cleanup(func() { pumpBatchWindow = prevWin })

	sink := &termDataSink{}
	m := NewManager(nil, sink)
	pr, pw := io.Pipe()

	var want bytes.Buffer
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		m.pumpOutput("s1", pr, nil)
	}()

	// Write 120 small chunks back-to-back to simulate a terminal flood.
	for i := 0; i < 120; i++ {
		line := fmt.Sprintf("line-%03d: output flood payload\n", i)
		want.WriteString(line)
		if _, err := pw.Write([]byte(line)); err != nil {
			t.Fatalf("pipe write %d failed: %v", i, err)
		}
	}
	_ = pw.Close()

	select {
	case <-pumpDone:
	case <-time.After(3 * time.Second):
		t.Fatal("pumpOutput did not exit after EOF")
	}

	sink.mu.Lock()
	eventCount := len(sink.events)
	var got bytes.Buffer
	for _, ev := range sink.events {
		got.Write(ev)
	}
	sink.mu.Unlock()

	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("coalesced output mismatch: got %d bytes, want %d bytes", got.Len(), want.Len())
	}
	if eventCount >= 100 {
		t.Fatalf("expected pumpOutput to coalesce 120 burst chunks into far fewer events, got %d events", eventCount)
	}
}

type transferCaptureSink struct {
	mu     sync.Mutex
	events []models.TransferProgress
}

func (s *transferCaptureSink) Emit(eventName string, data ...any) {
	if eventName == "sftp:progress" && len(data) == 1 {
		if p, ok := data[0].(models.TransferProgress); ok {
			s.mu.Lock()
			s.events = append(s.events, p)
			s.mu.Unlock()
		}
	}
}

func TestSFTPDownloadPackedGzipStreamAndLiveProgress(t *testing.T) {
	// Build a real gzipped tar stream containing two files.
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gzw)
	data1 := bytes.Repeat([]byte("A"), 48*1024)
	data2 := bytes.Repeat([]byte("B"), 48*1024)
	_ = tw.WriteHeader(&tar.Header{Name: "app/", Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: "app/part1.bin", Typeflag: tar.TypeReg, Size: int64(len(data1)), Mode: 0o644})
	_, _ = tw.Write(data1)
	_ = tw.WriteHeader(&tar.Header{Name: "app/part2.bin", Typeflag: tar.TypeReg, Size: int64(len(data2)), Mode: 0o644})
	_, _ = tw.Write(data2)
	_ = tw.Close()
	_ = gzw.Close()
	payload := gzBuf.Bytes()

	// Start a custom SSH exec server that streams the gzipped tar in two chunks
	// separated by 120ms so live indeterminate progress events are emitted.
	hostPEM, _, err := sshkey.GenerateKeyPair("ed25519", "pack-host", "")
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.ParsePrivateKey([]byte(hostPEM))
	if err != nil {
		t.Fatal(err)
	}
	scfg := &ssh.ServerConfig{NoClientAuth: true}
	scfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var sawGzipCmd atomic.Bool
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				defer nc.Close()
				sc, chs, reqs, err := ssh.NewServerConn(nc, scfg)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(reqs)
				for chReq := range chs {
					ch, cr, err := chReq.Accept()
					if err != nil {
						return
					}
					go func(ch ssh.Channel, cr <-chan *ssh.Request) {
						defer ch.Close()
						for r := range cr {
							if r.Type == "exec" {
								if len(r.Payload) >= 4 && strings.Contains(string(r.Payload[4:]), "tar -czf -") {
									sawGzipCmd.Store(true)
								}
								_ = r.Reply(true, nil)
								mid := len(payload) / 2
								_, _ = ch.Write(payload[:mid])
								time.Sleep(130 * time.Millisecond)
								_, _ = ch.Write(payload[mid:])
								_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
								return
							}
						}
					}(ch, cr)
				}
			}(c)
		}
	}()

	cli, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User:            "u",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh.Dial failed: %v", err)
	}
	defer cli.Close()

	sink := &transferCaptureSink{}
	m := NewManager(nil, sink)
	m.sessions["s-pack"] = &sessionState{
		dialRes:  &DialResult{Client: cli},
		stopCh:   make(chan struct{}),
		dirCache: make(map[string]dirCacheEntry),
	}

	dest := t.TempDir()
	n, err := m.SFTPDownloadPacked("s-pack", "/srv/app", dest)
	if err != nil {
		t.Fatalf("SFTPDownloadPacked failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 unpacked files, got %d", n)
	}
	if !sawGzipCmd.Load() {
		t.Fatal("expected SFTPDownloadPacked to execute gzip-compressed tar -czf - command")
	}

	got1, err := os.ReadFile(filepath.Join(dest, "app", "part1.bin"))
	if err != nil || !bytes.Equal(got1, data1) {
		t.Fatalf("part1 mismatch: len=%d err=%v", len(got1), err)
	}
	got2, err := os.ReadFile(filepath.Join(dest, "app", "part2.bin"))
	if err != nil || !bytes.Equal(got2, data2) {
		t.Fatalf("part2 mismatch: len=%d err=%v", len(got2), err)
	}

	sink.mu.Lock()
	events := append([]models.TransferProgress(nil), sink.events...)
	sink.mu.Unlock()
	if len(events) < 3 {
		t.Fatalf("expected initial, live running, and done sftp:progress events, got %d: %+v", len(events), events)
	}
	if events[0].Status != "running" {
		t.Fatalf("expected first progress event status=running, got %+v", events[0])
	}
	hasLiveRunning := false
	for _, ev := range events[1 : len(events)-1] {
		if ev.Status == "running" && ev.Transferred > 0 {
			hasLiveRunning = true
			break
		}
	}
	if !hasLiveRunning {
		t.Fatalf("expected live intermediate running sftp:progress update with Transferred>0, got %+v", events)
	}
	last := events[len(events)-1]
	wantTotal := int64(len(data1) + len(data2))
	if last.Status != "done" || last.Percent != 100 || last.Transferred != wantTotal || last.Total != wantTotal {
		t.Fatalf("unexpected final transfer progress: %+v (want total=%d)", last, wantTotal)
	}
}
