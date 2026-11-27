package sshclient

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
)

const transferProgressMinInterval = 50 * time.Millisecond

func (m *Manager) emitTransferError(prog models.TransferProgress, err error) error {
	prog.Status = "error"
	prog.Error = err.Error()
	m.emitTransfer(prog)
	return err
}

func (m *Manager) finishTransfer(
	sessionID string,
	usedSC *sftp.Client,
	tracker *progressTracker,
	cpErr, closeErr error,
	invalidateDir string,
) error {
	if cpErr != nil || closeErr != nil {
		finalErr := cpErr
		if finalErr == nil {
			finalErr = closeErr
		}
		if isSFTPConnError(finalErr) {
			m.invalidateSFTP(sessionID, usedSC)
		}
		if invalidateDir != "" {
			m.invalidateDirCache(sessionID, invalidateDir)
		}
		return m.emitTransferError(tracker.snapshot(), finalErr)
	}

	snap := tracker.snapshot()
	snap.Percent = 100
	snap.Status = "done"
	m.emitTransfer(snap)
	if invalidateDir != "" {
		m.invalidateDirCache(sessionID, invalidateDir)
	}
	return nil
}

// SFTPUpload streams a local file to remoteDir atomically via a hidden temp file + rename
// (cleaning up the temp file on failure and falling back to in-place only if temp creation/rename fails).
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
	tmpPath := path.Join(cleanDir, fmt.Sprintf(".%s.gssh-tmp-%s", fileName, uuid.NewString()[:8]))
	prog := models.TransferProgress{
		TransferID: uuid.NewString(),
		SessionID:  sessionID,
		Direction:  "upload",
		FileName:   fileName,
		RemotePath: remotePath,
		LocalPath:  localPath,
		Total:      st.Size(),
		Status:     "running",
	}

	var (
		dst          *sftp.File
		usedSC       *sftp.Client
		usingTemp    bool
		existingMode os.FileMode
	)
	err = m.withSFTP(sessionID, func(sc *sftp.Client) error {
		if existingSt, statErr := sc.Lstat(remotePath); statErr == nil && existingSt.Mode().IsRegular() {
			existingMode = existingSt.Mode().Perm()
		}
		f, oErr := sc.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if oErr != nil {
			if isSFTPConnError(oErr) {
				return oErr
			}
			// Fallback to in-place open when hidden temp file creation is disallowed.
			f2, oErr2 := sc.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
			if oErr2 != nil {
				return oErr2
			}
			dst = f2
			usedSC = sc
			usingTemp = false
			return nil
		}
		dst = f
		usedSC = sc
		usingTemp = true
		return nil
	})
	if err != nil {
		m.invalidateDirCache(sessionID, cleanDir)
		return m.emitTransferError(prog, fmt.Errorf("创建远程文件失败: %w", err))
	}

	tracker := newProgressTracker(m, &prog, m.SessionStopCh(sessionID))
	tracker.emitInitial()
	// Use ReadFromWithConcurrency so every chunk's server write status (including the
	// final <32KB packet, which pkg/sftp's sequential ReadFrom swallows on ErrUnexpectedEOF)
	// is strictly verified before renaming the temp file into place.
	conc := uploadConcurrency(st.Size())
	_, cpErr := dst.ReadFromWithConcurrency(&progressReader{r: src, size: st.Size(), tracker: tracker}, conc)
	closeErr := dst.Close()
	if cpErr != nil || closeErr != nil {
		if usingTemp && usedSC != nil {
			_ = usedSC.Remove(tmpPath)
		}
		return m.finishTransfer(sessionID, usedSC, tracker, cpErr, closeErr, cleanDir)
	}

	if usingTemp && usedSC != nil {
		if existingMode != 0 {
			_ = usedSC.Chmod(tmpPath, existingMode)
		}
		if rErr := renameRemote(usedSC, tmpPath, remotePath, true); rErr != nil {
			_ = usedSC.Remove(tmpPath)
			if isSFTPConnError(rErr) {
				return m.finishTransfer(sessionID, usedSC, tracker, rErr, nil, cleanDir)
			}
			// Fallback to in-place upload if server disallows rename over existing target.
			if _, seekErr := src.Seek(0, io.SeekStart); seekErr == nil {
				if dst2, oErr2 := usedSC.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC); oErr2 == nil {
					tracker.reset()
					_, cpErr2 := dst2.ReadFromWithConcurrency(&progressReader{r: src, size: st.Size(), tracker: tracker}, conc)
					closeErr2 := dst2.Close()
					return m.finishTransfer(sessionID, usedSC, tracker, cpErr2, closeErr2, cleanDir)
				}
			}
			return m.finishTransfer(sessionID, usedSC, tracker, rErr, nil, cleanDir)
		}
	}

	return m.finishTransfer(sessionID, usedSC, tracker, nil, nil, cleanDir)
}

func uploadConcurrency(size int64) int {
	const chunk = 32 * 1024
	if size <= chunk {
		return 1
	}
	n := int(size/chunk) + 1
	if n > sftpMaxConcurrentRequestsPerFile {
		return sftpMaxConcurrentRequestsPerFile
	}
	return n
}

// SFTPDownload streams a remote file directly to localPath (rebuilding a stale SFTP client on open error).
func (m *Manager) SFTPDownload(sessionID, remotePath, localPath string) error {
	cleanRemote := path.Clean(remotePath)
	fileName := path.Base(cleanRemote)
	prog := models.TransferProgress{
		TransferID: uuid.NewString(),
		SessionID:  sessionID,
		Direction:  "download",
		FileName:   fileName,
		RemotePath: cleanRemote,
		LocalPath:  localPath,
		Status:     "running",
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	var (
		src    *sftp.File
		usedSC *sftp.Client
		total  int64
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
		usedSC = sc
		total = st.Size()
		return nil
	})
	if err != nil {
		return m.emitTransferError(prog, err)
	}
	defer src.Close()

	prog.Total = total
	dst, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return m.emitTransferError(prog, fmt.Errorf("创建本地文件失败: %w", err))
	}

	tracker := newProgressTracker(m, &prog, m.SessionStopCh(sessionID))
	tracker.emitInitial()
	// Wrap dst as plain io.Writer (not io.ReaderFrom) so io.Copy invokes src.(*sftp.File).WriteTo
	// and engages pkg/sftp pipelined concurrent reads.
	_, cpErr := io.Copy(&progressWriter{w: dst, tracker: tracker}, src)
	closeErr := dst.Close()
	if cpErr != nil || closeErr != nil {
		_ = os.Remove(localPath)
	}
	return m.finishTransfer(sessionID, usedSC, tracker, cpErr, closeErr, "")
}

type progressTracker struct {
	m           *Manager
	mu          sync.Mutex
	prog        *models.TransferProgress
	stopCh      <-chan struct{}
	lastPercent int
	lastEmit    time.Time
}

func newProgressTracker(m *Manager, prog *models.TransferProgress, stopCh <-chan struct{}) *progressTracker {
	return &progressTracker{
		m:           m,
		prog:        prog,
		stopCh:      stopCh,
		lastPercent: -1,
	}
}

func (p *progressTracker) checkCanceled() error {
	if p.stopCh == nil {
		return nil
	}
	select {
	case <-p.stopCh:
		return errors.New("会话已断开，传输已取消")
	default:
		return nil
	}
}

func (p *progressTracker) emitInitial() {
	p.mu.Lock()
	if p.prog.Total > 0 {
		p.prog.Percent = int(p.prog.Transferred * 100 / p.prog.Total)
		p.lastPercent = p.prog.Percent
	}
	p.lastEmit = time.Now()
	snap := *p.prog
	p.mu.Unlock()
	p.m.emitTransfer(snap)
}

func (p *progressTracker) add(n int64) {
	if n <= 0 {
		return
	}
	var (
		shouldEmit bool
		snap       models.TransferProgress
	)
	now := time.Now()
	p.mu.Lock()
	p.prog.Transferred += n
	if p.prog.Total > 0 {
		pct := int(p.prog.Transferred * 100 / p.prog.Total)
		if pct > 100 {
			pct = 100
		}
		if pct != p.lastPercent && (pct == 100 || now.Sub(p.lastEmit) >= transferProgressMinInterval) {
			p.lastPercent = pct
			p.prog.Percent = pct
			p.lastEmit = now
			snap = *p.prog
			shouldEmit = true
		}
	} else if now.Sub(p.lastEmit) >= 100*time.Millisecond {
		p.lastEmit = now
		snap = *p.prog
		shouldEmit = true
	}
	p.mu.Unlock()
	if shouldEmit {
		p.m.emitTransfer(snap)
	}
}

func (p *progressTracker) reset() {
	p.mu.Lock()
	p.prog.Transferred = 0
	p.prog.Percent = 0
	p.lastPercent = 0
	p.mu.Unlock()
}

func (p *progressTracker) snapshot() models.TransferProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return *p.prog
}

type progressReader struct {
	r       io.Reader
	size    int64
	tracker *progressTracker
}

// Size reports the known total length so pkg/sftp (*File).ReadFrom can enable
// pipelined concurrent writes even when invoked via io.Copy.
func (pr *progressReader) Size() int64 {
	return pr.size
}

func (pr *progressReader) Read(p []byte) (int, error) {
	if err := pr.tracker.checkCanceled(); err != nil {
		return 0, err
	}
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.tracker.add(int64(n))
	}
	return n, err
}

type progressWriter struct {
	w       io.Writer
	tracker *progressTracker
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	if err := pw.tracker.checkCanceled(); err != nil {
		return 0, err
	}
	n, err := pw.w.Write(p)
	if n > 0 {
		pw.tracker.add(int64(n))
	}
	return n, err
}
