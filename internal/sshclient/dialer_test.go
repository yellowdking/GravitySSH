package sshclient

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
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

	jumpHost, jumpPort, stopJump := startTestSSHServer(t, "jumpuser", "jumppass", nil)
	defer stopJump()

	targetHost, targetPort, stopTarget := startTestSSHServer(t, "appuser", "", targetKeyInfo.Signer.PublicKey())
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
	cb := tofuHostKeyCallback(khPath)
	hostPort := net.JoinHostPort(host, strconv.Itoa(port))
	err = cb(hostPort, nil, newKeyInfo.Signer.PublicKey())
	if err == nil || !strings.Contains(err.Error(), "SSH 缓存") {
		t.Fatalf("expected Chinese mismatch error mentioning SSH 缓存, got: %v", err)
	}

	// 4. Clearing the host from known_hosts allows TOFU to accept the new key.
	if _, err := ClearKnownHostsHost(khPath, host, port); err != nil {
		t.Fatalf("ClearKnownHostsHost failed: %v", err)
	}
	if err := cb(hostPort, nil, newKeyInfo.Signer.PublicKey()); err != nil {
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
