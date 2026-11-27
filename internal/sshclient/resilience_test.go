package sshclient

import (
	"context"
	"errors"
	"net"
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
