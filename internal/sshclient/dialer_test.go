package sshclient

import (
	"context"
	"strings"
	"testing"

	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
)

func TestDialServerViaHTTPProxyAndJumpHost(t *testing.T) {
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
