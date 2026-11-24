package sshclient

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

// SFTPUpload streams a local file directly to remoteDir (rebuilding a stale SFTP client on open error).
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
	transferID := uuid.NewString()
	total := st.Size()

	var dst *sftp.File
	err = m.withSFTP(sessionID, func(sc *sftp.Client) error {
		f, oErr := sc.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if oErr != nil {
			return oErr
		}
		dst = f
		return nil
	})
	if err != nil {
		emitErr := fmt.Errorf("创建远程文件失败: %w", err)
		m.emitTransfer(models.TransferProgress{
			TransferID: transferID, SessionID: sessionID, Direction: "upload",
			FileName: fileName, RemotePath: remotePath, LocalPath: localPath,
			Total: total, Status: "error", Error: emitErr.Error(),
		})
		return emitErr
	}

	prog := models.TransferProgress{
		TransferID: transferID, SessionID: sessionID, Direction: "upload",
		FileName: fileName, RemotePath: remotePath, LocalPath: localPath,
		Total: total, Status: "running",
	}
	cpErr := m.copyChunked(dst, src, &prog)
	closeErr := dst.Close()
	if cpErr != nil || closeErr != nil {
		finalErr := cpErr
		if finalErr == nil {
			finalErr = closeErr
		}
		if isSFTPConnError(finalErr) {
			if sc, scErr := m.getSFTP(sessionID); scErr == nil {
				m.invalidateSFTP(sessionID, sc)
			}
		}
		m.emitTransfer(models.TransferProgress{
			TransferID: transferID, SessionID: sessionID, Direction: "upload",
			FileName: fileName, RemotePath: remotePath, LocalPath: localPath,
			Transferred: prog.Transferred, Total: total, Status: "error", Error: finalErr.Error(),
		})
		return finalErr
	}

	prog.Percent = 100
	prog.Status = "done"
	m.emitTransfer(prog)
	m.invalidateDirCache(sessionID, cleanDir)
	return nil
}

// SFTPDownload streams a remote file directly to localPath (rebuilding a stale SFTP client on open error).
func (m *Manager) SFTPDownload(sessionID, remotePath, localPath string) error {
	cleanRemote := path.Clean(remotePath)
	fileName := path.Base(cleanRemote)
	transferID := uuid.NewString()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	var (
		src   *sftp.File
		total int64
	)
	err := m.withSFTP(sessionID, func(sc *sftp.Client) error {
		f, oErr := sc.Open(cleanRemote)
		if oErr != nil {
			return oErr
		}
		st, stErr := f.Stat()
		if stErr != nil {
			_ = f.Close()
			return stErr
		}
		if st.IsDir() {
			_ = f.Close()
			return fmt.Errorf("无法直接下载目录，请使用打包下载目录")
		}
		src = f
		total = st.Size()
		return nil
	})
	if err != nil {
		m.emitTransfer(models.TransferProgress{
			TransferID: transferID, SessionID: sessionID, Direction: "download",
			FileName: fileName, RemotePath: cleanRemote, LocalPath: localPath,
			Status: "error", Error: err.Error(),
		})
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("创建本地文件失败: %w", err)
	}

	prog := models.TransferProgress{
		TransferID: transferID, SessionID: sessionID, Direction: "download",
		FileName: fileName, RemotePath: cleanRemote, LocalPath: localPath,
		Total: total, Status: "running",
	}
	cpErr := m.copyChunked(dst, src, &prog)
	closeErr := dst.Close()
	if cpErr != nil || closeErr != nil {
		finalErr := cpErr
		if finalErr == nil {
			finalErr = closeErr
		}
		if isSFTPConnError(finalErr) {
			if sc, scErr := m.getSFTP(sessionID); scErr == nil {
				m.invalidateSFTP(sessionID, sc)
			}
		}
		m.emitTransfer(models.TransferProgress{
			TransferID: transferID, SessionID: sessionID, Direction: "download",
			FileName: fileName, RemotePath: cleanRemote, LocalPath: localPath,
			Transferred: prog.Transferred, Total: total, Status: "error", Error: finalErr.Error(),
		})
		return finalErr
	}

	prog.Percent = 100
	prog.Status = "done"
	m.emitTransfer(prog)
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
