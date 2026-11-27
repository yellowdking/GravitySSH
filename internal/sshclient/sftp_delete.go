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

	sc, err := m.getSFTP(sessionID)
	if err != nil {
		if m.cachedSSHSameFS(sessionID) {
			if fbErr := m.deleteViaSSH(sessionID, clean); fbErr == nil {
				m.invalidateDirTreeCache(sessionID, clean)
				return nil
			}
		}
		return err
	}

	info, lerr := sc.Lstat(clean)
	if lerr != nil && isSFTPConnError(lerr) {
		m.invalidateSFTP(sessionID, sc)
		if sc2, err2 := m.getSFTP(sessionID); err2 == nil {
			sc = sc2
			info, lerr = sc.Lstat(clean)
		}
	}
	if lerr != nil {
		if errors.Is(lerr, os.ErrNotExist) {
			return lerr
		}
		if m.cachedSSHSameFS(sessionID) {
			if fbErr := m.deleteViaSSH(sessionID, clean); fbErr == nil {
				m.invalidateDirTreeCache(sessionID, clean)
				return nil
			}
		}
		return lerr
	}

	isRealDir := info.IsDir() && info.Mode()&os.ModeSymlink == 0
	var sshTried bool
	if isRealDir && m.sshMatchesSFTPRoot(sessionID, sc) {
		sshTried = true
		if fbErr := m.deleteViaSSH(sessionID, clean); fbErr == nil {
			m.invalidateDirTreeCache(sessionID, clean)
			return nil
		} else if errors.Is(fbErr, errSSHDeleteNotFound) {
			if _, chkErr := sc.Lstat(clean); errors.Is(chkErr, os.ErrNotExist) {
				m.invalidateDirTreeCache(sessionID, clean)
				return nil
			}
		}
	}

	rmErr := removeRemoteNodeWithInfo(sc, clean, info, false)
	if rmErr != nil && isSFTPConnError(rmErr) {
		m.invalidateSFTP(sessionID, sc)
		if sc2, err2 := m.getSFTP(sessionID); err2 == nil {
			sc = sc2
			if curInfo, chkErr := sc.Lstat(clean); errors.Is(chkErr, os.ErrNotExist) {
				m.invalidateDirTreeCache(sessionID, clean)
				return nil
			} else if chkErr == nil {
				rmErr = removeRemoteNodeWithInfo(sc, clean, curInfo, false)
			}
		}
	}

	if rmErr != nil {
		if _, postErr := sc.Lstat(clean); errors.Is(postErr, os.ErrNotExist) {
			m.invalidateDirTreeCache(sessionID, clean)
			return nil
		}
		if !sshTried && m.sshMatchesSFTPRoot(sessionID, sc) {
			fbErr := m.deleteViaSSH(sessionID, clean)
			if fbErr == nil {
				m.invalidateDirTreeCache(sessionID, clean)
				return nil
			}
			if errors.Is(fbErr, errSSHDeleteNotFound) {
				if _, postErr := sc.Lstat(clean); errors.Is(postErr, os.ErrNotExist) {
					m.invalidateDirTreeCache(sessionID, clean)
					return nil
				}
			}
			return fmt.Errorf("%w (SSH 回退失败: %v)", rmErr, fbErr)
		}
		return rmErr
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
	sem := make(chan struct{}, maxDeleteConcurrency)
	var (
		wg       sync.WaitGroup
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
	for _, f := range files {
		if aborted.Load() {
			break
		}
		p := path.Join(parent, f.Name())
		wg.Add(1)
		sem <- struct{}{}
		go func(target string) {
			defer func() { <-sem; wg.Done() }()
			if aborted.Load() {
				return
			}
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
		}(p)
	}
	wg.Wait()
	return firstErr
}
