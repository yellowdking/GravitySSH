package sshclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

// DeploySSHKeyToServers installs the selected key's public key into ~/.ssh/authorized_keys
// on each target server idempotently, and optionally switches those servers to key auth.
func (m *Manager) DeploySSHKeyToServers(
	ctx context.Context,
	keyID string,
	serverIDs []string,
	bindAsAuth bool,
) ([]models.KeyDeployResult, error) {
	keyRec, err := m.store.GetSSHKeyInternal(keyID)
	if err != nil {
		return nil, err
	}
	pubKey := strings.TrimSpace(keyRec.PublicKey)
	if pubKey == "" {
		return nil, errors.New("所选密钥缺少有效公钥内容")
	}

	uniqueIDs := make([]string, 0, len(serverIDs))
	seen := make(map[string]bool)
	for _, id := range serverIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	if len(uniqueIDs) == 0 {
		return nil, errors.New("请至少选择一台目标服务器")
	}

	results := make([]models.KeyDeployResult, len(uniqueIDs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for i, sid := range uniqueIDs {
		wg.Add(1)
		go func(idx int, serverID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = m.deployKeyToOneServer(ctx, serverID, keyID, pubKey, bindAsAuth)
		}(i, sid)
	}
	wg.Wait()
	return results, nil
}

func (m *Manager) deployKeyToOneServer(
	ctx context.Context,
	serverID, keyID, pubKey string,
	bindAsAuth bool,
) models.KeyDeployResult {
	srv, err := m.store.GetServerInternal(serverID)
	if err != nil {
		return models.KeyDeployResult{
			ServerID:   serverID,
			ServerName: serverID,
			OK:         false,
			Message:    "服务器配置不存在",
		}
	}

	res := models.KeyDeployResult{
		ServerID:   srv.ID,
		ServerName: srv.Name,
	}

	dialCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	dialRes, err := DialServer(dialCtx, srv, m.store)
	if err != nil {
		res.Message = fmt.Sprintf("连接失败: %v", err)
		return res
	}
	defer dialRes.Close()

	sshSess, err := dialRes.Client.NewSession()
	if err != nil {
		res.Message = fmt.Sprintf("创建 SSH 会话失败: %v", err)
		return res
	}
	defer sshSess.Close()

	qPub := shellQuote(pubKey)
	installCmd := fmt.Sprintf(
		"mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && (grep -qxF %s ~/.ssh/authorized_keys || printf '%%s\\n' %s >> ~/.ssh/authorized_keys)",
		qPub, qPub,
	)

	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-dialCtx.Done():
			_ = sshSess.Close()
		}
	}()

	out, runErr := sshSess.CombinedOutput(installCmd)
	close(done)
	if runErr != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 180 {
			snippet = snippet[len(snippet)-180:]
		}
		if snippet != "" {
			res.Message = fmt.Sprintf("写入公钥失败: %s", snippet)
		} else {
			res.Message = fmt.Sprintf("写入公钥失败: %v", runErr)
		}
		return res
	}

	if bindAsAuth {
		if bindErr := m.store.BindSSHKeyToServer(srv.ID, keyID); bindErr != nil {
			res.OK = true
			res.Message = fmt.Sprintf("公钥已部署，但更新登录方式失败: %v", bindErr)
			return res
		}
		res.OK = true
		res.Message = "公钥已部署并自动切换为密钥登录"
		return res
	}

	res.OK = true
	res.Message = "公钥已部署到 authorized_keys"
	return res
}
