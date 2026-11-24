package sshclient

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

// SFTPUpload uploads a local file into remoteDir with resumable partial commit and auto-retry.
func (m *Manager) SFTPUpload(sessionID, localPath, remoteDir string) error {
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
	partPath := remotePath + ".gsshpart"
	transferID := uuid.NewString()
	total := st.Size()

	var lastErr error
	for attempt := 1; attempt <= maxTransferRetries; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		sc, scErr := m.getSFTP(sessionID)
		if scErr != nil {
			lastErr = scErr
			continue
		}

		var offset int64
		if pst, pErr := sc.Stat(partPath); pErr == nil && pst.Size() > 0 && pst.Size() <= total {
			offset = pst.Size()
		}
		if _, sErr := src.Seek(offset, io.SeekStart); sErr != nil {
			return sErr
		}

		flags := os.O_WRONLY | os.O_CREATE
		if offset == 0 {
			flags |= os.O_TRUNC
		}
		dst, oErr := sc.OpenFile(partPath, flags)
		if oErr != nil {
			lastErr = fmt.Errorf("创建远程文件失败: %w", oErr)
			continue
		}
		if offset > 0 {
			if _, sErr := dst.Seek(offset, io.SeekStart); sErr != nil {
				_ = dst.Close()
				lastErr = sErr
				continue
			}
		}

		prog := models.TransferProgress{
			TransferID: transferID, SessionID: sessionID, Direction: "upload",
			FileName: fileName, RemotePath: remotePath, LocalPath: localPath,
			Transferred: offset, Total: total, Status: "running",
		}
		cpErr := m.copyChunked(dst, src, &prog)
		closeErr := dst.Close()
		if cpErr != nil || closeErr != nil {
			if cpErr != nil {
				lastErr = cpErr
			} else {
				lastErr = closeErr
			}
			continue
		}

		if err := commitRemotePart(sc, partPath, remotePath); err != nil {
			lastErr = fmt.Errorf("提交远程文件失败: %w", err)
			continue
		}
		prog.Percent = 100
		prog.Status = "done"
		m.emitTransfer(prog)
		m.invalidateDirCache(sessionID, cleanDir)
		return nil
	}

	m.emitTransfer(models.TransferProgress{
		TransferID: transferID, SessionID: sessionID, Direction: "upload",
		FileName: fileName, RemotePath: remotePath, LocalPath: localPath,
		Total: total, Status: "error", Error: lastErr.Error(),
	})
	return lastErr
}

// SFTPDownload downloads a remote file to localPath with resumable .gsshpart and auto-retry.
func (m *Manager) SFTPDownload(sessionID, remotePath, localPath string) error {
	cleanRemote := path.Clean(remotePath)
	fileName := path.Base(cleanRemote)
	partPath := localPath + ".gsshpart"
	transferID := uuid.NewString()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	var (
		total   int64
		lastErr error
	)
	for attempt := 1; attempt <= maxTransferRetries; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		sc, scErr := m.getSFTP(sessionID)
		if scErr != nil {
			lastErr = scErr
			continue
		}
		src, oErr := sc.Open(cleanRemote)
		if oErr != nil {
			lastErr = fmt.Errorf("打开远程文件失败: %w", oErr)
			continue
		}
		st, stErr := src.Stat()
		if stErr != nil {
			_ = src.Close()
			lastErr = stErr
			continue
		}
		if st.IsDir() {
			_ = src.Close()
			return fmt.Errorf("无法直接下载目录，请使用打包下载目录")
		}
		total = st.Size()

		var offset int64
		if pst, pErr := os.Stat(partPath); pErr == nil && pst.Size() > 0 && pst.Size() <= total {
			offset = pst.Size()
		}
		if offset > 0 {
			if _, sErr := src.Seek(offset, io.SeekStart); sErr != nil {
				offset = 0
				_, _ = src.Seek(0, io.SeekStart)
			}
		}

		flags := os.O_WRONLY | os.O_CREATE
		if offset == 0 {
			flags |= os.O_TRUNC
		}
		dst, dErr := os.OpenFile(partPath, flags, 0o644)
		if dErr != nil {
			_ = src.Close()
			return fmt.Errorf("创建本地文件失败: %w", dErr)
		}
		if offset > 0 {
			if _, sErr := dst.Seek(offset, io.SeekStart); sErr != nil {
				_ = dst.Close()
				_ = src.Close()
				return sErr
			}
		}

		prog := models.TransferProgress{
			TransferID: transferID, SessionID: sessionID, Direction: "download",
			FileName: fileName, RemotePath: cleanRemote, LocalPath: localPath,
			Transferred: offset, Total: total, Status: "running",
		}
		cpErr := m.copyChunked(dst, src, &prog)
		_ = src.Close()
		closeErr := dst.Close()
		if cpErr != nil || closeErr != nil {
			if cpErr != nil {
				lastErr = cpErr
			} else {
				lastErr = closeErr
			}
			continue
		}

		if err := os.Rename(partPath, localPath); err != nil {
			_ = os.Remove(localPath)
			if err2 := os.Rename(partPath, localPath); err2 != nil {
				return fmt.Errorf("保存本地文件失败: %w", err2)
			}
		}
		prog.Percent = 100
		prog.Status = "done"
		m.emitTransfer(prog)
		return nil
	}

	m.emitTransfer(models.TransferProgress{
		TransferID: transferID, SessionID: sessionID, Direction: "download",
		FileName: fileName, RemotePath: cleanRemote, LocalPath: localPath,
		Total: total, Status: "error", Error: lastErr.Error(),
	})
	return lastErr
}
