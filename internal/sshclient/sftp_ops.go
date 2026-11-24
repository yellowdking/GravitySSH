package sshclient

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

func (m *Manager) SFTPMkdir(sessionID, remotePath string) error {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}
	clean := path.Clean(remotePath)
	if err := sc.MkdirAll(clean); err != nil {
		return err
	}
	m.invalidateDirCache(sessionID, path.Dir(clean))
	return nil
}

func (m *Manager) SFTPRename(sessionID, oldPath, newPath string) error {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" ||
		strings.ContainsRune(oldPath, 0) || strings.ContainsRune(newPath, 0) {
		return fmt.Errorf("无效的重命名路径")
	}
	cleanOld, cleanNew := path.Clean(oldPath), path.Clean(newPath)
	if cleanOld == "/" || cleanOld == "." || cleanOld == ".." ||
		cleanNew == "/" || cleanNew == "." || cleanNew == ".." {
		return fmt.Errorf("无效的重命名路径")
	}
	if err := renameRemote(sc, cleanOld, cleanNew); err != nil {
		return err
	}
	m.invalidateDirCache(sessionID, path.Dir(cleanOld))
	m.invalidateDirCache(sessionID, path.Dir(cleanNew))
	return nil
}

func (m *Manager) SFTPDelete(sessionID, remotePath string) error {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(remotePath) == "" || strings.ContainsRune(remotePath, 0) {
		return fmt.Errorf("无效的远程路径")
	}
	clean := path.Clean(remotePath)
	if clean == "/" || clean == "." || clean == ".." {
		return fmt.Errorf("禁止删除根目录")
	}
	_, preErr := sc.Lstat(clean)
	hadTarget := preErr == nil

	if err := removeRemoteRecursive(sc, clean); err != nil {
		if hadTarget {
			if _, postErr := sc.Lstat(clean); errors.Is(postErr, os.ErrNotExist) {
				m.invalidateDirCache(sessionID, path.Dir(clean))
				return nil
			}
		}
		if fbErr := m.deleteViaSSHFallback(sessionID, clean); fbErr != nil {
			return err
		}
	}
	m.invalidateDirCache(sessionID, path.Dir(clean))
	return nil
}

func (m *Manager) deleteViaSSHFallback(sessionID, clean string) error {
	if !strings.HasPrefix(clean, "/") || clean == "/" || clean == "." || clean == ".." {
		return fmt.Errorf("skip unsafe fallback path")
	}
	client, err := m.getSSHClient(sessionID)
	if err != nil {
		return err
	}
	sshSess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sshSess.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(60 * time.Second):
			_ = sshSess.Close()
		}
	}()

	q := shellQuote(clean)
	cmd := fmt.Sprintf("if [ -e %s ] || [ -L %s ]; then rm -rf -- %s && [ ! -e %s ] && [ ! -L %s ]; else exit 44; fi", q, q, q, q, q)
	out, runErr := sshSess.CombinedOutput(cmd)
	close(done)
	if runErr != nil {
		return fmt.Errorf("ssh rm fallback: %w (%s)", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// SFTPAlignPermissions aligns remotePath's permissions (and best-effort uid/gid)
// to match sibling files/directories in its parent directory.
func (m *Manager) SFTPAlignPermissions(sessionID, remotePath string) (string, error) {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return "", err
	}
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return "", fmt.Errorf("无效的目标路径")
	}

	targetInfo, err := sc.Lstat(clean)
	if err != nil {
		return "", fmt.Errorf("读取目标属性失败: %w", err)
	}
	isDir := targetInfo.IsDir()
	parentDir := path.Dir(clean)
	baseName := path.Base(clean)

	parentInfo, parentErr := sc.Stat(parentDir)
	siblings, _ := sc.ReadDir(parentDir)

	fallback := os.FileMode(0o644)
	if isDir {
		fallback = os.FileMode(0o755)
	}

	counts := make(map[os.FileMode]int)
	for _, s := range siblings {
		if s.Name() == baseName || s.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if s.IsDir() != isDir {
			continue
		}
		perm := s.Mode().Perm()
		if perm != 0 {
			counts[perm]++
		}
	}

	targetMode := pickDominantMode(counts, fallback)
	if len(counts) == 0 && isDir && parentErr == nil && parentInfo.Mode().Perm() != 0 {
		targetMode = parentInfo.Mode().Perm()
	}

	if err := sc.Chmod(clean, targetMode); err != nil {
		return "", fmt.Errorf("修改权限失败: %w", err)
	}
	if parentErr == nil {
		if st, ok := parentInfo.Sys().(*sftp.FileStat); ok {
			_ = sc.Chown(clean, int(st.UID), int(st.GID))
		}
	}

	m.invalidateDirCache(sessionID, parentDir)
	return fmt.Sprintf("%04o", targetMode), nil
}

func pickDominantMode(counts map[os.FileMode]int, fallback os.FileMode) os.FileMode {
	if len(counts) == 0 {
		return fallback
	}
	bestMode := fallback
	bestCount := -1
	for mode, cnt := range counts {
		if cnt > bestCount || (cnt == bestCount && (mode == fallback || (bestMode != fallback && mode < bestMode))) {
			bestMode = mode
			bestCount = cnt
		}
	}
	return bestMode
}
