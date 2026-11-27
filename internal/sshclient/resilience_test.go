package sshclient

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"golang.org/x/crypto/ssh"
)

// startStallingSSHServer starts an SSH server that completes the SSH handshake
// and then optionally stalls global requests, session channel opens, or pty-req.
func startStallingSSHServer(
	t *testing.T,
	stallGlobalReq bool,
	stallSessionOpen bool,
	stallPtyReq bool,
	sessionOpened *atomic.Int32,
	connClosed chan struct{},
) (*ssh.Client, string, int) {
	t.Helper()
	pem, _, err := sshkey.GenerateKeyPair("ed25519", "stall-host", "")
	if err != nil {
		t.Fatalf("gen host key: %v", err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		t.Fatalf("parse host key: %v", err)
	}
	scfg := &ssh.ServerConfig{NoClientAuth: true}
	scfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				defer func() {
					_ = nc.Close()
					if connClosed != nil {
						select {
						case connClosed <- struct{}{}:
						default:
						}
					}
				}()
				sc, chs, reqs, err := ssh.NewServerConn(nc, scfg)
				if err != nil {
					return
				}
				defer sc.Close()

				if stallGlobalReq {
					go func() {
						for range reqs {
							// Intentionally never reply to simulate a blackholed link.
							<-done
							return
						}
					}()
				} else {
					go ssh.DiscardRequests(reqs)
				}

				for chReq := range chs {
					if sessionOpened != nil {
						sessionOpened.Add(1)
					}
					if stallSessionOpen {
						// Intentionally never Accept or Reject the session channel open.
						<-done
						return
					}
					ch, cr, err := chReq.Accept()
					if err != nil {
						return
					}
					go func(ch ssh.Channel, cr <-chan *ssh.Request) {
						defer ch.Close()
						for r := range cr {
							if stallPtyReq && r.Type == "pty-req" {
								// Intentionally never reply to pty-req.
								<-done
								return
							}
							_ = r.Reply(true, nil)
						}
					}(ch, cr)
				}
			}(c)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	cli, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User:            "u",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli, addr.IP.String(), addr.Port
}

func TestDialCancelAfterSuccessDoesNotCloseLiveConn(t *testing.T) {
	useTempKnownHosts(t)

	jumpHost, jumpPort, stopJump := startTestSSHServer(t, "jump", "jpass", nil)
	defer stopJump()
	targetHost, targetPort, stopTarget := startTestSSHServer(t, "user", "upass", nil)
	defer stopTarget()
	proxyHost, proxyPort, stopProxy := startTestHTTPConnectProxy(t, "puser", "ppass")
	defer stopProxy()

	resolver := &memResolver{
		servers: map[string]*models.ServerConfig{
			"jump": {
				ID: "jump", Name: "Jump", Host: jumpHost, Port: jumpPort,
				Username: "jump", AuthType: models.AuthPassword, Password: "jpass",
				TimeoutSeconds: 5,
				Proxy: models.ProxyConfig{
					Type: models.ProxyHTTP, Host: proxyHost, Port: proxyPort,
					Username: "puser", Password: "ppass",
				},
			},
		},
	}
	targetCfg := &models.ServerConfig{
		ID: "target", Name: "Target", Host: targetHost, Port: targetPort,
		Username: "user", AuthType: models.AuthPassword, Password: "upass",
		TimeoutSeconds: 5,
		JumpHost:       models.JumpHostConfig{Mode: models.JumpServer, ServerID: "jump"},
	}

	for i := 0; i < 15; i++ {
		dialCtx, cancel := context.WithCancel(context.Background())
		res, err := DialServer(dialCtx, targetCfg, resolver)
		if err != nil {
			cancel()
			t.Fatalf("iter %d: DialServer failed: %v", i, err)
		}
		// Immediately cancel dialCtx just like establishSession's defer does.
		cancel()
		// Yield briefly so any stray watcher goroutine would have raced to close sockets.
		time.Sleep(5 * time.Millisecond)

		if _, _, err := res.Client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			res.Close()
			t.Fatalf("iter %d: live SSH client was closed after dialCtx cancel: %v", i, err)
		}
		res.Close()
	}
}

func TestSOCKS5DialRespectsTimeoutOnBlackholedProxy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Accept connections and intentionally blackhole the SOCKS5 handshake.
	accepted := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	defer func() {
		for {
			select {
			case c := <-accepted:
				_ = c.Close()
			default:
				return
			}
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	start := time.Now()
	conn, err := dialViaProxy(
		context.Background(),
		models.ProxyConfig{Type: models.ProxySOCKS5, Host: addr.IP.String(), Port: addr.Port},
		"127.0.0.1:22",
		200*time.Millisecond,
	)
	elapsed := time.Since(start)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("expected SOCKS5 dial to fail on blackholed proxy")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("SOCKS5 dial took too long (%v); timeout was not enforced", elapsed)
	}
}

func TestRunSSHExecTimesOutWhenNewSessionStalls(t *testing.T) {
	cli, _, _ := startStallingSSHServer(t, false, true, false, nil, nil)

	start := time.Now()
	_, err := runSSHExec(context.Background(), cli, "echo hi", 200*time.Millisecond, false)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("runSSHExec took %v instead of timing out promptly", elapsed)
	}
}

func TestGetSessionMetricsReturnsEarlyOnRTTProbeTimeout(t *testing.T) {
	var sessionOpens atomic.Int32
	cli, _, _ := startStallingSSHServer(t, true, false, false, &sessionOpens, nil)

	prevTimeout := rttProbeTimeout
	rttProbeTimeout = 150 * time.Millisecond
	t.Cleanup(func() { rttProbeTimeout = prevTimeout })

	m := NewManager(nil, nil)
	s := &sessionState{
		dialRes:  &DialResult{Client: cli},
		stopCh:   make(chan struct{}),
		dirCache: map[string]dirCacheEntry{},
	}
	m.sessions["s1"] = s

	start := time.Now()
	_, err := m.GetSessionMetrics("s1")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error when RTT probe times out")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("GetSessionMetrics blocked for %v instead of failing fast on RTT timeout", elapsed)
	}
	if got := sessionOpens.Load(); got != 0 {
		t.Fatalf("expected 0 exec session opens after RTT probe timeout, got %d", got)
	}
}

func TestOpenShellSessionCancelsAndClosesOnDisconnect(t *testing.T) {
	connClosed := make(chan struct{}, 1)
	cli, _, _ := startStallingSSHServer(t, false, false, true, nil, connClosed)
	dialRes := &DialResult{Client: cli}

	m := NewManager(nil, nil)
	dialCtx, cancelDial := context.WithCancel(context.Background())
	sess := &sessionState{
		info:        models.SessionInfo{SessionID: "stall-1", Status: "connecting"},
		cancelDial:  cancelDial,
		pendingDial: dialRes,
		stopCh:      make(chan struct{}),
		dirCache:    map[string]dirCacheEntry{},
	}
	m.sessions["stall-1"] = sess

	errCh := make(chan error, 1)
	go func() {
		_, err := openShellSession(dialCtx, dialRes, 80, 24)
		errCh <- err
	}()

	// Give openShellSession a moment to enter RequestPty and stall.
	time.Sleep(80 * time.Millisecond)
	start := time.Now()
	m.Disconnect("stall-1")

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected openShellSession to fail when disconnected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("openShellSession leaked and did not unblock after Disconnect")
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Disconnect unblock took too long: %v", elapsed)
	}

	select {
	case <-connClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected underlying SSH connection to be closed after Disconnect during stalled openShellSession")
	}
}

// startStallingExecServer starts an SSH server that accepts session channels and
// replies true to "exec", optionally writes initialOutput, and then intentionally
// never exits or closes the channel (simulating a stuck remote command or blackhole).
func startStallingExecServer(t *testing.T, initialOutput []byte, gotSigKill *atomic.Bool) *ssh.Client {
	t.Helper()
	pem, _, err := sshkey.GenerateKeyPair("ed25519", "stall-exec", "")
	if err != nil {
		t.Fatalf("gen host key: %v", err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		t.Fatalf("parse host key: %v", err)
	}
	scfg := &ssh.ServerConfig{NoClientAuth: true}
	scfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})

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
				go func() {
					for r := range reqs {
						if r.WantReply {
							_ = r.Reply(true, nil)
						}
					}
				}()
				for chReq := range chs {
					ch, cr, err := chReq.Accept()
					if err != nil {
						return
					}
					go func(ch ssh.Channel, cr <-chan *ssh.Request) {
						defer ch.Close()
						for r := range cr {
							switch r.Type {
							case "exec":
								_ = r.Reply(true, nil)
								if len(initialOutput) > 0 {
									_, _ = ch.Write(initialOutput)
								}
							case "signal":
								if gotSigKill != nil && strings.Contains(string(r.Payload), string(ssh.SIGKILL)) {
									gotSigKill.Store(true)
								}
								if r.WantReply {
									_ = r.Reply(true, nil)
								}
							default:
								if r.WantReply {
									_ = r.Reply(false, nil)
								}
							}
						}
						<-done
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
		t.Fatalf("ssh dial: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func TestRunSSHExecUnblocksPromptlyOnStalledCommand(t *testing.T) {
	var gotKill atomic.Bool
	cli := startStallingExecServer(t, nil, &gotKill)

	start := time.Now()
	_, err := runSSHExec(context.Background(), cli, "sleep 999", 180*time.Millisecond, true)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("runSSHExec hung for %v past timeout", elapsed)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for !gotKill.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !gotKill.Load() {
		t.Fatal("expected SIGKILL to be sent to stalled exec session on timeout")
	}
}

func TestGetSessionMetricsUnblocksOnStalledExecAndClearsInFlight(t *testing.T) {
	cli := startStallingExecServer(t, nil, nil)

	prevTimeout := metricsExecTimeout
	metricsExecTimeout = 150 * time.Millisecond
	t.Cleanup(func() { metricsExecTimeout = prevTimeout })

	m := NewManager(nil, nil)
	s := &sessionState{
		dialRes:  &DialResult{Client: cli},
		stopCh:   make(chan struct{}),
		dirCache: map[string]dirCacheEntry{},
	}
	m.sessions["s1"] = s

	start := time.Now()
	_, err := m.GetSessionMetrics("s1")
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded on stalled metrics exec, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("GetSessionMetrics hung for %v", elapsed)
	}
	if s.metricsInFlight.Load() {
		t.Fatal("expected metricsInFlight to be cleared after stalled exec timeout")
	}
}

func TestSFTPDownloadPackedUnblocksOnTimeoutAndEarlyUnpackError(t *testing.T) {
	// 1. Timeout when remote tar never sends output or exits.
	cli1 := startStallingExecServer(t, nil, nil)
	prevTimeout := packedDownloadTimeout
	packedDownloadTimeout = 180 * time.Millisecond
	t.Cleanup(func() { packedDownloadTimeout = prevTimeout })

	sink1 := &captureSink{}
	m1 := NewManager(nil, sink1)
	m1.sessions["s1"] = &sessionState{dialRes: &DialResult{Client: cli1}, stopCh: make(chan struct{})}

	start := time.Now()
	_, err := m1.SFTPDownloadPacked("s1", "/var/log/app", t.TempDir())
	if err == nil {
		t.Fatal("expected timeout error from stalled SFTPDownloadPacked")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("SFTPDownloadPacked hung for %v past timeout", elapsed)
	}

	// 2. Early unpackErr (path traversal) while remote tar stays open forever must NOT hang in Wait().
	packedDownloadTimeout = 30 * time.Second
	var evilTar bytes.Buffer
	tw := tar.NewWriter(&evilTar)
	_ = tw.WriteHeader(&tar.Header{Name: "../../evil.txt", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Flush()

	var gotKill atomic.Bool
	cli2 := startStallingExecServer(t, evilTar.Bytes(), &gotKill)
	m2 := NewManager(nil, nil)
	m2.sessions["s2"] = &sessionState{dialRes: &DialResult{Client: cli2}, stopCh: make(chan struct{})}

	start2 := time.Now()
	_, err2 := m2.SFTPDownloadPacked("s2", "/var/log/app", t.TempDir())
	if err2 == nil || !strings.Contains(err2.Error(), "不安全") {
		t.Fatalf("expected unsafe path error, got: %v", err2)
	}
	if elapsed := time.Since(start2); elapsed > 2*time.Second {
		t.Fatalf("SFTPDownloadPacked hung in Wait() for %v after early unpackErr", elapsed)
	}
}

func TestUpgradeSSHConnJumpHostEnforcesTimeoutOnHungTarget(t *testing.T) {
	useTempKnownHosts(t)

	jumpHost, jumpPort, stopJump := startTestSSHServer(t, "jump", "jpass", nil)
	defer stopJump()

	// Target accepts TCP via jump host direct-tcpip, then stalls SSH banner forever.
	tarpitLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tarpit: %v", err)
	}
	defer tarpitLn.Close()
	tarpitAccepted := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := tarpitLn.Accept()
			if err != nil {
				return
			}
			tarpitAccepted <- c
		}
	}()
	defer func() {
		for {
			select {
			case c := <-tarpitAccepted:
				_ = c.Close()
			default:
				return
			}
		}
	}()
	tarpitAddr := tarpitLn.Addr().(*net.TCPAddr)

	resolver := &memResolver{
		servers: map[string]*models.ServerConfig{
			"jump-1": {
				ID: "jump-1", Name: "Jump", Host: jumpHost, Port: jumpPort,
				Username: "jump", AuthType: models.AuthPassword, Password: "jpass",
				TimeoutSeconds: 5,
			},
		},
	}
	targetCfg := &models.ServerConfig{
		ID: "target-1", Name: "TarpitTarget", Host: tarpitAddr.IP.String(), Port: tarpitAddr.Port,
		Username: "root", AuthType: models.AuthPassword, Password: "x",
		TimeoutSeconds: 1,
		JumpHost:       models.JumpHostConfig{Mode: models.JumpServer, ServerID: "jump-1"},
	}

	// Note parent ctx has NO deadline; only targetCfg.TimeoutSeconds=1 bounds upgradeSSHConn over chanConn.
	start := time.Now()
	res, err := DialServer(context.Background(), targetCfg, resolver)
	elapsed := time.Since(start)
	if res != nil {
		res.Close()
	}
	if err == nil {
		t.Fatal("expected DialServer over JumpHost to time out against hung SSH target")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("DialServer over JumpHost took %v; upgradeSSHConn did not enforce TimeoutSeconds", elapsed)
	}
}

type statusCaptureSink struct {
	mu       sync.Mutex
	statuses []string
}

func (s *statusCaptureSink) Emit(eventName string, data ...any) {
	if eventName == "session:status" && len(data) == 1 {
		if info, ok := data[0].(models.SessionInfo); ok {
			s.mu.Lock()
			s.statuses = append(s.statuses, info.Status)
			s.mu.Unlock()
		}
	}
}

func TestDisconnectDuringEstablishNeverEmitsConnectedAfterDisconnected(t *testing.T) {
	useTempKnownHosts(t)

	// Start an SSH server that accepts PTY + shell and pauses inside window-change so
	// Disconnect deterministically races establishSession right before connected commit.
	pem, _, err := sshkey.GenerateKeyPair("ed25519", "race-host", "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		t.Fatal(err)
	}
	scfg := &ssh.ServerConfig{NoClientAuth: true}
	scfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	inPtyReq := make(chan struct{}, 1)
	releasePtyReq := make(chan struct{})
	inWindowChange := make(chan struct{}, 1)
	releaseWindowChange := make(chan struct{})
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
							switch r.Type {
							case "pty-req":
								select {
								case inPtyReq <- struct{}{}:
								default:
								}
								<-releasePtyReq
								if r.WantReply {
									_ = r.Reply(true, nil)
								}
							case "shell", "env":
								if r.WantReply {
									_ = r.Reply(true, nil)
								}
							case "window-change":
								select {
								case inWindowChange <- struct{}{}:
								default:
								}
								<-releaseWindowChange
							default:
								if r.WantReply {
									_ = r.Reply(false, nil)
								}
							}
						}
					}(ch, cr)
				}
			}(c)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	sink := &statusCaptureSink{}
	m := NewManager(nil, sink)
	dialCtx, cancelDial := context.WithCancel(context.Background())
	sess := &sessionState{
		info:       models.SessionInfo{SessionID: "race-1", Status: "connecting"},
		cancelDial: cancelDial,
		stopCh:     make(chan struct{}),
		lastCols:   80,
		lastRows:   24,
		dirCache:   map[string]dirCacheEntry{},
	}
	m.sessions["race-1"] = sess

	srv := &models.ServerConfig{
		ID: "srv-race", Name: "RaceSrv", Host: addr.IP.String(), Port: addr.Port,
		Username: "root", AuthType: models.AuthPassword, Password: "x", TimeoutSeconds: 5,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.establishSession(dialCtx, sess, srv, 80, 24)
	}()

	// Wait until openShellSession has started PTY setup, then resize so WindowChange runs before commit.
	select {
	case <-inPtyReq:
	case <-time.After(3 * time.Second):
		close(releasePtyReq)
		close(releaseWindowChange)
		t.Fatal("timed out waiting for establishSession to reach pty-req")
	}
	_ = m.Resize("race-1", 100, 30)
	close(releasePtyReq)

	select {
	case <-inWindowChange:
	case <-time.After(3 * time.Second):
		close(releaseWindowChange)
		t.Fatal("timed out waiting for establishSession to reach WindowChange")
	}

	// Disconnect while establishSession is in WindowChange.
	m.Disconnect("race-1")
	close(releaseWindowChange)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("establishSession did not return after Disconnect")
	}

	// Late emitSessionStatus (e.g. from initSessionSFTP) must also be blocked after terminal status.
	if m.emitSessionStatus(sess, models.SessionInfo{SessionID: "race-1", Status: "connected", SFTPReady: true}, false, nil) {
		t.Fatal("expected late non-terminal emitSessionStatus to be rejected after Disconnect")
	}

	sink.mu.Lock()
	statuses := append([]string(nil), sink.statuses...)
	sink.mu.Unlock()
	for i, st := range statuses {
		if st == "disconnected" {
			for _, later := range statuses[i+1:] {
				if later == "connected" {
					t.Fatalf("emitted 'connected' after 'disconnected': %v", statuses)
				}
			}
		}
	}
	if len(statuses) == 0 || statuses[len(statuses)-1] != "disconnected" {
		t.Fatalf("expected final status 'disconnected', got %v", statuses)
	}
}

func TestKeepAliveLoopTimerNoDoubleCountWhenIntervalShort(t *testing.T) {
	cli, _, _ := startStallingSSHServer(t, true, false, false, nil, nil)

	prevTimeout := keepAliveProbeTimeout
	keepAliveProbeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { keepAliveProbeTimeout = prevTimeout })

	m := NewManager(nil, nil)
	stopCh := make(chan struct{})
	sess := &sessionState{
		info:     models.SessionInfo{SessionID: "ka-1", Status: "connected"},
		dialRes:  &DialResult{Client: cli},
		stopCh:   stopCh,
		dirCache: map[string]dirCacheEntry{},
	}
	m.sessions["ka-1"] = sess

	// interval (40ms) < keepAliveProbeTimeout (100ms):
	// Previously time.Ticker buffered a tick during the 100ms probe and fired at 0ms later
	// (double-counting errProbeInFlight so disconnect happened after ~140ms).
	// With a resettable time.Timer, 3 failures require:
	//   wait 40ms + probe 100ms (fail 1) + wait 40ms + probe 0ms (inFlight fail 2) + wait 40ms + probe 0ms (inFlight fail 3) >= 220ms
	// (specifically 3 full intervals + probe timeout, never 0ms immediate double-count!).
	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.keepAliveLoop(sess, stopCh, 50*time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		close(stopCh)
		t.Fatal("keepAliveLoop did not disconnect after maxKeepAliveFailures")
	}
	elapsed := time.Since(start)
	// With 3 intervals of 50ms + 1st probe timeout 100ms = ~250ms minimum; old ticker bug disconnected at ~150ms.
	if elapsed < 200*time.Millisecond {
		t.Fatalf("keepAliveLoop disconnected too quickly (%v), indicating buffered tick double-counted errProbeInFlight", elapsed)
	}
}
