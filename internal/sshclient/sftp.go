package sshclient

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

const maxTransferRetries = 3

func (m *Manager) getSFTP(sessionID string) (*sftp.Client, error) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("会话不存在或已关闭: %s", sessionID)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.sftpClient == nil {
		if sess.dialRes == nil || sess.dialRes.Client == nil {
			return nil, fmt.Errorf("SSH 连接未就绪")
		}
		sc, err := sftp.NewClient(sess.dialRes.Client)
		if err != nil {
			return nil, fmt.Errorf("初始化 SFTP 失败: %w", err)
		}
		sess.sftpClient = sc
	}
	return sess.sftpClient, nil
}

// SFTPList lists entries in the given remote directory (defaults to home/cwd when empty).
func (m *Manager) SFTPList(sessionID, remotePath string) (models.SFTPListResult, error) {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return models.SFTPListResult{}, err
	}

	dir := strings.TrimSpace(remotePath)
	if dir == "" || dir == "~" || strings.HasPrefix(dir, "~/") {
		wd, wdErr := sc.Getwd()
		if wdErr != nil || wd == "" {
			wd = "/"
		}
		if strings.HasPrefix(dir, "~/") {
			dir = path.Join(wd, dir[2:])
		} else {
			dir = wd
		}
	}
	dir = path.Clean(dir)
	if !strings.HasPrefix(dir, "/") {
		dir = "/" + dir
	}

	infos, err := sc.ReadDir(dir)
	if err != nil {
		return models.SFTPListResult{}, fmt.Errorf("读取目录 %s 失败: %w", dir, err)
	}

	entries := make([]models.SFTPFileEntry, 0, len(infos))
	for _, info := range infos {
		full := path.Join(dir, info.Name())
		isSymlink := info.Mode()&os.ModeSymlink != 0
		isDir := info.IsDir()
		if isSymlink {
			if targetInfo, statErr := sc.Stat(full); statErr == nil && targetInfo.IsDir() {
				isDir = true
			}
		}
		entries = append(entries, models.SFTPFileEntry{
			Name:      info.Name(),
			Path:      full,
			IsDir:     isDir,
			IsSymlink: isSymlink,
			Size:      info.Size(),
			Mode:      info.Mode().String(),
			ModTime:   info.ModTime(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	m.updateDirCache(sessionID, dir, entries)

	return models.SFTPListResult{
		Path:    dir,
		Parent:  path.Dir(dir),
		Entries: entries,
	}, nil
}

func commitRemotePart(sc *sftp.Client, partPath, remotePath string) error {
	if err := sc.PosixRename(partPath, remotePath); err == nil {
		return nil
	}
	_ = sc.Remove(remotePath)
	return sc.Rename(partPath, remotePath)
}

func renameRemote(sc *sftp.Client, oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}
	_, preErr := sc.Lstat(oldPath)
	hadOld := preErr == nil

	var err error
	if _, hasPosix := sc.HasExtension("posix-rename@openssh.com"); hasPosix {
		err = sc.PosixRename(oldPath, newPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			if rErr := sc.Rename(oldPath, newPath); rErr == nil {
				return nil
			} else {
				err = rErr
			}
		}
	} else {
		err = sc.Rename(oldPath, newPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
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

func removeRemoteRecursive(sc *sftp.Client, target string) error {
	return removeRemoteNode(sc, target, false)
}

func removeRemoteNode(sc *sftp.Client, target string, ignoreNotExist bool) error {
	info, err := sc.Lstat(target)
	if err != nil {
		if ignoreNotExist && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
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
		for _, item := range items {
			name := item.Name()
			if name == "" || name == "." || name == ".." {
				continue
			}
			if err := removeRemoteNode(sc, path.Join(target, name), true); err != nil {
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
		if _, lerr := sc.Lstat(target); errors.Is(lerr, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

func (m *Manager) copyChunked(dst io.Writer, src io.Reader, prog *models.TransferProgress) error {
	buf := make([]byte, 64*1024)
	lastPercent := -1
	if prog.Total > 0 {
		prog.Percent = int(prog.Transferred * 100 / prog.Total)
		lastPercent = prog.Percent
	}
	m.emitTransfer(*prog)

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			prog.Transferred += int64(written)
			if prog.Total > 0 {
				pct := int(prog.Transferred * 100 / prog.Total)
				if pct > 100 {
					pct = 100
				}
				if pct != lastPercent {
					lastPercent = pct
					prog.Percent = pct
					m.emitTransfer(*prog)
				}
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
