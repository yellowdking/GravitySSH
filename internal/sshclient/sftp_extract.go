package sshclient

import (
	"fmt"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SFTPExtractArchive extracts an archive in its own directory on the remote server,
// automatically installing unzip / p7zip if missing on the remote host.
func (m *Manager) SFTPExtractArchive(sessionID, remotePath string) (string, error) {
	client, err := m.getSSHClient(sessionID)
	if err != nil {
		return "", err
	}
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return "", fmt.Errorf("无效的压缩包路径")
	}

	dir := path.Dir(clean)
	base := path.Base(clean)
	cmd, err := buildExtractCommand(dir, base)
	if err != nil {
		return "", err
	}

	sshSess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建远程执行会话失败: %w", err)
	}
	defer sshSess.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(180 * time.Second):
			_ = sshSess.Close()
		}
	}()

	out, runErr := sshSess.CombinedOutput(cmd)
	close(done)
	m.invalidateDirCache(sessionID, dir)

	if runErr != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 240 {
			snippet = snippet[len(snippet)-240:]
		}
		if snippet != "" {
			return "", fmt.Errorf("解压 %s 失败: %s", base, snippet)
		}
		return "", fmt.Errorf("解压 %s 失败: %v", base, runErr)
	}
	return fmt.Sprintf("已成功解压 %s", base), nil
}

func (m *Manager) getSSHClient(sessionID string) (*ssh.Client, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("会话不存在或已关闭: %s", sessionID)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.dialRes == nil || sess.dialRes.Client == nil {
		return nil, fmt.Errorf("SSH 连接未就绪")
	}
	return sess.dialRes.Client, nil
}

func buildExtractCommand(dir, base string) (string, error) {
	qDir := shellQuote(dir)
	qBase := shellQuote(base)
	lower := strings.ToLower(base)

	switch {
	case strings.HasSuffix(lower, ".zip"):
		install := autoInstallSnippet("unzip", "unzip", "unzip", "unzip", "unzip", "unzip")
		return fmt.Sprintf("cd %s && { if ! command -v unzip >/dev/null 2>&1; then %s; fi; unzip -o %s -d .; }", qDir, install, qBase), nil
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return fmt.Sprintf("cd %s && tar -xzf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return fmt.Sprintf("cd %s && tar -xjf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		return fmt.Sprintf("cd %s && tar -xJf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".tar"):
		return fmt.Sprintf("cd %s && tar -xf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".7z"):
		install := autoInstallSnippet("p7zip-full", "p7zip", "p7zip", "p7zip", "7zip", "p7zip")
		return fmt.Sprintf("cd %s && { if ! command -v 7z >/dev/null 2>&1; then %s; fi; 7z x -y %s; }", qDir, install, qBase), nil
	case strings.HasSuffix(lower, ".gz"):
		return fmt.Sprintf("cd %s && gunzip -kf %s", qDir, qBase), nil
	default:
		return "", fmt.Errorf("暂不支持解压该格式: %s", base)
	}
}

func autoInstallSnippet(aptPkg, apkPkg, dnfPkg, yumPkg, pacmanPkg, zypperPkg string) string {
	return fmt.Sprintf(
		`SUDO=""; [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1 && SUDO="sudo"; `+
			`if command -v apt-get >/dev/null 2>&1; then $SUDO apt-get update -qq && $SUDO apt-get install -y -qq %s; `+
			`elif command -v apk >/dev/null 2>&1; then $SUDO apk add --no-cache %s; `+
			`elif command -v dnf >/dev/null 2>&1; then $SUDO dnf install -y %s; `+
			`elif command -v yum >/dev/null 2>&1; then $SUDO yum install -y %s; `+
			`elif command -v pacman >/dev/null 2>&1; then $SUDO pacman -Sy --noconfirm %s; `+
			`elif command -v zypper >/dev/null 2>&1; then $SUDO zypper --non-interactive install %s; `+
			`else echo "未找到可用包管理器，无法自动安装解压工具" >&2; exit 127; fi`,
		aptPkg, apkPkg, dnfPkg, yumPkg, pacmanPkg, zypperPkg,
	)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
