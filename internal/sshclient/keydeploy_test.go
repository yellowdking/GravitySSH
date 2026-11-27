package sshclient

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"github.com/yellowdking/GravitySSH/internal/vault"
	"golang.org/x/crypto/ssh"
)

type testNoopProtector struct{}

func (testNoopProtector) Protect(p []byte) ([]byte, error)   { return append([]byte(nil), p...), nil }
func (testNoopProtector) Unprotect(c []byte) ([]byte, error) { return append([]byte(nil), c...), nil }
func (testNoopProtector) EngineName() string                 { return "test-noop" }
func (testNoopProtector) IsMachineBound() bool               { return false }
func (testNoopProtector) IsUserBound() bool                  { return false }

func TestSanitizeAuthorizedPublicKey(t *testing.T) {
	_, edInfo, err := sshkey.GenerateKeyPair("ed25519", "alice@laptop", "")
	if err != nil {
		t.Fatalf("gen ed25519: %v", err)
	}
	edPub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(edInfo.PublicKey))
	if err != nil {
		t.Fatalf("parse ed25519 pubkey: %v", err)
	}
	edClean := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(edPub)))

	// Valid key with comment -> stripped cleanly to "<type> <base64>".
	got, err := sanitizeAuthorizedPublicKey("  " + edClean + " alice@laptop  ")
	if err != nil {
		t.Fatalf("expected valid key with comment to succeed, got: %v", err)
	}
	if got != edClean {
		t.Fatalf("expected clean %q, got %q", edClean, got)
	}

	// Reject empty, control chars, options, multi-key, and flag-like invalid inputs.
	badInputs := []string{
		"",
		"   ",
		edClean + "\n" + edClean,
		edClean + "\r",
		edClean + "\x00",
		`command="id",no-pty ` + edClean,
		`from="10.0.0.1" ` + edClean,
		`-f /etc/passwd`,
		edClean + " comment\nsecond-line",
		"ssh-ed25519 NOT_VALID_BASE64!!!",
	}
	for _, bad := range badInputs {
		if res, err := sanitizeAuthorizedPublicKey(bad); err == nil {
			t.Fatalf("expected %q to be rejected, but got %q", bad, res)
		}
	}
}

func TestBuildKeyDeployCommandHardening(t *testing.T) {
	_, edInfo, err := sshkey.GenerateKeyPair("ed25519", "test", "")
	if err != nil {
		t.Fatal(err)
	}
	edPub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(edInfo.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	cleanKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(edPub)))
	cmd := buildKeyDeployCommand(cleanKey)

	for _, want := range []string{
		"env LC_ALL=C sh -c ",
		"umask 077",
		"chmod 700 --",
		"chmod 600 --",
		"grep -qxF --",
		"tail -c 1",
		"mv -f --",
		"flock -n 9",
		cleanKey,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected deploy command to contain %q, got:\n%s", want, cmd)
		}
	}
}

func TestDeploySSHKeyToServersSeparateExecTimeoutClosesConn(t *testing.T) {
	useTempKnownHosts(t)

	// Start an SSH server that authenticates with password, accepts exec, and stalls forever,
	// tracking when the client closes the TCP connection.
	hostPEM, _, err := sshkey.GenerateKeyPair("ed25519", "kd-host", "")
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.ParsePrivateKey([]byte(hostPEM))
	if err != nil {
		t.Fatal(err)
	}
	scfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "root" && string(pass) == "pw" {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		},
	}
	scfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var connClosed atomic.Bool
	connClosedCh := make(chan struct{}, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				defer func() {
					_ = nc.Close()
					connClosed.Store(true)
					select {
					case connClosedCh <- struct{}{}:
					default:
					}
				}()
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
								_ = r.Reply(true, nil)
							}
						}
					}(ch, cr)
				}
			}(c)
		}
	}()

	store, err := vault.Open(filepath.Join(t.TempDir(), "vault.gsv"), testNoopProtector{})
	if err != nil {
		t.Fatalf("vault.Open: %v", err)
	}
	defer store.Close()

	keyMeta, err := store.GenerateSSHKey("deploy-key", "ed25519", "", "unit-test")
	if err != nil {
		t.Fatalf("GenerateSSHKey: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	srvView, err := store.SaveServer(models.ServerSaveRequest{
		Name:           "StallDeploySrv",
		Host:           addr.IP.String(),
		Port:           addr.Port,
		Username:       "root",
		AuthType:       models.AuthPassword,
		Password:       "pw",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	prevExecTimeout := keyDeployExecTimeout
	keyDeployExecTimeout = 200 * time.Millisecond
	t.Cleanup(func() { keyDeployExecTimeout = prevExecTimeout })

	m := NewManager(store, nil)
	start := time.Now()
	results, err := m.DeploySSHKeyToServers(context.Background(), keyMeta.ID, []string{srvView.ID}, false)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("DeploySSHKeyToServers returned top-level error: %v", err)
	}
	if len(results) != 1 || results[0].OK {
		t.Fatalf("expected 1 failed deploy result on exec timeout, got %+v", results)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("DeploySSHKeyToServers hung for %v past exec timeout", elapsed)
	}

	select {
	case <-connClosedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected deployKeyToOneServer to close single-use SSH connection when execCtx timed out")
	}
	if !connClosed.Load() {
		t.Fatal("expected underlying TCP connection closed")
	}
}

func TestDeploySSHKeyToServersHappyPathDedupeBindAndErrorHandling(t *testing.T) {
	useTempKnownHosts(t)

	var execCount atomic.Int32
	var failRemote atomic.Bool

	hostPEM, _, err := sshkey.GenerateKeyPair("ed25519", "kd-ok-host", "")
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.ParsePrivateKey([]byte(hostPEM))
	if err != nil {
		t.Fatal(err)
	}
	scfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "root" && string(pass) == "pw" {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		},
	}
	scfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

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
								execCount.Add(1)
								_ = r.Reply(true, nil)
								code := uint32(0)
								if failRemote.Load() {
									_, _ = ch.Stderr().Write([]byte("permission denied on authorized_keys"))
									code = 1
								}
								_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
								return
							}
						}
					}(ch, cr)
				}
			}(c)
		}
	}()

	store, err := vault.Open(filepath.Join(t.TempDir(), "vault.gsv"), testNoopProtector{})
	if err != nil {
		t.Fatalf("vault.Open: %v", err)
	}
	defer store.Close()

	keyMeta, err := store.GenerateSSHKey("deploy-key-2", "ed25519", "", "unit-test")
	if err != nil {
		t.Fatalf("GenerateSSHKey: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	srvView, err := store.SaveServer(models.ServerSaveRequest{
		Name:           "HealthyDeploySrv",
		Host:           addr.IP.String(),
		Port:           addr.Port,
		Username:       "root",
		AuthType:       models.AuthPassword,
		Password:       "pw",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	m := NewManager(store, nil)

	// Empty/blank server list must error.
	if _, err := m.DeploySSHKeyToServers(context.Background(), keyMeta.ID, []string{" ", ""}, false); err == nil {
		t.Fatal("expected error when no valid serverIDs provided")
	}
	// Unknown keyID must error.
	if _, err := m.DeploySSHKeyToServers(context.Background(), "missing-key", []string{srvView.ID}, false); err == nil {
		t.Fatal("expected error for unknown keyID")
	}

	// Duplicate serverID + blank + nonexistent server in one batch with bindAsAuth=true.
	results, err := m.DeploySSHKeyToServers(context.Background(), keyMeta.ID, []string{srvView.ID, " " + srvView.ID + " ", "", "nonexistent-srv"}, true)
	if err != nil {
		t.Fatalf("DeploySSHKeyToServers failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 deduplicated results, got %d: %+v", len(results), results)
	}
	if !results[0].OK || results[0].ServerID != srvView.ID {
		t.Fatalf("expected healthy server deploy OK, got %+v", results[0])
	}
	if results[1].OK || results[1].ServerID != "nonexistent-srv" {
		t.Fatalf("expected missing server failure, got %+v", results[1])
	}
	if got := execCount.Load(); got != 1 {
		t.Fatalf("expected duplicate serverID to execute once, got %d", got)
	}
	// Server had password -> bindAsAuth must switch AuthType to AuthBoth and bind keyMeta.ID.
	updatedSrv, err := store.GetServerInternal(srvView.ID)
	if err != nil || updatedSrv.SSHKeyID != keyMeta.ID || updatedSrv.AuthType != models.AuthBoth {
		t.Fatalf("expected server bound to key with AuthBoth, got %+v err=%v", updatedSrv, err)
	}

	// Remote command failure must report error snippet without claiming OK.
	failRemote.Store(true)
	failResults, err := m.DeploySSHKeyToServers(context.Background(), keyMeta.ID, []string{srvView.ID}, false)
	if err != nil {
		t.Fatalf("DeploySSHKeyToServers remote fail returned top-level error: %v", err)
	}
	if len(failResults) != 1 || failResults[0].OK || !strings.Contains(failResults[0].Message, "permission denied") {
		t.Fatalf("expected remote error message in deploy result, got %+v", failResults)
	}

	// Pre-canceled context returns canceled result cleanly.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelRes, err := m.DeploySSHKeyToServers(cancelCtx, keyMeta.ID, []string{srvView.ID}, false)
	if err != nil {
		t.Fatalf("DeploySSHKeyToServers canceled returned top-level error: %v", err)
	}
	if len(cancelRes) != 1 || cancelRes[0].OK {
		t.Fatalf("expected failed result on canceled ctx, got %+v", cancelRes)
	}
}
