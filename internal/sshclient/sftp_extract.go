package sshclient

import (
	"context"
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

	out, runErr := runSSHExec(context.Background(), client, cmd, 180*time.Second, true)
	m.invalidateDirTreeCache(sessionID, dir)

	if runErr != nil {
		return "", formatExecError(fmt.Sprintf("解压 %s 失败", base), out, runErr, 240)
	}
	return fmt.Sprintf("已成功解压 %s", base), nil
}

func (m *Manager) getSSHClient(sessionID string) (*ssh.Client, error) {
	sess := m.getSession(sessionID)
	if sess == nil {
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
	qBase := shellQuote(safeRelativeName(base))
	lower := strings.ToLower(base)

	switch {
	case strings.HasSuffix(lower, ".zip"):
		install := autoInstallSnippet("unzip", "unzip", "unzip", "unzip", "unzip", "unzip")
		return fmt.Sprintf("cd -- %s && { if ! command -v unzip >/dev/null 2>&1; then %s; fi && unzip -oq %s -d .; }", qDir, install, qBase), nil
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return fmt.Sprintf("cd -- %s && tar -xzf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return fmt.Sprintf("cd -- %s && tar -xjf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		return fmt.Sprintf("cd -- %s && tar -xJf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".tar"):
		return fmt.Sprintf("cd -- %s && tar -xf %s", qDir, qBase), nil
	case strings.HasSuffix(lower, ".7z"):
		install := autoInstallSnippet("p7zip-full", "p7zip", "p7zip", "p7zip", "7zip", "p7zip")
		return fmt.Sprintf("cd -- %s && { if ! command -v 7z >/dev/null 2>&1; then %s; fi && 7z x -y -bso0 -- %s; }", qDir, install, qBase), nil
	case strings.HasSuffix(lower, ".gz"):
		return fmt.Sprintf("cd -- %s && gunzip -kf -- %s", qDir, qBase), nil
	default:
		return "", fmt.Errorf("暂不支持解压该格式: %s", base)
	}
}

// safeRelativeName prefixes "./" when a relative filename starts with "-" or contains ":"
// so CLI tools (tar, zip, unzip, gunzip, 7z) never interpret it as an option flag or
// as a remote rsh/ssh host specification (GNU tar treats host:file as remote without '/').
func safeRelativeName(name string) string {
	if strings.HasPrefix(name, "-") || strings.Contains(name, ":") {
		return "./" + name
	}
	return name
}

func autoInstallSnippet(aptPkg, apkPkg, dnfPkg, yumPkg, pacmanPkg, zypperPkg string) string {
	return fmt.Sprintf(
		`SUDO=""; [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1 && SUDO="sudo -n"; `+
			`if command -v apt-get >/dev/null 2>&1; then DEBIAN_FRONTEND=noninteractive $SUDO env DEBIAN_FRONTEND=noninteractive apt-get update -qq && DEBIAN_FRONTEND=noninteractive $SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq %s; `+
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
