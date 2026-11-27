package sshclient

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
	"golang.org/x/crypto/ssh"
)

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
	sess.mu.Unlock()

	m.sendTerminalText(sessionID, fmt.Sprintf("\x1b[38;5;105m● 正在连接 %s (%s@%s:%d)...\x1b[0m\r\n", srv.Name, srv.Username, srv.Host, defaultPort(srv.Port)))

	dialRes, err := DialServer(ctx, srv, m.store)
	if err != nil {
		m.failSession(sess, err.Error())
		return
	}

	sshSess, err := dialRes.Client.NewSession()
	if err != nil {
		dialRes.Close()
		m.failSession(sess, fmt.Sprintf("创建 SSH 会话失败: %v", err))
		return
	}

	sess.mu.Lock()
	ptyCols, ptyRows := sess.lastCols, sess.lastRows
	sess.mu.Unlock()
	if ptyCols <= 0 {
		ptyCols = cols
	}
	if ptyRows <= 0 {
		ptyRows = rows
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSess.RequestPty("xterm-256color", ptyRows, ptyCols, modes); err != nil {
		_ = sshSess.Close()
		dialRes.Close()
		m.failSession(sess, fmt.Sprintf("请求 PTY 终端失败: %v", err))
		return
	}

	_ = sshSess.Setenv("LANG", "en_US.UTF-8")
	_ = sshSess.Setenv("LC_CTYPE", "en_US.UTF-8")

	stdin, err := sshSess.StdinPipe()
	if err != nil {
		_ = sshSess.Close()
		dialRes.Close()
		m.failSession(sess, fmt.Sprintf("获取 stdin 失败: %v", err))
		return
	}
	stdout, err := sshSess.StdoutPipe()
	if err != nil {
		_ = sshSess.Close()
		dialRes.Close()
		m.failSession(sess, fmt.Sprintf("获取 stdout 失败: %v", err))
		return
	}
	stderr, err := sshSess.StderrPipe()
	if err != nil {
		_ = sshSess.Close()
		dialRes.Close()
		m.failSession(sess, fmt.Sprintf("获取 stderr 失败: %v", err))
		return
	}

	if err := sshSess.Shell(); err != nil {
		_ = sshSess.Close()
		dialRes.Close()
		m.failSession(sess, fmt.Sprintf("启动远程 Shell 失败: %v", err))
		return
	}

	sess.mu.Lock()
	latestCols, latestRows := sess.lastCols, sess.lastRows
	sess.mu.Unlock()
	if latestCols > 0 && latestRows > 0 && (latestCols != ptyCols || latestRows != ptyRows) {
		_ = sshSess.WindowChange(latestRows, latestCols)
	}

	var sftpClient *sftp.Client
	homeDir := srv.DefaultRemotePath
	sftpReady := false
	sc, scErr := newSFTPClient(dialRes.Client)
	if scErr != nil {
		time.Sleep(300 * time.Millisecond)
		sc, scErr = newSFTPClient(dialRes.Client)
	}
	if scErr == nil {
		sftpClient = sc
		sftpReady = true
		if homeDir == "" {
			if wd, wdErr := sc.Getwd(); wdErr == nil && wd != "" {
				homeDir = wd
			} else {
				homeDir = "/"
			}
		}
	}
	if homeDir == "" {
		homeDir = "/"
	}

	sess.mu.Lock()
	if sess.userClosed {
		sess.mu.Unlock()
		if sftpClient != nil {
			_ = sftpClient.Close()
		}
		_ = sshSess.Close()
		dialRes.Close()
		return
	}
	sess.closed = false
	sess.dialRes = dialRes
	sess.sshSession = sshSess
	sess.stdin = stdin
	sess.sftpClient = sftpClient
	sess.info.Status = "connected"
	sess.info.SFTPReady = sftpReady
	sess.info.HomeDir = homeDir
	sess.info.Error = ""
	sess.info.ViaSummary = dialRes.ViaSummary
	if sess.currentDir == "" {
		sess.currentDir = homeDir
	}
	infoSnap := sess.info
	stopCh := sess.stopCh
	sess.mu.Unlock()

	m.store.MarkServerConnected(srv.ID)

	if dialRes.ViaSummary != "" {
		m.sendTerminalText(sessionID, fmt.Sprintf("\x1b[38;5;78m✓ 已连接 · 链路: %s → %s\x1b[0m\r\n", dialRes.ViaSummary, srv.Host))
	} else {
		m.sendTerminalText(sessionID, "\x1b[38;5;78m✓ SSH 与 SFTP 已自动就绪\x1b[0m\r\n")
	}
	m.emit("session:status", infoSnap)

	if srv.StartupCommand != "" {
		go func() {
			time.Sleep(450 * time.Millisecond)
			_ = m.WriteInput(sessionID, srv.StartupCommand+"\n")
		}()
	}

	go m.pumpOutput(sessionID, stdout)
	go m.pumpOutput(sessionID, stderr)

	kaSec := srv.KeepAliveSeconds
	if kaSec <= 0 {
		kaSec = 30
	}
	go m.keepAliveLoop(sess, stopCh, time.Duration(kaSec)*time.Second)

	go func() {
		_ = sshSess.Wait()
		m.markDisconnected(sess, "远程会话已结束")
	}()
}

func (m *Manager) failSession(sess *sessionState, errMsg string) {
	sess.mu.Lock()
	if sess.userClosed || sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.info.Status = "error"
	sess.info.Error = errMsg
	infoSnap := sess.info
	sess.mu.Unlock()
	m.sendTerminalText(infoSnap.SessionID, fmt.Sprintf("\r\n\x1b[38;5;203m✗ 连接失败: %s\x1b[0m\r\n", errMsg))
	m.emit("session:status", infoSnap)
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
	if sess.stopCh != nil {
		select {
		case <-sess.stopCh:
		default:
			close(sess.stopCh)
		}
	}
	sess.stdin = nil
	if sess.sftpClient != nil {
		_ = sess.sftpClient.Close()
		sess.sftpClient = nil
	}
	if sess.sshSession != nil {
		_ = sess.sshSession.Close()
		sess.sshSession = nil
	}
	if sess.dialRes != nil {
		sess.dialRes.Close()
		sess.dialRes = nil
	}
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

	m.sendTerminalText(infoSnap.SessionID, fmt.Sprintf("\r\n\x1b[38;5;245m● %s\x1b[0m\r\n", reason))
	m.emit("session:status", infoSnap)
}
