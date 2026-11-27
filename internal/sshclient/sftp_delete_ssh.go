package sshclient

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var errSSHDeleteNotFound = errors.New("ssh delete target not found")

func (m *Manager) cachedSSHSameFS(sessionID string) bool {
	sess := m.getSession(sessionID)
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.sshSameFSChecked && sess.sshSameFS
}

// sshMatchesSFTPRoot checks once per session whether SSH exec shares the same
// filesystem root as SFTP (prevents running SSH rm against a chrooted SFTP).
func (m *Manager) sshMatchesSFTPRoot(sessionID string, sc *sftp.Client) bool {
	sess := m.getSession(sessionID)
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	if sess.sshSameFSChecked {
		same := sess.sshSameFS
		sess.mu.Unlock()
		return same
	}
	var client *ssh.Client
	if sess.dialRes != nil {
		client = sess.dialRes.Client
	}
	sess.mu.Unlock()
	if client == nil || sc == nil {
		return false
	}

	setResult := func(same bool) bool {
		sess.mu.Lock()
		sess.sshSameFSChecked = true
		sess.sshSameFS = same
		sess.mu.Unlock()
		return same
	}

	sftpWD, err := sc.Getwd()
	if err != nil {
		if !isSFTPConnError(err) {
			return setResult(false)
		}
		return false
	}
	sftpWD = path.Clean(strings.TrimSpace(sftpWD))
	if sftpWD == "" || sftpWD == "/" {
		return setResult(false)
	}

	out, runErr := runSSHExec(context.Background(), client, "sh -c 'pwd -P'", 8*time.Second, false)
	if runErr != nil {
		if !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
			return setResult(false)
		}
		return false
	}

	same := path.Clean(strings.TrimSpace(string(out))) == sftpWD
	return setResult(same)
}

func (m *Manager) deleteViaSSH(sessionID, clean string) error {
	if !strings.HasPrefix(clean, "/") || clean == "/" || clean == "." || clean == ".." {
		return fmt.Errorf("skip unsafe fallback path")
	}
	client, err := m.getSSHClient(sessionID)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf(
		"sh -c 'p=$1; if [ ! -e \"$p\" ] && [ ! -L \"$p\" ]; then exit 44; fi; if [ -d \"$p\" ] && [ ! -L \"$p\" ]; then chmod -R u+rwX -- \"$p\" 2>/dev/null || true; fi; rm -rf -- \"$p\" && [ ! -e \"$p\" ] && [ ! -L \"$p\" ]' _ %s",
		shellQuote(clean),
	)
	out, runErr := runSSHExec(context.Background(), client, cmd, 90*time.Second, true)
	if runErr != nil {
		var exitErr *ssh.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitStatus() == 44 {
			return errSSHDeleteNotFound
		}
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("ssh rm: %s", msg)
		}
		return fmt.Errorf("ssh rm: %w", runErr)
	}
	return nil
}
