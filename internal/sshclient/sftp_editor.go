package sshclient

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const maxEditableFileSize = 1024 * 1024 // 1 MB

// SFTPReadTextFile reads a remote text/config file for the built-in editor.
func (m *Manager) SFTPReadTextFile(sessionID, remotePath string) (models.RemoteFileContent, error) {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return models.RemoteFileContent{}, err
	}
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return models.RemoteFileContent{}, fmt.Errorf("无效的文件路径")
	}

	st, err := sc.Stat(clean)
	if err != nil {
		return models.RemoteFileContent{}, fmt.Errorf("读取文件信息失败: %w", err)
	}
	if st.IsDir() {
		return models.RemoteFileContent{}, fmt.Errorf("无法编辑目录")
	}
	if st.Size() > maxEditableFileSize {
		return models.RemoteFileContent{}, fmt.Errorf("文件过大（%.1f MB），内置编辑器仅支持 1 MB 以内的文本文件", float64(st.Size())/(1024*1024))
	}

	f, err := sc.Open(clean)
	if err != nil {
		return models.RemoteFileContent{}, fmt.Errorf("打开远程文件失败: %w", err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, maxEditableFileSize+1))
	if err != nil {
		return models.RemoteFileContent{}, fmt.Errorf("读取文件内容失败: %w", err)
	}
	probeLen := len(raw)
	if probeLen > 4096 {
		probeLen = 4096
	}
	if bytes.IndexByte(raw[:probeLen], 0x00) >= 0 {
		return models.RemoteFileContent{}, fmt.Errorf("检测到二进制文件，请直接下载查看")
	}

	return models.RemoteFileContent{
		Path:    clean,
		Content: string(raw),
		Size:    int64(len(raw)),
	}, nil
}

// SFTPWriteTextFile writes edited text back to remotePath, preserving existing file permissions.
func (m *Manager) SFTPWriteTextFile(sessionID, remotePath, content string) error {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return fmt.Errorf("无效的文件路径")
	}

	var existingMode os.FileMode
	if st, statErr := sc.Stat(clean); statErr == nil {
		existingMode = st.Mode().Perm()
	}

	f, err := sc.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("写入远程文件失败: %w", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		_ = f.Close()
		return fmt.Errorf("保存远程文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("关闭远程文件失败: %w", err)
	}
	if existingMode != 0 {
		_ = sc.Chmod(clean, existingMode)
	}
	m.invalidateDirCache(sessionID, path.Dir(clean))
	return nil
}
