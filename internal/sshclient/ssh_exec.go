package sshclient

import (
	"context"
	"errors"
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
