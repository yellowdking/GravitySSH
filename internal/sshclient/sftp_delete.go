package sshclient

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pkg/sftp"
)

const maxDeleteConcurrency = 8

func (m *Manager) SFTPDelete(sessionID, remotePath string) error {
	if strings.TrimSpace(remotePath) == "" || strings.ContainsRune(remotePath, 0) {
		return fmt.Errorf("无效的远程路径")
	}
	clean := path.Clean(remotePath)
	if clean == "/" || clean == "." || clean == ".." {
		return fmt.Errorf("禁止删除根目录")
	}

	var (
		sawTarget bool
		sshTried  bool
	)
	err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		info, lerr := sc.Lstat(clean)
		if lerr != nil {
			if errors.Is(lerr, os.ErrNotExist) {
				if sawTarget {
					return nil
				}
				return lerr
			}
			if isSFTPConnError(lerr) {
				return lerr
			}
			if m.cachedSSHSameFS(sessionID) {
				if fbErr := m.deleteViaSSH(sessionID, clean); fbErr == nil {
					return nil
				}
			}
			return lerr
		}
		sawTarget = true

		isRealDir := info.IsDir() && info.Mode()&os.ModeSymlink == 0
		if !sshTried && isRealDir && m.sshMatchesSFTPRoot(sessionID, sc) {
			sshTried = true
			if fbErr := m.deleteViaSSH(sessionID, clean); fbErr == nil {
				return nil
			} else if errors.Is(fbErr, errSSHDeleteNotFound) {
				if _, chkErr := sc.Lstat(clean); errors.Is(chkErr, os.ErrNotExist) {
					return nil
				}
			}
		}

		rmErr := removeRemoteNodeWithInfo(sc, clean, info, false)
		if rmErr == nil {
			return nil
		}
		if isSFTPConnError(rmErr) {
			return rmErr
		}
		if _, postErr := sc.Lstat(clean); errors.Is(postErr, os.ErrNotExist) {
			return nil
		}
		if !sshTried && m.sshMatchesSFTPRoot(sessionID, sc) {
			fbErr := m.deleteViaSSH(sessionID, clean)
			if fbErr == nil {
				return nil
			}
			if errors.Is(fbErr, errSSHDeleteNotFound) {
				if _, postErr := sc.Lstat(clean); errors.Is(postErr, os.ErrNotExist) {
					return nil
				}
			}
			return fmt.Errorf("%w (SSH 回退失败: %v)", rmErr, fbErr)
		}
		return rmErr
	})
	if err != nil {
		if !sawTarget && !errors.Is(err, os.ErrNotExist) && m.cachedSSHSameFS(sessionID) {
			if fbErr := m.deleteViaSSH(sessionID, clean); fbErr == nil {
				m.invalidateDirTreeCache(sessionID, clean)
				return nil
			}
		}
		return err
	}

	m.invalidateDirTreeCache(sessionID, clean)
	return nil
}

func removeRemoteNodeWithInfo(sc *sftp.Client, target string, info os.FileInfo, ignoreNotExist bool) error {
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		if info.Mode().Perm()&0o200 == 0 {
			_ = sc.Chmod(target, info.Mode().Perm()|0o700)
		}
		items, err := sc.ReadDir(target)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				_ = sc.Chmod(target, 0o700)
				items, err = sc.ReadDir(target)
			}
			if err != nil {
				if ignoreNotExist && errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
		}

		var files, dirs []os.FileInfo
		for _, item := range items {
			name := item.Name()
			if name == "" || name == "." || name == ".." {
				continue
			}
			if item.IsDir() && item.Mode()&os.ModeSymlink == 0 {
				dirs = append(dirs, item)
			} else {
				files = append(files, item)
			}
		}

		if len(files) > 0 {
			if err := removeFilesParallel(sc, target, files); err != nil {
				return err
			}
		}
		for _, d := range dirs {
			if err := removeRemoteNodeWithInfo(sc, path.Join(target, d.Name()), d, true); err != nil {
				return err
			}
		}
		if err := sc.RemoveDirectory(target); err != nil {
			if _, lerr := sc.Lstat(target); errors.Is(lerr, os.ErrNotExist) {
				return nil
			}
			return err
		}
		return nil
	}

	if err := sc.Remove(target); err != nil {
		if ignoreNotExist && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if _, lerr := sc.Lstat(target); errors.Is(lerr, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

func removeFilesParallel(sc *sftp.Client, parent string, files []os.FileInfo) error {
	var (
		errMu    sync.Mutex
		firstErr error
		aborted  atomic.Bool
	)
	recordErr := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}
	runBounded(len(files), maxDeleteConcurrency, aborted.Load, func(idx int) {
		target := path.Join(parent, files[idx].Name())
		if err := sc.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			if isSFTPConnError(err) {
				aborted.Store(true)
				recordErr(err)
				return
			}
			if _, lerr := sc.Lstat(target); !errors.Is(lerr, os.ErrNotExist) {
				if isSFTPConnError(lerr) {
					aborted.Store(true)
				}
				recordErr(err)
			}
		}
	})
	return firstErr
}
