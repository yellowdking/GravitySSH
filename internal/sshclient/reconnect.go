package sshclient

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

const maxKeepAliveFailures = 3

var keepAliveProbeTimeout = 10 * time.Second

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

func sendKeepAliveProbe(client *ssh.Client, stopCh <-chan struct{}, timeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		errCh <- err
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-stopCh:
		return context.Canceled
	case err := <-errCh:
		return err
	case <-timer.C:
		return errors.New("keepalive probe timed out")
	}
}

// keepAliveLoop sends periodic SSH keepalive probes bounded by keepAliveProbeTimeout
// so blackholed links reliably trip maxKeepAliveFailures without prematurely killing
// high-latency links on a single transient failure.
func (m *Manager) keepAliveLoop(sess *sessionState, stopCh chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	failures := 0
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
			err := sendKeepAliveProbe(client, stopCh, keepAliveProbeTimeout)
			if errors.Is(err, context.Canceled) {
				return
			}
			if err != nil {
				failures++
				if failures >= maxKeepAliveFailures {
					m.markDisconnected(sess, "连接心跳超时，会话已断开")
					return
				}
			} else {
				failures = 0
			}
		}
	}
}
