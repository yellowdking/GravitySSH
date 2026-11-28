package sshclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	stopCh := sess.stopCh
	sess.mu.Unlock()
	if stdin == nil {
		return errors.New("终端尚未就绪")
	}
	raw := []byte(data)
	var emitted []string
	sess.writeMu.Lock()
	for offset := 0; offset < len(raw); offset += writeChunkSize {
		select {
		case <-stopCh:
			sess.writeMu.Unlock()
			return errors.New("会话已断开")
		default:
		}
		end := offset + writeChunkSize
		if end > len(raw) {
			end = len(raw)
		}
		if _, err := stdin.Write(raw[offset:end]); err != nil {
			sess.writeMu.Unlock()
			return err
		}
		if end < len(raw) {
			timer := time.NewTimer(5 * time.Millisecond)
			select {
			case <-stopCh:
				timer.Stop()
				sess.writeMu.Unlock()
				return errors.New("会话已断开")
			case <-timer.C:
			}
		}
	}
	if rec != nil {
		emitted = rec.FeedInput(raw)
	}
	sess.writeMu.Unlock()

	if rec != nil && len(emitted) > 0 {
		rec.DispatchCommands(emitted)
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

// sessionExecContext creates a context bounded by timeout (if > 0) that cancels
// immediately when the SSH session's stopCh closes, without leaking watcher goroutines.
func (m *Manager) sessionExecContext(sessionID string, timeout time.Duration) (context.Context, context.CancelFunc) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	stopCh := m.SessionStopCh(sessionID)
	if stopCh == nil {
		return ctx, cancel
	}
	select {
	case <-stopCh:
		cancel()
		return ctx, cancel
	default:
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		select {
		case <-stopCh:
			cancel()
		case <-ctx.Done():
		case <-done:
		}
	}()
	return ctx, func() {
		once.Do(func() {
			close(done)
			cancel()
		})
	}
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
