package sshclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const sftpMaxConcurrentRequestsPerFile = 64

func newSFTPClient(cli *ssh.Client) (*sftp.Client, error) {
	return sftp.NewClient(
		cli,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(sftpMaxConcurrentRequestsPerFile),
	)
}

// newSessionWithContext opens an SSH session bounded by ctx so a stalled SSH
// transport cannot block callers indefinitely. If ctx expires before NewSession
// returns, any eventually opened session is closed cleanly.
func newSessionWithContext(ctx context.Context, client *ssh.Client) (*ssh.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		sess *ssh.Session
		err  error
	}
	ch := make(chan result)
	go func() {
		sess, err := client.NewSession()
		select {
		case ch <- result{sess: sess, err: err}:
		case <-ctx.Done():
			if sess != nil {
				_ = sess.Close()
			}
		}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, res.err
		}
		if err := ctx.Err(); err != nil {
			_ = res.sess.Close()
			return nil, err
		}
		return res.sess, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// abortSSHSession sends SIGKILL and closes the SSH session asynchronously so
// callers can return immediately even on a blackholed link or stuck remote command.
func abortSSHSession(sess *ssh.Session) {
	if sess == nil {
		return
	}
	go func() {
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
	}()
}

// runSSHExec executes cmd over client with context and optional timeout cancellation,
// returning promptly on cancellation/timeout even if the remote peer stalls or blackholes.
func runSSHExec(ctx context.Context, client *ssh.Client, cmd string, timeout time.Duration, combined bool) ([]byte, error) {
	if client == nil {
		return nil, errors.New("SSH 连接未就绪")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	execCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	sshSess, err := newSessionWithContext(execCtx, client)
	if err != nil {
		return nil, err
	}

	type execResult struct {
		out []byte
		err error
	}
	resCh := make(chan execResult, 1)
	go func() {
		defer sshSess.Close()
		var (
			out    []byte
			runErr error
		)
		if combined {
			out, runErr = sshSess.CombinedOutput(cmd)
		} else {
			out, runErr = sshSess.Output(cmd)
		}
		resCh <- execResult{out: out, err: runErr}
	}()

	select {
	case res := <-resCh:
		if res.err != nil && execCtx.Err() != nil {
			return res.out, execCtx.Err()
		}
		return res.out, res.err
	case <-execCtx.Done():
		abortSSHSession(sshSess)
		return nil, execCtx.Err()
	}
}

func tailUTF8Safe(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func formatExecError(prefix string, out []byte, runErr error, maxLen int) error {
	snippet := strings.TrimSpace(string(out))
	if maxLen > 0 && len(snippet) > maxLen {
		snippet = tailUTF8Safe(snippet, maxLen)
	}
	if snippet != "" {
		return fmt.Errorf("%s: %s", prefix, snippet)
	}
	return fmt.Errorf("%s: %v", prefix, runErr)
}

func runBounded(count, limit int, aborted func() bool, fn func(idx int)) {
	if count <= 0 {
		return
	}
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		if aborted != nil && aborted() {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer func() { <-sem; wg.Done() }()
			if aborted != nil && aborted() {
				return
			}
			fn(idx)
		}(i)
	}
	wg.Wait()
}
