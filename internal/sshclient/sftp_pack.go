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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
)

var packedDownloadTimeout = 30 * time.Minute

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

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

	dlCtx, cancel := context.WithTimeout(context.Background(), packedDownloadTimeout)
	defer cancel()
	stopCh := m.SessionStopCh(sessionID)
	if stopCh != nil {
		go func() {
			select {
			case <-stopCh:
				cancel()
			case <-dlCtx.Done():
			}
		}()
	}

	sshSess, err := newSessionWithContext(dlCtx, client)
	if err != nil {
		return 0, fmt.Errorf("创建打包传输会话失败: %w", err)
	}
	abortedSession := false
	defer func() {
		if !abortedSession {
			_ = sshSess.Close()
		}
	}()

	stdout, err := sshSess.StdoutPipe()
	if err != nil {
		return 0, err
	}
	var stderr lockedBuffer
	sshSess.Stderr = &stderr

	cmd := buildPackedDownloadCommand(parentDir, baseName)
	startCh := make(chan error, 1)
	go func() {
		startCh <- sshSess.Start(cmd)
	}()
	select {
	case startErr := <-startCh:
		if startErr != nil {
			return 0, fmt.Errorf("启动远程流式打包失败: %w", startErr)
		}
	case <-dlCtx.Done():
		abortedSession = true
		abortSSHSession(sshSess)
		return 0, fmt.Errorf("启动远程流式打包失败: %w", dlCtx.Err())
	}

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

	type unpackResult struct {
		count int
		err   error
	}
	unpackCh := make(chan unpackResult, 1)
	go func() {
		n, uErr := unpackTarStream(stdout, localDir)
		unpackCh <- unpackResult{count: n, err: uErr}
	}()

	var (
		fileCount int
		unpackErr error
		waitErr   error
	)
	select {
	case res := <-unpackCh:
		fileCount, unpackErr = res.count, res.err
	case <-dlCtx.Done():
		abortedSession = true
		abortSSHSession(sshSess)
		unpackErr = dlCtx.Err()
	}

	if unpackErr != nil {
		if !abortedSession {
			abortedSession = true
			abortSSHSession(sshSess)
		}
	} else {
		waitCh := make(chan error, 1)
		go func() {
			waitCh <- sshSess.Wait()
		}()
		select {
		case waitErr = <-waitCh:
		case <-dlCtx.Done():
			abortedSession = true
			abortSSHSession(sshSess)
			waitErr = dlCtx.Err()
		}
	}

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
				_ = os.Remove(target)
				return count, cpErr
			}
			if closeErr != nil {
				_ = os.Remove(target)
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
	for _, seg := range strings.Split(norm, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if strings.ContainsAny(seg, `:*?"<>|`) {
			return "", fmt.Errorf("检测到不安全的归档路径（含非法字符）: %s", tarEntryName)
		}
		for _, r := range seg {
			if r < 0x20 {
				return "", fmt.Errorf("检测到不安全的归档路径（含控制字符）: %s", tarEntryName)
			}
		}
	}
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
	rawDir := strings.TrimSpace(remoteDir)
	dir := "/"
	if rawDir != "" {
		cleaned := path.Clean(rawDir)
		if cleaned != "" && cleaned != "." {
			dir = cleaned
		}
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
		return "", formatExecError("压缩失败", out, runErr, 220)
	}
	return archiveName, nil
}

func buildPackedDownloadCommand(parentDir, baseName string) string {
	safeBase := safeRelativeName(baseName)
	return fmt.Sprintf(
		`sh -c 'cd -- "$1" && if [ -d "$2" ]; then exec tar -cf - -- "$2/."; else exec tar -cf - -- "$2"; fi' _ %s %s`,
		shellQuote(parentDir),
		shellQuote(safeBase),
	)
}

func buildCompressCommand(dir string, names []string, format string) (string, string, error) {
	base := names[0]
	if len(names) > 1 {
		base = fmt.Sprintf("archive-%d", time.Now().Unix())
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = shellQuote(safeRelativeName(n))
	}
	args := strings.Join(quoted, " ")
	qDir := shellQuote(dir)

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		archive := base + ".zip"
		install := autoInstallSnippet("zip", "zip", "zip", "zip", "zip", "zip")
		cmd := fmt.Sprintf("cd -- %s && { if ! command -v zip >/dev/null 2>&1; then %s; fi && zip -rq -- %s %s; }", qDir, install, shellQuote(safeRelativeName(archive)), args)
		return archive, cmd, nil
	case "", "tar.gz", "tgz":
		archive := base + ".tar.gz"
		cmd := fmt.Sprintf("cd -- %s && tar -czf %s -- %s", qDir, shellQuote(safeRelativeName(archive)), args)
		return archive, cmd, nil
	default:
		return "", "", fmt.Errorf("不支持的压缩格式: %s", format)
	}
}