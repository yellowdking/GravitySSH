package sshclient

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

func (m *Manager) SFTPMkdir(sessionID, remotePath string) error {
	clean := path.Clean(remotePath)
	if err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		return sc.MkdirAll(clean)
	}); err != nil {
		return err
	}
	m.invalidateDirCache(sessionID, path.Dir(clean))
	return nil
}

func (m *Manager) SFTPRename(sessionID, oldPath, newPath string) error {
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" ||
		strings.ContainsRune(oldPath, 0) || strings.ContainsRune(newPath, 0) {
		return fmt.Errorf("无效的重命名路径")
	}
	cleanOld, cleanNew := path.Clean(oldPath), path.Clean(newPath)
	if cleanOld == "/" || cleanOld == "." || cleanOld == ".." ||
		cleanNew == "/" || cleanNew == "." || cleanNew == ".." {
		return fmt.Errorf("无效的重命名路径")
	}
	if cleanOld == cleanNew {
		return nil
	}

	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}
	_, preErr := sc.Lstat(cleanOld)
	if isSFTPConnError(preErr) {
		m.invalidateSFTP(sessionID, sc)
		sc, err = m.getSFTP(sessionID)
		if err != nil {
			return preErr
		}
		_, preErr = sc.Lstat(cleanOld)
	}
	hadOld := preErr == nil

	if err := renameRemote(sc, cleanOld, cleanNew); err != nil {
		if isSFTPConnError(err) {
			m.invalidateSFTP(sessionID, sc)
			sc2, err2 := m.getSFTP(sessionID)
			if err2 != nil {
				return err
			}
			if hadOld {
				_, oErr := sc2.Lstat(cleanOld)
				_, nErr := sc2.Lstat(cleanNew)
				if errors.Is(oErr, os.ErrNotExist) && nErr == nil {
					m.invalidateDirTreeCache(sessionID, cleanOld)
					m.invalidateDirCache(sessionID, path.Dir(cleanNew))
					return nil
				}
			}
			if rErr := renameRemote(sc2, cleanOld, cleanNew); rErr != nil {
				return rErr
			}
		} else {
			return err
		}
	}
	m.invalidateDirTreeCache(sessionID, cleanOld)
	m.invalidateDirCache(sessionID, path.Dir(cleanNew))
	return nil
}

// SFTPAlignPermissions aligns remotePath's permissions (and best-effort uid/gid)
// to match sibling files/directories in its parent directory.
func (m *Manager) SFTPAlignPermissions(sessionID, remotePath string) (string, error) {
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return "", fmt.Errorf("无效的目标路径")
	}

	var formatted string
	err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		targetInfo, err := sc.Lstat(clean)
		if err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("读取目标属性失败: %w", err)
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
			if s.Name() == baseName || s.Mode()&os.ModeSymlink != 0 || s.IsDir() != isDir {
				continue
			}
			if perm := s.Mode().Perm(); perm != 0 {
				counts[perm]++
			}
		}

		targetMode := pickDominantMode(counts, fallback)
		if len(counts) == 0 && isDir && parentErr == nil && parentInfo.Mode().Perm() != 0 {
			targetMode = parentInfo.Mode().Perm()
		}

		if err := sc.Chmod(clean, targetMode); err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("修改权限失败: %w", err)
		}
		if parentErr == nil {
			if st, ok := parentInfo.Sys().(*sftp.FileStat); ok {
				_ = sc.Chown(clean, int(st.UID), int(st.GID))
			}
		}
		m.invalidateDirCache(sessionID, parentDir)
		formatted = fmt.Sprintf("%04o", targetMode)
		return nil
	})
	return formatted, err
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
