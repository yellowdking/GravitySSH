package sshclient

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

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

// SFTPWriteTextFile writes edited text back to remotePath, preserving existing file permissions.
func (m *Manager) SFTPWriteTextFile(sessionID, remotePath, content string) error {
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return fmt.Errorf("无效的文件路径")
	}

	err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		var existingMode os.FileMode
		if st, statErr := sc.Stat(clean); statErr == nil {
			existingMode = st.Mode().Perm()
		}

		f, err := sc.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("写入远程文件失败: %w", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			_ = f.Close()
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("保存远程文件失败: %w", err)
		}
		if err := f.Close(); err != nil {
			if isSFTPConnError(err) {
				return err
			}
			return fmt.Errorf("关闭远程文件失败: %w", err)
		}
		if existingMode != 0 {
			_ = sc.Chmod(clean, existingMode)
		}
		return nil
	})
	if err == nil {
		m.invalidateDirCache(sessionID, path.Dir(clean))
	}
	return err
}
