package sshclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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

// runSSHExec executes cmd over client with context and optional timeout cancellation,
// closing the remote SSH session promptly when the context expires.
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

	sshSess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sshSess.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-execCtx.Done():
			_ = sshSess.Close()
		}
	}()

	var out []byte
	if combined {
		out, err = sshSess.CombinedOutput(cmd)
	} else {
		out, err = sshSess.Output(cmd)
	}
	close(done)
	if err != nil && execCtx.Err() != nil {
		return out, execCtx.Err()
	}
	return out, err
}

func formatExecError(prefix string, out []byte, runErr error, maxLen int) error {
	snippet := strings.TrimSpace(string(out))
	if maxLen > 0 && len(snippet) > maxLen {
		snippet = snippet[len(snippet)-maxLen:]
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
