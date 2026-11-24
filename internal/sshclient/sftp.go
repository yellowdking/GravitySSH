package sshclient

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

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
	if dir == "" || dir == "~" {
		wd, wdErr := sc.Getwd()
		if wdErr != nil || wd == "" {
			dir = "/"
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

// SFTPUpload uploads a local file into remoteDir with live progress events.
func (m *Manager) SFTPUpload(sessionID, localPath, remoteDir string) error {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}

	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer src.Close()

	st, err := src.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("暂不支持直接上传文件夹，请选择文件")
	}

	fileName := filepath.Base(localPath)
	cleanDir := path.Clean(remoteDir)
	remotePath := path.Join(cleanDir, fileName)
	transferID := uuid.NewString()

	dst, err := sc.Create(remotePath)
	if err != nil {
		m.emitTransfer(models.TransferProgress{
			TransferID: transferID,
			SessionID:  sessionID,
			Direction:  "upload",
			FileName:   fileName,
			RemotePath: remotePath,
			LocalPath:  localPath,
			Total:      st.Size(),
			Status:     "error",
			Error:      err.Error(),
		})
		return fmt.Errorf("创建远程文件失败: %w", err)
	}
	defer dst.Close()

	err = m.copyWithProgress(dst, src, models.TransferProgress{
		TransferID: transferID,
		SessionID:  sessionID,
		Direction:  "upload",
		FileName:   fileName,
		RemotePath: remotePath,
		LocalPath:  localPath,
		Total:      st.Size(),
		Status:     "running",
	})
	m.invalidateDirCache(sessionID, cleanDir)
	return err
}

// SFTPDownload downloads a remote file to localPath with live progress events.
func (m *Manager) SFTPDownload(sessionID, remotePath, localPath string) error {
	sc, err := m.getSFTP(sessionID)
	if err != nil {
		return err
	}

	cleanRemote := path.Clean(remotePath)
	src, err := sc.Open(cleanRemote)
	if err != nil {
		return fmt.Errorf("打开远程文件失败: %w", err)
	}
	defer src.Close()

	st, err := src.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("无法直接下载目录，请选择具体文件")
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	dst, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("创建本地文件失败: %w", err)
	}
	defer dst.Close()

	transferID := uuid.NewString()
	return m.copyWithProgress(dst, src, models.TransferProgress{
		TransferID: transferID,
		SessionID:  sessionID,
		Direction:  "download",
		FileName:   path.Base(cleanRemote),
		RemotePath: cleanRemote,
		LocalPath:  localPath,
		Total:      st.Size(),
		Status:     "running",
	})
}

func (m *Manager) copyWithProgress(dst io.Writer, src io.Reader, prog models.TransferProgress) error {
	buf := make([]byte, 64*1024)
	lastPercent := -1
	m.emitTransfer(prog)

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			if writeErr != nil {
				prog.Status = "error"
				prog.Error = writeErr.Error()
				m.emitTransfer(prog)
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
					m.emitTransfer(prog)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			prog.Status = "error"
			prog.Error = readErr.Error()
			m.emitTransfer(prog)
			return readErr
		}
	}

	prog.Percent = 100
	prog.Status = "done"
	m.emitTransfer(prog)
	return nil
}
