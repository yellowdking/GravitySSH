package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"golang.org/x/crypto/ssh"
)

var (
	keyDeployDialTimeout = 20 * time.Second
	keyDeployExecTimeout = 20 * time.Second
)

// sanitizeAuthorizedPublicKey strictly validates a single OpenSSH public key without
// authorized_keys options or control characters, and rebuilds a clean "<type> <base64>" line.
func sanitizeAuthorizedPublicKey(raw string) (string, error) {
	if strings.ContainsAny(raw, "\x00\r\n") {
		return "", errors.New("公钥格式无效：不支持包含空字符或换行符的公钥")
	}
	pubKey := strings.TrimSpace(raw)
	if pubKey == "" {
		return "", errors.New("所选密钥缺少有效公钥内容")
	}
	parsedKey, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(pubKey))
	if err != nil {
		return "", fmt.Errorf("公钥格式无效：%w", err)
	}
	if len(options) > 0 {
		return "", errors.New("公钥格式无效：不允许包含 authorized_keys 选项前缀")
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return "", errors.New("公钥格式无效：仅支持单个标准公钥")
	}
	clean := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(parsedKey)))
	if clean == "" || strings.ContainsAny(clean, "\x00\r\n") {
		return "", errors.New("公钥格式无效")
	}
	return clean, nil
}

func buildKeyDeployCommand(cleanPubKey string) string {
	script := `umask 077
d=~/.ssh
ak="$d/authorized_keys"
pub="$1"
mkdir -p -- "$d" && chmod 700 -- "$d" || exit 1
lockdir=""
if command -v flock >/dev/null 2>&1; then
  exec 9>"$d/.authorized_keys.gssh.lock"
  i=0
  while ! flock -n 9 2>/dev/null; do
    [ "$i" -ge 100 ] && { echo "无法获取 authorized_keys 写锁" >&2; exit 1; }
    if sleep 0.1 2>/dev/null; then i=$((i + 1)); else i=$((i + 10)); fi
  done
else
  lockdir="$d/.authorized_keys.gssh.lockdir"
  i=0
  while ! mkdir -- "$lockdir" 2>/dev/null; do
    [ "$i" -ge 100 ] && { echo "无法获取 authorized_keys 写锁" >&2; exit 1; }
    if sleep 0.1 2>/dev/null; then i=$((i + 1)); else i=$((i + 10)); fi
  done
fi
tmp="$d/.authorized_keys.gssh.tmp.$$"
trap 'rm -f -- "$tmp"; [ -n "$lockdir" ] && rmdir -- "$lockdir" 2>/dev/null || true' EXIT INT TERM HUP
if [ -f "$ak" ] && grep -qxF -- "$pub" "$ak" 2>/dev/null; then
  chmod 600 -- "$ak"
  exit 0
fi
if [ -f "$ak" ]; then
  cat -- "$ak" > "$tmp" || exit 1
  if [ -s "$tmp" ] && [ -n "$(tail -c 1 "$tmp" 2>/dev/null)" ]; then
    printf '\n' >> "$tmp" || exit 1
  fi
else
  : > "$tmp" || exit 1
fi
printf '%s\n' "$pub" >> "$tmp" || exit 1
chmod 600 -- "$tmp" && mv -f -- "$tmp" "$ak"`
	return fmt.Sprintf("sh -c %s _ %s", shellQuote(script), shellQuote(cleanPubKey))
}

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
	pubKey, err := sanitizeAuthorizedPublicKey(keyRec.PublicKey)
	if err != nil {
		return nil, err
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
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = models.KeyDeployResult{
					ServerID:   serverID,
					ServerName: serverID,
					OK:         false,
					Message:    "部署已取消",
				}
				return
			}
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

	dialTimeout := keyDeployDialTimeout
	if srv.TimeoutSeconds > 0 {
		if t := time.Duration(srv.TimeoutSeconds) * time.Second; t > 0 {
			dialTimeout = t
		}
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	dialRes, err := DialServer(dialCtx, srv, m.store)
	cancelDial()
	if err != nil {
		res.Message = fmt.Sprintf("连接失败: %v", err)
		return res
	}
	defer dialRes.Close()

	execCtx, cancelExec := context.WithTimeout(ctx, keyDeployExecTimeout)
	defer cancelExec()
	stopClose := context.AfterFunc(execCtx, dialRes.Close)
	defer stopClose()

	installCmd := buildKeyDeployCommand(pubKey)
	out, runErr := runSSHExec(execCtx, dialRes.Client, installCmd, 0, true)
	if runErr != nil {
		res.Message = formatExecError("写入公钥失败", out, runErr, 180).Error()
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
