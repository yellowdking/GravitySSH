package sshclient

import (
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

// terminalModeResetSeq clears stale alternate-screen, mouse tracking, and bracketed-paste modes
// left behind by an abruptly dropped full-screen program (nano/vim/htop) before reconnecting.
const terminalModeResetSeq = "\x1b[?1049l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\x1b[0m\r\n"

// WakeReconnect signals all currently reconnecting sessions to retry immediately
// (e.g. when Windows wakes from sleep or network comes back online).
func (m *Manager) WakeReconnect() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sess := range m.sessions {
		select {
		case sess.wakeCh <- struct{}{}:
		default:
		}
	}
}

func (m *Manager) shouldAutoReconnect(sess *sessionState) bool {
	if m.store == nil || !m.store.GetSettings().AutoReconnect {
		return false
	}
	sess.mu.Lock()
	uc := sess.userClosed
	sess.mu.Unlock()
	return !uc
}

func (m *Manager) resetSessionTransport(sess *sessionState) {
	sess.mu.Lock()
	if sess.stopCh != nil {
		select {
		case <-sess.stopCh:
		default:
			close(sess.stopCh)
		}
	}
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
	sess.stdin = nil
	sess.prevCPU = nil
	sess.prevNet = nil
	sess.info.SFTPReady = false
	sess.mu.Unlock()
}

func (m *Manager) runReconnectLoop(sess *sessionState, serverID string) {
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	m.sendTerminalText(sess.info.SessionID, terminalModeResetSeq)

	for attempt := 1; ; attempt++ {
		sess.mu.Lock()
		if sess.userClosed {
			sess.mu.Unlock()
			return
		}
		sess.info.Status = "reconnecting"
		sess.info.Error = fmt.Sprintf("连接中断，%d 秒后自动重连（第 %d 次）", int(backoff.Seconds()), attempt)
		snap := sess.info
		sess.mu.Unlock()

		m.emit("session:status", snap)
		m.sendTerminalText(
			sess.info.SessionID,
			fmt.Sprintf("\x1b[38;5;214m↻ 网络断开，%d 秒后自动重连（第 %d 次）...\x1b[0m\r\n", int(backoff.Seconds()), attempt),
		)

		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-sess.wakeCh:
			timer.Stop()
			backoff = 1 * time.Second
			m.sendTerminalText(sess.info.SessionID, "\x1b[38;5;111m⚡ 检测到网络恢复/唤醒，立即重连...\x1b[0m\r\n")
		}

		sess.mu.Lock()
		if sess.userClosed {
			sess.mu.Unlock()
			return
		}
		cols, rows := sess.lastCols, sess.lastRows
		ctx := sess.appCtx
		sess.stopCh = make(chan struct{})
		sess.mu.Unlock()

		srv, err := m.store.GetServerInternal(serverID)
		if err != nil {
			m.markDisconnected(sess, "服务器配置已删除，停止重连")
			return
		}

		if m.establishOnce(ctx, sess, srv, cols, rows, true) {
			return
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
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

func (m *Manager) keepAliveLoop(sess *sessionState, stopCh chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			sess.mu.Lock()
			var client *ssh.Client
			if sess.dialRes != nil {
				client = sess.dialRes.Client
			}
			closed := sess.closed || sess.userClosed
			sess.mu.Unlock()
			if closed || client == nil {
				return
			}
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				m.resetSessionTransport(sess)
				return
			}
		}
	}
}
