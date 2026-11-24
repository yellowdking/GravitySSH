package sshclient

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func isSFTPConnError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, sftp.ErrSSHFxNoConnection) {
		return true
	}
	low := strings.ToLower(err.Error())
	for _, tok := range []string{
		"connection lost", "no connection", "broken pipe",
		"connection reset", "use of closed", "channel closed",
		"ssh: disconnect", "unexpected eof",
	} {
		if strings.Contains(low, tok) {
			return true
		}
	}
	return low == "eof"
}

func (m *Manager) getSFTP(sessionID string) (*sftp.Client, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	dialHook := m.sftpDial
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("会话不存在或已关闭: %s", sessionID)
	}

	sess.mu.Lock()
	if sess.sftpClient != nil {
		sc := sess.sftpClient
		sess.mu.Unlock()
		return sc, nil
	}
	if sess.closed || sess.userClosed {
		sess.mu.Unlock()
		return nil, fmt.Errorf("会话已关闭: %s", sessionID)
	}
	sshCli := sess.dialRes
	sess.mu.Unlock()

	var (
		sc  *sftp.Client
		err error
	)
	if dialHook != nil {
		sc, err = dialHook(sessionID)
	} else {
		if sshCli == nil || sshCli.Client == nil {
			return nil, fmt.Errorf("SSH 连接未就绪")
		}
		sc, err = sftp.NewClient(sshCli.Client)
	}
	if err != nil {
		return nil, fmt.Errorf("初始化 SFTP 失败: %w", err)
	}

	sess.mu.Lock()
	if sess.closed || sess.userClosed || (dialHook == nil && (sess.dialRes == nil || sess.dialRes != sshCli)) {
		sess.mu.Unlock()
		_ = sc.Close()
		return nil, fmt.Errorf("会话已关闭: %s", sessionID)
	}
	if sess.sftpClient != nil {
		existing := sess.sftpClient
		sess.mu.Unlock()
		_ = sc.Close()
		return existing, nil
	}
	sess.sftpClient = sc
	sess.info.SFTPReady = true
	sess.mu.Unlock()
	return sc, nil
}

func (m *Manager) invalidateSFTP(sessionID string, bad *sftp.Client) {
	if bad == nil {
		return
	}
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	var toClose *sftp.Client
	sess.mu.Lock()
	if sess.sftpClient == bad {
		toClose = sess.sftpClient
		sess.sftpClient = nil
	}
	sess.mu.Unlock()
	if toClose != nil {
		go func(c *sftp.Client) { _ = c.Close() }(toClose)
	}
}

func (m *Manager) withSFTP(sessionID string, fn func(sc *sftp.Client) error) error {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}
	err = fn(sc)
	if err == nil || !isSFTPConnError(err) {
		return err
	}
	m.invalidateSFTP(sessionID, sc)
	sc2, err2 := m.getSFTP(sessionID)
	if err2 != nil {
		return err
	}
	return fn(sc2)
}

// SFTPList lists entries in the given remote directory (defaults to home/cwd when empty).
func (m *Manager) SFTPList(sessionID, remotePath string) (models.SFTPListResult, error) {
	var (
		dir     string
		entries []models.SFTPFileEntry
	)
	err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		d := strings.TrimSpace(remotePath)
		if d == "" || d == "~" || strings.HasPrefix(d, "~/") {
			wd, wdErr := sc.Getwd()
			if wdErr != nil {
				if isSFTPConnError(wdErr) {
					return wdErr
				}
				wd = "/"
			}
			if wd == "" {
				wd = "/"
			}
			if strings.HasPrefix(d, "~/") {
				d = path.Join(wd, d[2:])
			} else {
				d = wd
			}
		}
		d = path.Clean(d)
		if !strings.HasPrefix(d, "/") {
			d = "/" + d
		}
		infos, rErr := sc.ReadDir(d)
		if rErr != nil {
			if isSFTPConnError(rErr) {
				return rErr
			}
			return fmt.Errorf("读取目录 %s 失败: %w", d, rErr)
		}
		dir = d
		entries = buildSFTPEntries(sc, d, infos)
		return nil
	})
	if err != nil {
		return models.SFTPListResult{}, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	m.updateDirCache(sessionID, dir, entries)
	return models.SFTPListResult{Path: dir, Parent: path.Dir(dir), Entries: entries}, nil
}

func buildSFTPEntries(sc *sftp.Client, dir string, infos []os.FileInfo) []models.SFTPFileEntry {
	entries := make([]models.SFTPFileEntry, len(infos))
	var symlinks []int
	for i, info := range infos {
		full := path.Join(dir, info.Name())
		isSymlink := info.Mode()&os.ModeSymlink != 0
		entries[i] = models.SFTPFileEntry{
			Name: info.Name(), Path: full, IsDir: info.IsDir(), IsSymlink: isSymlink,
			Size: info.Size(), Mode: info.Mode().String(), ModTime: info.ModTime(),
		}
		if isSymlink {
			symlinks = append(symlinks, i)
		}
	}
	if len(symlinks) > 0 {
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for _, idx := range symlinks {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int) {
				defer func() { <-sem; wg.Done() }()
				if st, err := sc.Stat(entries[i].Path); err == nil && st.IsDir() {
					entries[i].IsDir = true
				}
			}(idx)
		}
		wg.Wait()
	}
	return entries
}

func renameRemote(sc *sftp.Client, oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}
	_, preErr := sc.Lstat(oldPath)
	if isSFTPConnError(preErr) {
		return preErr
	}
	hadOld := preErr == nil

	var err error
	if _, hasPosix := sc.HasExtension("posix-rename@openssh.com"); hasPosix {
		err = sc.PosixRename(oldPath, newPath)
		if err != nil && !isSFTPConnError(err) && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			if rErr := sc.Rename(oldPath, newPath); rErr == nil {
				return nil
			} else {
				err = rErr
			}
		}
	} else {
		err = sc.Rename(oldPath, newPath)
		if err != nil && !isSFTPConnError(err) && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			if pErr := sc.PosixRename(oldPath, newPath); pErr == nil {
				return nil
			}
		}
	}

	if err != nil && hadOld && errors.Is(err, os.ErrNotExist) {
		_, oldErr := sc.Lstat(oldPath)
		_, newErr := sc.Lstat(newPath)
		if errors.Is(oldErr, os.ErrNotExist) && newErr == nil {
			return nil
		}
	}
	return err
}
