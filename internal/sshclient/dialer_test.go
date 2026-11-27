package sshclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
	"golang.org/x/crypto/ssh"
)

func useTempKnownHosts(t *testing.T) string {
	t.Helper()
	khPath := filepath.Join(t.TempDir(), ".ssh", "known_hosts")
	prev := knownHostsPathFunc
	knownHostsPathFunc = func() string { return khPath }
	t.Cleanup(func() { knownHostsPathFunc = prev })
	return khPath
}

func TestDialServerViaHTTPProxyAndJumpHost(t *testing.T) {
	khPath := useTempKnownHosts(t)
	targetKeyPEM, targetKeyInfo, err := sshkey.GenerateKeyPair("ed25519", "target", "")
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	targetPub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(targetKeyInfo.PublicKey))
	if err != nil {
		t.Fatalf("parse target pubkey: %v", err)
	}

	jumpHost, jumpPort, stopJump := startTestSSHServer(t, "jumpuser", "jumppass", nil)
	defer stopJump()

	targetHost, targetPort, stopTarget := startTestSSHServer(t, "appuser", "", targetPub)
	defer stopTarget()

	proxyHost, proxyPort, stopProxy := startTestHTTPConnectProxy(t, "puser", "ppass")
	defer stopProxy()

	resolver := &memResolver{
		servers: map[string]*models.ServerConfig{
			"jump-1": {
				ID:             "jump-1",
				Name:           "Bastion-HK",
				Host:           jumpHost,
				Port:           jumpPort,
				Username:       "jumpuser",
				AuthType:       models.AuthPassword,
				Password:       "jumppass",
				TimeoutSeconds: 5,
				Proxy: models.ProxyConfig{
					Type:     models.ProxyHTTP,
					Host:     proxyHost,
					Port:     proxyPort,
					Username: "puser",
					Password: "ppass",
				},
			},
		},
		keys: map[string]*models.SSHKeyRecord{
			"key-1": {
				ID:            "key-1",
				Name:          "TargetKey",
				PrivateKeyPEM: targetKeyPEM,
			},
		},
	}

	targetCfg := &models.ServerConfig{
		ID:             "target-1",
		Name:           "Prod-DB",
		Host:           targetHost,
		Port:           targetPort,
		Username:       "appuser",
		AuthType:       models.AuthKey,
		SSHKeyID:       "key-1",
		TimeoutSeconds: 5,
		JumpHost: models.JumpHostConfig{
			Mode:     models.JumpServer,
			ServerID: "jump-1",
		},
	}

	res, err := DialServer(context.Background(), targetCfg, resolver)
	if err != nil {
		t.Fatalf("DialServer through HTTP proxy + Jump host failed: %v", err)
	}
	defer res.Close()

	if !strings.Contains(res.ViaSummary, "HTTP(") || !strings.Contains(res.ViaSummary, "跳板(Bastion-HK)") {
		t.Fatalf("unexpected ViaSummary: %q", res.ViaSummary)
	}
	if len(res.JumpClients) != 1 {
		t.Fatalf("expected 1 jump client, got %d", len(res.JumpClients))
	}
	if CountKnownHosts(khPath) != 2 {
		t.Fatalf("expected TOFU to record both jump and target host keys, got %d", CountKnownHosts(khPath))
	}
}

func TestTOFUHostKeyVerificationAcceptsFirstAndRejectsMismatch(t *testing.T) {
	khPath := useTempKnownHosts(t)

	host, port, stop1 := startTestSSHServer(t, "u", "p", nil)
	cfg := &models.ServerConfig{
		ID: "s1", Name: "Node1", Host: host, Port: port,
		Username: "u", AuthType: models.AuthPassword, Password: "p", TimeoutSeconds: 5,
	}

	// 1. First connection records host key via TOFU.
	res1, err := DialServer(context.Background(), cfg, &memResolver{})
	if err != nil {
		stop1()
		t.Fatalf("first TOFU dial failed: %v", err)
	}
	res1.Close()

	// 2. Second connection to same server succeeds.
	res2, err := DialServer(context.Background(), cfg, &memResolver{})
	if err != nil {
		stop1()
		t.Fatalf("second dial to known host failed: %v", err)
	}
	res2.Close()
	stop1()

	// 3. Simulate server reinstall with a different host key at the same address.
	_, newKeyInfo, err := sshkey.GenerateKeyPair("ed25519", "reinstalled", "")
	if err != nil {
		t.Fatalf("gen new key: %v", err)
	}
	newPubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(newKeyInfo.PublicKey))
	if err != nil {
		t.Fatalf("parse new pubkey: %v", err)
	}
	cb := tofuHostKeyCallback(khPath)
	hostPort := net.JoinHostPort(host, strconv.Itoa(port))
	err = cb(hostPort, nil, newPubKey)
	if err == nil || !strings.Contains(err.Error(), "SSH 缓存") {
		t.Fatalf("expected Chinese mismatch error mentioning SSH 缓存, got: %v", err)
	}

	// 4. Clearing the host from known_hosts allows TOFU to accept the new key.
	if _, err := ClearKnownHostsHost(khPath, host, port); err != nil {
		t.Fatalf("ClearKnownHostsHost failed: %v", err)
	}
	if err := cb(hostPort, nil, newPubKey); err != nil {
		t.Fatalf("expected TOFU accept after clearing cache, got: %v", err)
	}
}

func TestDialServerDetectsJumpCycle(t *testing.T) {
	resolver := &memResolver{
		servers: map[string]*models.ServerConfig{},
		keys:    map[string]*models.SSHKeyRecord{},
	}
	resolver.servers["a"] = &models.ServerConfig{
		ID: "a", Name: "A", Host: "127.0.0.1", Port: 22, Username: "root", AuthType: models.AuthPassword, Password: "x",
		JumpHost: models.JumpHostConfig{Mode: models.JumpServer, ServerID: "b"},
	}
	resolver.servers["b"] = &models.ServerConfig{
		ID: "b", Name: "B", Host: "127.0.0.1", Port: 22, Username: "root", AuthType: models.AuthPassword, Password: "x",
		JumpHost: models.JumpHostConfig{Mode: models.JumpServer, ServerID: "a"},
	}

	_, err := DialServer(context.Background(), resolver.servers["a"], resolver)
	if err == nil || !strings.Contains(err.Error(), "循环跳板机") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

func startMultiKeySSHServer(t *testing.T, signers []ssh.Signer, user, pass string) (string, int, func()) {
	t.Helper()
	scfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(p) == pass {
				return nil, nil
			}
			return nil, errors.New("invalid password")
		},
	}
	for _, s := range signers {
		scfg.AddHostKey(s)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
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
				for ch := range chs {
					_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
				}
			}(c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() { _ = ln.Close() }
}

func TestTOFUHostKeyAlgorithmsOpenSSHCompat(t *testing.T) {
	khPath := useTempKnownHosts(t)

	// Generate Ed25519, ECDSA-P256, and RSA-2048 host keys for a multi-key server.
	edPEM, _, err := sshkey.GenerateKeyPair("ed25519", "ed-host", "")
	if err != nil {
		t.Fatalf("gen ed25519: %v", err)
	}
	edSigner, err := ssh.ParsePrivateKey([]byte(edPEM))
	if err != nil {
		t.Fatalf("parse ed25519: %v", err)
	}
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen ecdsa: %v", err)
	}
	ecSigner, err := ssh.NewSignerFromKey(ecPriv)
	if err != nil {
		t.Fatalf("signer ecdsa: %v", err)
	}
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	rsaSigner, err := ssh.NewSignerFromKey(rsaPriv)
	if err != nil {
		t.Fatalf("signer rsa: %v", err)
	}

	// Unknown host prefers ssh-ed25519 first (matching OpenSSH).
	unknownAlgos := preferredHostKeyAlgorithms(khPath, "10.0.0.99:22")
	if len(unknownAlgos) == 0 || unknownAlgos[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("expected unknown host to prefer %s first, got %v", ssh.KeyAlgoED25519, unknownAlgos)
	}

	host, port, stop := startMultiKeySSHServer(t, []ssh.Signer{ecSigner, rsaSigner, edSigner}, "u", "p")
	defer stop()
	hostToken := fmt.Sprintf("[%s]:%d", host, port)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	cfg := &models.ServerConfig{
		ID: "mk", Name: "MultiKey", Host: host, Port: port,
		Username: "u", AuthType: models.AuthPassword, Password: "p", TimeoutSeconds: 5,
	}

	// 1. Pre-populate known_hosts as OpenSSH would (ssh-ed25519 only).
	// Without HostKeyAlgorithms fix, x/crypto/ssh would negotiate ECDSA first and falsely reject.
	if err := os.MkdirAll(filepath.Dir(khPath), 0o700); err != nil {
		t.Fatal(err)
	}
	edLine := hostToken + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(edSigner.PublicKey()))) + "\n"
	if err := os.WriteFile(khPath, []byte(edLine), 0o600); err != nil {
		t.Fatal(err)
	}
	res1, err := DialServer(context.Background(), cfg, &memResolver{})
	if err != nil {
		t.Fatalf("expected dial to OpenSSH-recorded ed25519 host to succeed, got: %v", err)
	}
	res1.Close()

	// 2. Pre-populate known_hosts with ECDSA only (existing GravitySSH users).
	ecLine := hostToken + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(ecSigner.PublicKey()))) + "\n"
	if err := os.WriteFile(khPath, []byte(ecLine), 0o600); err != nil {
		t.Fatal(err)
	}
	if algos := preferredHostKeyAlgorithms(khPath, addr); len(algos) == 0 || algos[0] != ssh.KeyAlgoECDSA256 {
		t.Fatalf("expected ECDSA prioritized first, got %v", algos)
	}
	res2, err := DialServer(context.Background(), cfg, &memResolver{})
	if err != nil {
		t.Fatalf("expected dial to ECDSA-recorded host to succeed, got: %v", err)
	}
	res2.Close()

	// 3. Pre-populate known_hosts with ssh-rsa only -> must expand to rsa-sha2-512, rsa-sha2-256, ssh-rsa.
	rsaLine := hostToken + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(rsaSigner.PublicKey()))) + "\n"
	if err := os.WriteFile(khPath, []byte(rsaLine), 0o600); err != nil {
		t.Fatal(err)
	}
	rsaAlgos := preferredHostKeyAlgorithms(khPath, addr)
	wantPrefix := []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	if len(rsaAlgos) < 3 || !slices.Equal(rsaAlgos[:3], wantPrefix) {
		t.Fatalf("expected ssh-rsa expansion %v at head, got %v", wantPrefix, rsaAlgos)
	}
	res3, err := DialServer(context.Background(), cfg, &memResolver{})
	if err != nil {
		t.Fatalf("expected dial to ssh-rsa-recorded host to succeed, got: %v", err)
	}
	res3.Close()

	// 4. Revoked key line must NOT be prioritized over valid fallback.
	revokedLine := "@revoked " + ecLine
	if err := os.WriteFile(khPath, []byte(revokedLine), 0o600); err != nil {
		t.Fatal(err)
	}
	if algos := preferredHostKeyAlgorithms(khPath, addr); len(algos) == 0 || algos[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("expected revoked key not to override default Ed25519 priority, got %v", algos)
	}
}
