package sshclient

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

const maxEditableFileSize = 1024 * 1024 // 1 MB

// SFTPReadTextFile reads a remote text/config file for the built-in editor.
func (m *Manager) SFTPReadTextFile(sessionID, remotePath string) (models.RemoteFileContent, error) {
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return models.RemoteFileContent{}, fmt.Errorf("无效的文件路径")
	}

	var res models.RemoteFileContent
	err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		st, err := sc.Stat(clean)
		if err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("读取文件信息失败: %w", err)
		}
		if st.IsDir() {
			return fmt.Errorf("无法编辑目录")
		}
		if st.Size() > maxEditableFileSize {
			return fmt.Errorf("文件过大（%.1f MB），内置编辑器仅支持 1 MB 以内的文本文件", float64(st.Size())/(1024*1024))
		}

		f, err := sc.Open(clean)
		if err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("打开远程文件失败: %w", err)
		}
		defer f.Close()

		raw, err := io.ReadAll(io.LimitReader(f, maxEditableFileSize+1))
		if err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("读取文件内容失败: %w", err)
		}
		if len(raw) > maxEditableFileSize {
			return fmt.Errorf("文件过大（超过 1 MB），内置编辑器仅支持 1 MB 以内的文本文件")
		}
		probeLen := len(raw)
		if probeLen > 4096 {
			probeLen = 4096
		}
		if bytes.IndexByte(raw[:probeLen], 0x00) >= 0 {
			return fmt.Errorf("检测到二进制文件，请直接下载查看")
		}

		res = models.RemoteFileContent{
			Path:    clean,
			Content: string(raw),
			Size:    int64(len(raw)),
		}
		return nil
	})
	return res, err
}

func writeRemoteFileContent(sc *sftp.Client, target string, data []byte, mode os.FileMode) error {
	f, err := sc.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if mode != 0 {
		_ = sc.Chmod(target, mode)
	}
	return nil
}

// SFTPWriteTextFile writes edited text back to remotePath atomically via a temp file + rename
// (resolving symlinks via RealPath so symlinks are preserved, and falling back to in-place write
// when directory permissions or target semantics disallow rename), preserving existing file permissions.
func (m *Manager) SFTPWriteTextFile(sessionID, remotePath, content string) error {
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return fmt.Errorf("无效的文件路径")
	}

	data := []byte(content)
	writeTarget := clean
	err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		target := clean
		allowAtomicRename := true

		lst, lstatErr := sc.Lstat(clean)
		if lstatErr != nil && isSFTPConnError(lstatErr) {
			return lstatErr
		}
		if lstatErr == nil && lst.Mode()&os.ModeSymlink != 0 {
			realTarget, rpErr := sc.RealPath(clean)
			if rpErr != nil {
				if isSFTPConnError(rpErr) {
					return rpErr
				}
				// Do not rename over an unresolved symlink; write in-place to follow the link.
				allowAtomicRename = false
			} else {
				realClean := path.Clean(strings.TrimSpace(realTarget))
				if realClean != "" && realClean != "/" && realClean != "." && realClean != clean {
					target = realClean
				} else {
					allowAtomicRename = false
				}
			}
		}

		var existingMode os.FileMode
		if st, statErr := sc.Stat(target); statErr == nil {
			if st.IsDir() {
				return fmt.Errorf("无法编辑目录")
			}
			existingMode = st.Mode().Perm()
		} else if isSFTPConnError(statErr) {
			return statErr
		}

		writeTarget = target
		if allowAtomicRename {
			tmpPath := path.Join(path.Dir(target), fmt.Sprintf(".%s.gssh-tmp-%s", path.Base(target), uuid.NewString()[:8]))
			if wErr := writeRemoteFileContent(sc, tmpPath, data, existingMode); wErr == nil {
				if rErr := renameRemote(sc, tmpPath, target, true); rErr == nil {
					return nil
				} else {
					_ = sc.Remove(tmpPath)
					if isSFTPConnError(rErr) {
						return rErr
					}
				}
			} else {
				_ = sc.Remove(tmpPath)
				if isSFTPConnError(wErr) {
					return wErr
				}
			}
		}

		if err := writeRemoteFileContent(sc, target, data, existingMode); err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("保存远程文件失败: %w", err)
		}
		return nil
	})
	if err == nil {
		m.invalidateDirCache(sessionID, path.Dir(clean))
		if targetDir := path.Dir(writeTarget); targetDir != path.Dir(clean) {
			m.invalidateDirCache(sessionID, targetDir)
		}
	}
	return err
}
