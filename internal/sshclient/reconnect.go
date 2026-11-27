package sshclient

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

const maxKeepAliveFailures = 3

var (
	keepAliveProbeTimeout = 10 * time.Second
	errProbeInFlight      = errors.New("keepalive probe already in flight")
)

func (m *Manager) pumpOutput(sessionID string, r io.Reader, rec *CommandRecorder) {
	buf := make([]byte, 16*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if rec != nil {
				rec.FeedOutput(chunk)
			}
			m.emit("terminal:data", map[string]string{
				"sessionId": sessionID,
				"data":      base64.StdEncoding.EncodeToString(chunk),
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

func sendKeepAliveProbe(client *ssh.Client, stopCh <-chan struct{}, timeout time.Duration, inFlight *atomic.Bool) error {
	select {
	case <-stopCh:
		return context.Canceled
	default:
	}
	if inFlight != nil && !inFlight.CompareAndSwap(false, true) {
		return errProbeInFlight
	}
	errCh := make(chan error, 1)
	go func() {
		if inFlight != nil {
			defer inFlight.Store(false)
		}
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
	if interval <= 0 {
		return
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()

	failures := 0
	for {
		select {
		case <-stopCh:
			return
		case <-timer.C:
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
			err := sendKeepAliveProbe(client, stopCh, keepAliveProbeTimeout, &sess.keepAliveInFlight)
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
			timer.Reset(interval)
		}
	}
}
