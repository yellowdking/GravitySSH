package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	sftpMaxConcurrentRequestsPerFile = 64
	maxSSHExecOutputBytes            = 256 * 1024
)

var sftpInitTimeout = 15 * time.Second

type sessionPipeCloser struct {
	io.WriteCloser
	sess *ssh.Session
	once sync.Once
}

func (c *sessionPipeCloser) Close() error {
	var err error
	c.once.Do(func() {
		err = c.WriteCloser.Close()
		if c.sess != nil {
			_ = c.sess.Close()
		}
	})
	return err
}

func newSFTPClient(cli *ssh.Client) (*sftp.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sftpInitTimeout)
	defer cancel()
	return newSFTPClientWithContext(ctx, cli)
}

func newSFTPClientWithContext(ctx context.Context, cli *ssh.Client) (*sftp.Client, error) {
	if cli == nil {
		return nil, errors.New("SSH 连接未就绪")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sshSess, err := newSessionWithContext(ctx, cli)
	if err != nil {
		return nil, err
	}

	type initResult struct {
		client *sftp.Client
		err    error
	}
	ch := make(chan initResult, 1)
	go func() {
		wr, err := sshSess.StdinPipe()
		if err != nil {
			_ = sshSess.Close()
			ch <- initResult{err: err}
			return
		}
		rd, err := sshSess.StdoutPipe()
		if err != nil {
			_ = sshSess.Close()
			ch <- initResult{err: err}
			return
		}
		if err := sshSess.RequestSubsystem("sftp"); err != nil {
			_ = sshSess.Close()
			ch <- initResult{err: err}
			return
		}
		wrappedWr := &sessionPipeCloser{WriteCloser: wr, sess: sshSess}
		sc, err := sftp.NewClientPipe(
			rd,
			wrappedWr,
			sftp.UseConcurrentReads(true),
			sftp.UseConcurrentWrites(true),
			sftp.MaxConcurrentRequestsPerFile(sftpMaxConcurrentRequestsPerFile),
		)
		if err != nil {
			_ = wrappedWr.Close()
			ch <- initResult{err: err}
			return
		}
		select {
		case ch <- initResult{client: sc}:
		case <-ctx.Done():
			_ = sc.Close()
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
			_ = res.client.Close()
			return nil, err
		}
		return res.client, nil
	case <-ctx.Done():
		abortSSHSession(sshSess)
		return nil, ctx.Err()
	}
}

func dialSFTPWithContext(ctx context.Context, fn func() (*sftp.Client, error)) (*sftp.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type dialResult struct {
		client *sftp.Client
		err    error
	}
	ch := make(chan dialResult, 1)
	go func() {
		sc, err := fn()
		select {
		case ch <- dialResult{client: sc, err: err}:
		case <-ctx.Done():
			if sc != nil {
				_ = sc.Close()
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
			if res.client != nil {
				_ = res.client.Close()
			}
			return nil, err
		}
		return res.client, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type boundedBuffer struct {
	mu       sync.Mutex
	limit    int
	keepTail bool
	buf      []byte
}

func newBoundedBuffer(limit int, keepTail bool) *boundedBuffer {
	if limit <= 0 {
		limit = maxSSHExecOutputBytes
	}
	return &boundedBuffer{limit: limit, keepTail: keepTail}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.keepTail {
		rem := b.limit - len(b.buf)
		if rem > 0 {
			if len(p) > rem {
				b.buf = append(b.buf, p[:rem]...)
			} else {
				b.buf = append(b.buf, p...)
			}
		}
		return len(p), nil
	}
	if len(p) >= b.limit {
		if cap(b.buf) < b.limit {
			b.buf = make([]byte, b.limit)
		} else {
			b.buf = b.buf[:b.limit]
		}
		copy(b.buf, p[len(p)-b.limit:])
		return len(p), nil
	}
	total := len(b.buf) + len(p)
	if total > b.limit {
		drop := total - b.limit
		keep := len(b.buf) - drop
		copy(b.buf[:keep], b.buf[drop:])
		b.buf = b.buf[:keep]
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	return out
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
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
		buf := newBoundedBuffer(maxSSHExecOutputBytes, combined)
		sshSess.Stdout = buf
		if combined {
			sshSess.Stderr = buf
		}
		runErr := sshSess.Run(cmd)
		resCh <- execResult{out: buf.Bytes(), err: runErr}
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
