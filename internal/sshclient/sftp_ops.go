package sshclient

import (
	"fmt"
	"os"
	"path"
	"strings"

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
	cleanOld, cleanNew := path.Clean(oldPath), path.Clean(newPath)
	if err := sc.Rename(cleanOld, cleanNew); err != nil {
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
	clean := path.Clean(remotePath)
	if clean == "/" || clean == "." {
		return fmt.Errorf("禁止删除根目录")
	}
	if err := removeRemoteRecursive(sc, clean); err != nil {
		return err
	}
	m.invalidateDirCache(sessionID, path.Dir(clean))
	return nil
}

func removeRemoteRecursive(sc *sftp.Client, target string) error {
	info, err := sc.Lstat(target)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		items, err := sc.ReadDir(target)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := removeRemoteRecursive(sc, path.Join(target, item.Name())); err != nil {
				return err
			}
		}
		return sc.RemoveDirectory(target)
	}
	return sc.Remove(target)
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
