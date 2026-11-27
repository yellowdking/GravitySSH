package sshclient

import (
	"errors"
	"fmt"
	"time"
)

const writeChunkSize = 1024

func (m *Manager) WriteInput(sessionID, data string) error {
	sess := m.getSession(sessionID)
	if sess == nil {
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
	sess.writeMu.Lock()
	defer sess.writeMu.Unlock()
	for offset := 0; offset < len(raw); offset += writeChunkSize {
		end := offset + writeChunkSize
		if end > len(raw) {
			end = len(raw)
		}
		if _, err := stdin.Write(raw[offset:end]); err != nil {
			return err
		}
		if end < len(raw) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if rec != nil {
		rec.Feed(raw)
	}
	return nil
}

// SessionStopCh returns the session's cancellation channel, or nil if unknown.
func (m *Manager) SessionStopCh(sessionID string) <-chan struct{} {
	if sess := m.getSession(sessionID); sess != nil {
		sess.mu.Lock()
		ch := sess.stopCh
		sess.mu.Unlock()
		return ch
	}
	return nil
}

// Resize records the latest terminal dimensions even while connecting,
// and forwards WindowChange to the live SSH session once ready.
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	sess := m.getSession(sessionID)
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	sess.lastCols = cols
	sess.lastRows = rows
	sshSess := sess.sshSession
	sess.mu.Unlock()
	if sshSess == nil {
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
		sess.mu.Lock()
		sess.userClosed = true
		cancelDial := sess.cancelDial
		sess.cancelDial = nil
		sess.mu.Unlock()
		if cancelDial != nil {
			cancelDial()
		}
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
		s.mu.Lock()
		s.userClosed = true
		cancelDial := s.cancelDial
		s.cancelDial = nil
		s.mu.Unlock()
		if cancelDial != nil {
			cancelDial()
		}
		m.markDisconnected(s, "应用关闭")
	}
}
