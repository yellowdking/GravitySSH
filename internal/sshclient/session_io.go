package sshclient

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
	"golang.org/x/crypto/ssh"
)

type shellHandles struct {
	sshSess *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  io.Reader
}

// emitSessionStatus serializes session:status emissions per session and guarantees
// that once a terminal status ("disconnected"/"error") is emitted, no late
// non-terminal status ("connecting"/"connected") can overwrite it.
func (m *Manager) emitSessionStatus(sess *sessionState, info models.SessionInfo, terminal bool, beforeEmit func()) bool {
	sess.statusMu.Lock()
	defer sess.statusMu.Unlock()
	if sess.terminalStatusEmitted {
		return false
	}
	if !terminal {
		sess.mu.Lock()
		closed := sess.closed || sess.userClosed
		sess.mu.Unlock()
		if closed {
			return false
		}
	} else {
		sess.terminalStatusEmitted = true
	}
	if beforeEmit != nil {
		beforeEmit()
	}
	m.emit("session:status", info)
	return true
}

func (m *Manager) establishSession(ctx context.Context, sess *sessionState, srv *models.ServerConfig, cols, rows int) {
	defer func() {
		sess.mu.Lock()
		cancel := sess.cancelDial
		sess.cancelDial = nil
		sess.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}()

	sess.mu.Lock()
	sessionID := sess.info.SessionID
	ptyCols, ptyRows := sess.lastCols, sess.lastRows
	sess.mu.Unlock()
	if ptyCols <= 0 {
		ptyCols = cols
	}
	if ptyRows <= 0 {
		ptyRows = rows
	}

	m.sendTerminalText(sessionID, fmt.Sprintf("\x1b[38;5;105m● 正在连接 %s (%s@%s:%d)...\x1b[0m\r\n", srv.Name, srv.Username, srv.Host, defaultPort(srv.Port)))

	dialRes, err := DialServer(ctx, srv, m.store)
	if err != nil {
		m.failSession(sess, err.Error())
		return
	}

	sess.mu.Lock()
	if sess.userClosed || sess.closed {
		sess.mu.Unlock()
		dialRes.Close()
		return
	}
	sess.pendingDial = dialRes
	sess.mu.Unlock()

	shellTimeout := time.Duration(srv.TimeoutSeconds) * time.Second
	if shellTimeout <= 0 {
		shellTimeout = 15 * time.Second
	}
	shellCtx, cancelShell := context.WithTimeout(ctx, shellTimeout)
	sh, err := openShellSession(shellCtx, dialRes, ptyCols, ptyRows)
	cancelShell()
	if err != nil {
		sess.mu.Lock()
		if sess.pendingDial == dialRes {
			sess.pendingDial = nil
		}
		sess.mu.Unlock()
		dialRes.Close()
		m.failSession(sess, err.Error())
		return
	}

	homeDir := srv.DefaultRemotePath
	if homeDir == "" {
		homeDir = "/"
	}

	sess.mu.Lock()
	if sess.userClosed || sess.closed {
		if sess.pendingDial == dialRes {
			sess.pendingDial = nil
		}
		sess.mu.Unlock()
		_ = sh.sshSess.Close()
		dialRes.Close()
		return
	}
	latestCols, latestRows := sess.lastCols, sess.lastRows
	sess.mu.Unlock()

	if latestCols > 0 && latestRows > 0 && (latestCols != ptyCols || latestRows != ptyRows) {
		_ = sh.sshSess.WindowChange(latestRows, latestCols)
		ptyCols, ptyRows = latestCols, latestRows
	}

	sess.mu.Lock()
	if sess.pendingDial == dialRes {
		sess.pendingDial = nil
	}
	if sess.userClosed || sess.closed {
		sess.mu.Unlock()
		_ = sh.sshSess.Close()
		dialRes.Close()
		return
	}
	finalCols, finalRows := sess.lastCols, sess.lastRows
	sess.closed = false
	sess.dialRes = dialRes
	sess.sshSession = sh.sshSess
	sess.stdin = sh.stdin
	sess.info.Status = "connected"
	sess.info.SFTPReady = false
	sess.info.HomeDir = homeDir
	sess.info.Error = ""
	sess.info.ViaSummary = dialRes.ViaSummary
	if sess.currentDir == "" {
		sess.currentDir = homeDir
	}
	infoSnap := sess.info
	stopCh := sess.stopCh
	rec := sess.recorder
	sess.mu.Unlock()

	connectedBanner := "\x1b[38;5;78m✓ SSH 已连接\x1b[0m\r\n"
	if dialRes.ViaSummary != "" {
		connectedBanner = fmt.Sprintf("\x1b[38;5;78m✓ 已连接 · 链路: %s → %s\x1b[0m\r\n", dialRes.ViaSummary, srv.Host)
	}
	if !m.emitSessionStatus(sess, infoSnap, false, func() {
		m.sendTerminalText(sessionID, connectedBanner)
	}) {
		_ = sh.sshSess.Close()
		dialRes.Close()
		return
	}

	// Re-check before spawning background loops in case Disconnect raced right after status emission.
	sess.mu.Lock()
	if sess.closed || sess.userClosed {
		sess.mu.Unlock()
		_ = sh.sshSess.Close()
		dialRes.Close()
		return
	}
	sess.mu.Unlock()

	if finalCols > 0 && finalRows > 0 && (finalCols != ptyCols || finalRows != ptyRows) {
		go func() { _ = sh.sshSess.WindowChange(finalRows, finalCols) }()
	}
	if m.store != nil {
		go m.store.MarkServerConnected(srv.ID)
	}

	go m.pumpOutput(sessionID, sh.stdout, rec)
	go m.pumpOutput(sessionID, sh.stderr, rec)
	go m.initSessionSFTP(sess, dialRes.Client, srv.DefaultRemotePath, stopCh)

	if srv.StartupCommand != "" {
		go m.scheduleStartupCommand(sessionID, srv.StartupCommand, stopCh)
	}

	kaSec := srv.KeepAliveSeconds
	if kaSec <= 0 {
		kaSec = 30
	}
	go m.keepAliveLoop(sess, stopCh, time.Duration(kaSec)*time.Second)

	go func() {
		_ = sh.sshSess.Wait()
		m.markDisconnected(sess, "远程会话已结束")
	}()
}

func openShellSession(ctx context.Context, dialRes *DialResult, cols, rows int) (*shellHandles, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("打开远程 Shell 已取消: %w", err)
	}
	stopCancel := context.AfterFunc(ctx, func() {
		dialRes.Close()
	})
	abort := func(sshSess *ssh.Session, prefix string, err error) (*shellHandles, error) {
		stopped := stopCancel()
		if sshSess != nil {
			_ = sshSess.Close()
		}
		if !stopped || ctx.Err() != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s: %w", prefix, ctx.Err())
			}
			return nil, fmt.Errorf("%s: %w", prefix, context.Canceled)
		}
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}

	sshSess, err := dialRes.Client.NewSession()
	if err != nil {
		return abort(nil, "创建 SSH 会话失败", err)
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		return abort(sshSess, "请求 PTY 终端失败", err)
	}
	_ = sshSess.Setenv("LANG", "en_US.UTF-8")
	_ = sshSess.Setenv("LC_CTYPE", "en_US.UTF-8")
	if err := ctx.Err(); err != nil {
		return abort(sshSess, "初始化终端环境失败", err)
	}

	stdin, err := sshSess.StdinPipe()
	if err != nil {
		return abort(sshSess, "获取 stdin 失败", err)
	}
	stdout, err := sshSess.StdoutPipe()
	if err != nil {
		return abort(sshSess, "获取 stdout 失败", err)
	}
	stderr, err := sshSess.StderrPipe()
	if err != nil {
		return abort(sshSess, "获取 stderr 失败", err)
	}
	if err := sshSess.Shell(); err != nil {
		return abort(sshSess, "启动远程 Shell 失败", err)
	}
	stopped := stopCancel()
	if !stopped || ctx.Err() != nil {
		_ = sshSess.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("启动远程 Shell 失败: %w", ctx.Err())
		}
		return nil, fmt.Errorf("启动远程 Shell 失败: %w", context.Canceled)
	}
	return &shellHandles{sshSess: sshSess, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (m *Manager) initSessionSFTP(sess *sessionState, client *ssh.Client, defaultRemotePath string, stopCh <-chan struct{}) {
	select {
	case <-stopCh:
		return
	default:
	}

	sc, scErr := newSFTPClient(client)
	if scErr != nil {
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
		sc, scErr = newSFTPClient(client)
	}
	if scErr != nil {
		return
	}

	homeDir := defaultRemotePath
	if homeDir == "" {
		if wd, wdErr := sc.Getwd(); wdErr == nil && wd != "" {
			homeDir = wd
		} else {
			homeDir = "/"
		}
	}

	sess.mu.Lock()
	if sess.closed || sess.userClosed || sess.dialRes == nil || sess.dialRes.Client != client {
		sess.mu.Unlock()
		_ = sc.Close()
		return
	}
	if sess.sftpClient != nil {
		_ = sc.Close()
	} else {
		sess.sftpClient = sc
	}
	sess.info.SFTPReady = true
	sess.info.HomeDir = homeDir
	if sess.currentDir == "" || (defaultRemotePath == "" && sess.currentDir == "/") {
		sess.currentDir = homeDir
	}
	cwd := sess.currentDir
	infoSnap := sess.info
	sess.mu.Unlock()

	if m.emitSessionStatus(sess, infoSnap, false, nil) && cwd != "" {
		m.emit("session:cwd", map[string]string{"sessionId": infoSnap.SessionID, "path": cwd})
	}
}

func (m *Manager) scheduleStartupCommand(sessionID, cmd string, stopCh <-chan struct{}) {
	timer := time.NewTimer(450 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-stopCh:
		return
	case <-timer.C:
		_ = m.WriteInput(sessionID, cmd+"\n")
	}
}

func closeStopChLocked(sess *sessionState) {
	if sess.stopCh != nil {
		select {
		case <-sess.stopCh:
		default:
			close(sess.stopCh)
		}
	}
}

func clearSessionRuntimeLocked(sess *sessionState) {
	sess.stdin = nil
	sess.recorder = nil
	sess.dirCache = nil
	sess.dirCacheGen++
	sess.prevCPU = nil
	sess.prevNet = nil
	sess.prevSampleAt = time.Time{}
}

func (m *Manager) deleteSessionEntry(sessionID string, sess *sessionState) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	if cur, ok := m.sessions[sessionID]; ok && cur == sess {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
}

func (m *Manager) failSession(sess *sessionState, errMsg string) {
	sess.mu.Lock()
	if sess.userClosed || sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	pendingDial := sess.pendingDial
	sess.pendingDial = nil
	closeStopChLocked(sess)
	clearSessionRuntimeLocked(sess)
	sess.info.Status = "error"
	sess.info.SFTPReady = false
	sess.info.Error = errMsg
	infoSnap := sess.info
	sess.mu.Unlock()

	if pendingDial != nil {
		pendingDial.Close()
	}
	m.deleteSessionEntry(infoSnap.SessionID, sess)
	m.emitSessionStatus(sess, infoSnap, true, func() {
		m.sendTerminalText(infoSnap.SessionID, fmt.Sprintf("\r\n\x1b[38;5;203m✗ 连接失败: %s\x1b[0m\r\n", errMsg))
	})
}

func (m *Manager) markDisconnected(sess *sessionState, reason string) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	cancelDial := sess.cancelDial
	sess.cancelDial = nil
	pendingDial := sess.pendingDial
	sess.pendingDial = nil
	sftpClient := sess.sftpClient
	sess.sftpClient = nil
	sshSession := sess.sshSession
	sess.sshSession = nil
	dialRes := sess.dialRes
	sess.dialRes = nil
	closeStopChLocked(sess)
	clearSessionRuntimeLocked(sess)
	if sess.info.Status != "error" {
		sess.info.Status = "disconnected"
		sess.info.SFTPReady = false
		sess.info.Error = reason
	}
	infoSnap := sess.info
	sess.mu.Unlock()

	if cancelDial != nil {
		cancelDial()
	}
	if sftpClient != nil {
		_ = sftpClient.Close()
	}
	if sshSession != nil {
		_ = sshSession.Close()
	}
	if dialRes != nil {
		dialRes.Close()
	}
	if pendingDial != nil && pendingDial != dialRes {
		pendingDial.Close()
	}
	m.deleteSessionEntry(infoSnap.SessionID, sess)
	m.emitSessionStatus(sess, infoSnap, true, func() {
		m.sendTerminalText(infoSnap.SessionID, fmt.Sprintf("\r\n\x1b[38;5;245m● %s\x1b[0m\r\n", reason))
	})
}
