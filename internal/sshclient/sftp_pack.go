package sshclient

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

const packedDownloadTimeout = 30 * time.Minute

// SFTPDownloadPacked streams a remote directory/file via `tar -cf -` over SSH
// and unpacks directly into localDir without leaving any temporary archive on the server.
func (m *Manager) SFTPDownloadPacked(sessionID, remotePath, localDir string) (int, error) {
	client, err := m.getSSHClient(sessionID)
	if err != nil {
		return 0, err
	}
	clean := path.Clean(strings.TrimSpace(remotePath))
	if clean == "" || clean == "/" || clean == "." {
		return 0, fmt.Errorf("无效的远程目录路径")
	}
	parentDir := path.Dir(clean)
	baseName := path.Base(clean)

	sshSess, err := client.NewSession()
	if err != nil {
		return 0, fmt.Errorf("创建打包传输会话失败: %w", err)
	}
	defer sshSess.Close()

	stdout, err := sshSess.StdoutPipe()
	if err != nil {
		return 0, err
	}
	var stderr bytes.Buffer
	sshSess.Stderr = &stderr

	cmd := fmt.Sprintf("tar -cf - -C %s %s", shellQuote(parentDir), shellQuote(baseName))
	if err := sshSess.Start(cmd); err != nil {
		return 0, fmt.Errorf("启动远程流式打包失败: %w", err)
	}

	dlCtx, cancel := context.WithTimeout(context.Background(), packedDownloadTimeout)
	defer cancel()
	stopCh := m.sessionStopCh(sessionID)
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-stopCh:
			_ = sshSess.Close()
		case <-dlCtx.Done():
			_ = sshSess.Close()
		}
	}()

	transferID := uuid.NewString()
	m.emitTransfer(models.TransferProgress{
		TransferID: transferID,
		SessionID:  sessionID,
		Direction:  "download",
		FileName:   baseName + " (打包下载)",
		RemotePath: clean,
		LocalPath:  localDir,
		Status:     "running",
	})

	fileCount, unpackErr := unpackTarStream(stdout, localDir)
	if unpackErr != nil {
		// Close the SSH session BEFORE Wait so a remote `tar` still writing to a full
		// SSH window cannot deadlock Wait().
		_ = sshSess.Close()
	}
	waitErr := sshSess.Wait()
	close(done)

	if unpackErr != nil || waitErr != nil {
		errText := ""
		if unpackErr != nil {
			errText = unpackErr.Error()
		} else if dlCtx.Err() != nil {
			errText = dlCtx.Err().Error()
		} else if s := strings.TrimSpace(stderr.String()); s != "" {
			errText = s
		} else {
			errText = waitErr.Error()
		}
		m.emitTransfer(models.TransferProgress{
			TransferID: transferID,
			SessionID:  sessionID,
			Direction:  "download",
			FileName:   baseName + " (打包下载)",
			RemotePath: clean,
			LocalPath:  localDir,
			Status:     "error",
			Error:      errText,
		})
		return fileCount, fmt.Errorf("打包下载失败: %s", errText)
	}

	m.emitTransfer(models.TransferProgress{
		TransferID: transferID,
		SessionID:  sessionID,
		Direction:  "download",
		FileName:   baseName + " (打包下载)",
		RemotePath: clean,
		LocalPath:  localDir,
		Percent:    100,
		Status:     "done",
	})
	return fileCount, nil
}

func unpackTarStream(r io.Reader, destRoot string) (int, error) {
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return 0, err
	}
	absRoot, err := filepath.Abs(destRoot)
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(r)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		target, err := safeJoinLocal(absRoot, hdr.Name)
		if err != nil {
			return count, err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return count, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return count, err
			}
			f, err := os.Create(target)
			if err != nil {
				return count, err
			}
			_, cpErr := io.Copy(f, tr)
			closeErr := f.Close()
			if cpErr != nil {
				return count, cpErr
			}
			if closeErr != nil {
				return count, closeErr
			}
			count++
		}
	}
	return count, nil
}

func safeJoinLocal(absRoot, tarEntryName string) (string, error) {
	if strings.ContainsRune(tarEntryName, 0) {
		return "", fmt.Errorf("检测到不安全的归档路径: %s", tarEntryName)
	}
	norm := strings.ReplaceAll(tarEntryName, "\\", "/")
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimLeft(norm, "/")))
	if cleaned == "." || cleaned == "" {
		return absRoot, nil
	}
	if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" {
		return "", fmt.Errorf("检测到不安全的归档路径: %s", tarEntryName)
	}
	joined := filepath.Join(absRoot, cleaned)
	absTarget, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("检测到不安全的归档路径: %s", tarEntryName)
	}
	return absTarget, nil
}

// SFTPCompress compresses one or more entries inside remoteDir into .tar.gz or .zip.
func (m *Manager) SFTPCompress(sessionID, remoteDir string, names []string, format string) (string, error) {
	client, err := m.getSSHClient(sessionID)
	if err != nil {
		return "", err
	}
	dir := path.Clean(strings.TrimSpace(remoteDir))
	if dir == "" {
		dir = "/"
	}
	cleanNames := make([]string, 0, len(names))
	for _, n := range names {
		b := path.Base(strings.TrimSpace(n))
		if b != "" && b != "." && b != ".." && b != "/" {
			cleanNames = append(cleanNames, b)
		}
	}
	if len(cleanNames) == 0 {
		return "", fmt.Errorf("未选择要压缩的文件或目录")
	}

	archiveName, cmd, err := buildCompressCommand(dir, cleanNames, format)
	if err != nil {
		return "", err
	}

	out, runErr := runSSHExec(context.Background(), client, cmd, 180*time.Second, true)
	m.invalidateDirCache(sessionID, dir)

	if runErr != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 220 {
			snippet = snippet[len(snippet)-220:]
		}
		if snippet != "" {
			return "", fmt.Errorf("压缩失败: %s", snippet)
		}
		return "", fmt.Errorf("压缩失败: %v", runErr)
	}
	return archiveName, nil
}

func buildCompressCommand(dir string, names []string, format string) (string, string, error) {
	base := names[0]
	if len(names) > 1 {
		base = fmt.Sprintf("archive-%d", time.Now().Unix())
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = shellQuote(n)
	}
	args := strings.Join(quoted, " ")
	qDir := shellQuote(dir)

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		archive := base + ".zip"
		install := autoInstallSnippet("zip", "zip", "zip", "zip", "zip", "zip")
		cmd := fmt.Sprintf("cd %s && { if ! command -v zip >/dev/null 2>&1; then %s; fi; zip -rq %s %s; }", qDir, install, shellQuote(archive), args)
		return archive, cmd, nil
	case "", "tar.gz", "tgz":
		archive := base + ".tar.gz"
		cmd := fmt.Sprintf("cd %s && tar -czf %s %s", qDir, shellQuote(archive), args)
		return archive, cmd, nil
	default:
		return "", "", fmt.Errorf("不支持的压缩格式: %s", format)
	}
}
