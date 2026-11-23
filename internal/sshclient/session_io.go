package sshclient

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/pkg/sftp"
	"github.com/yellowdking/GravitySSH/internal/models"
	"golang.org/x/crypto/ssh"
)

func (m *Manager) establishSession(ctx context.Context, sess *sessionState, srv *models.ServerConfig, cols, rows int) {
	m.sendTerminalText(sess.info.SessionID, fmt.Sprintf("\x1b[38;5;105m● 正在连接 %s (%s@%s:%d)...\x1b[0m\r\n", srv.Name, srv.Username, srv.Host, defaultPort(srv.Port)))

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

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = sshSess.Close()
		dialRes.Close()
		m.failSession(sess, fmt.Sprintf("请求 PTY 终端失败: %v", err))
		return
	}

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

	var sftpClient *sftp.Client
	homeDir := srv.DefaultRemotePath
	sftpReady := false
	if sc, scErr := sftp.NewClient(dialRes.Client); scErr == nil {
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

	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		if sftpClient != nil {
			_ = sftpClient.Close()
		}
		_ = sshSess.Close()
		dialRes.Close()
		return
	}
	sess.dialRes = dialRes
	sess.sshSession = sshSess
	sess.stdin = stdin
	sess.sftpClient = sftpClient
	sess.info.Status = "connected"
	sess.info.SFTPReady = sftpReady
	sess.info.HomeDir = homeDir
	sess.info.ViaSummary = dialRes.ViaSummary
	infoSnap := sess.info
	sess.mu.Unlock()

	m.store.MarkServerConnected(srv.ID)

	if dialRes.ViaSummary != "" {
		m.sendTerminalText(sess.info.SessionID, fmt.Sprintf("\x1b[38;5;78m✓ 已连接 · 链路: %s → %s\x1b[0m\r\n", dialRes.ViaSummary, srv.Host))
	} else {
		m.sendTerminalText(sess.info.SessionID, "\x1b[38;5;78m✓ SSH 与 SFTP 已自动就绪\x1b[0m\r\n")
	}
	m.emit("session:status", infoSnap)

	if srv.StartupCommand != "" {
		go func() {
			time.Sleep(450 * time.Millisecond)
			_ = m.WriteInput(sess.info.SessionID, srv.StartupCommand+"\n")
		}()
	}

	go m.pumpOutput(sess.info.SessionID, stdout)
	go m.pumpOutput(sess.info.SessionID, stderr)

	kaSec := srv.KeepAliveSeconds
	if kaSec <= 0 {
		kaSec = 30
	}
	go m.keepAliveLoop(sess, time.Duration(kaSec)*time.Second)

	_ = sshSess.Wait()
	m.markDisconnected(sess, "远程会话已结束")
}

func (m *Manager) pumpOutput(sessionID string, r io.Reader) {
	buf := make([]byte, 16*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			m.emit("terminal:data", map[string]string{
				"sessionId": sessionID,
				"data":      base64.StdEncoding.EncodeToString(buf[:n]),
			})
		}
		if err != nil {
			return
		}
	}
}

func (m *Manager) sendTerminalText(sessionID, text string) {
	m.emit("terminal:data", map[string]string{
		"sessionId": sessionID,
		"data":      base64.StdEncoding.EncodeToString([]byte(text)),
	})
}

func (m *Manager) keepAliveLoop(sess *sessionState, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-sess.stopCh:
			return
		case <-ticker.C:
			sess.mu.Lock()
			var client *ssh.Client
			if sess.dialRes != nil {
				client = sess.dialRes.Client
			}
			closed := sess.closed
			sess.mu.Unlock()
			if closed || client == nil {
				return
			}
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				m.markDisconnected(sess, "心跳保活超时，连接已断开")
				return
			}
		}
	}
}

func (m *Manager) failSession(sess *sessionState, errMsg string) {
	sess.mu.Lock()
	sess.info.Status = "error"
	sess.info.Error = errMsg
	infoSnap := sess.info
	sess.mu.Unlock()
	m.sendTerminalText(sess.info.SessionID, fmt.Sprintf("\r\n\x1b[38;5;203m✗ 连接失败: %s\x1b[0m\r\n", errMsg))
	m.emit("session:status", infoSnap)
}

func (m *Manager) markDisconnected(sess *sessionState, reason string) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	close(sess.stopCh)
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

	m.sendTerminalText(sess.info.SessionID, fmt.Sprintf("\r\n\x1b[38;5;245m● %s\x1b[0m\r\n", reason))
	m.emit("session:status", infoSnap)
}

func (m *Manager) WriteInput(sessionID, data string) error {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话不存在: %s", sessionID)
	}
	sess.mu.Lock()
	stdin := sess.stdin
	rec := sess.recorder
	sess.mu.Unlock()
	if stdin == nil {
		return errors.New("终端尚未就绪")
	}
	raw := []byte(data)
	if _, err := stdin.Write(raw); err != nil {
		return err
	}
	if rec != nil {
		rec.Feed(raw)
	}
	return nil
}

func (m *Manager) CurrentInputLine(sessionID string) string {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok || sess.recorder == nil {
		return ""
	}
	return sess.recorder.CurrentLine()
}

func (m *Manager) Resize(sessionID string, cols, rows int) error {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	sess.mu.Lock()
	sshSess := sess.sshSession
	sess.mu.Unlock()
	if sshSess == nil || cols <= 0 || rows <= 0 {
		return nil
	}
	return sshSess.WindowChange(rows, cols)
}

func (m *Manager) Disconnect(sessionID string) {
	m.mu.Lock()
	sess, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	if ok {
		m.markDisconnected(sess, "已主动断开连接")
	}
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*sessionState, 0, len(m.sessions))
	for id, s := range m.sessions {
		all = append(all, s)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	for _, s := range all {
		m.markDisconnected(s, "应用关闭")
	}
}
