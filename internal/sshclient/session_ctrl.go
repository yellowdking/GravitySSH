package sshclient

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const dirCacheTTL = 5 * time.Second

const writeChunkSize = 1024

func (m *Manager) WriteInput(sessionID, data string) error {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
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

func (m *Manager) CurrentInputLine(sessionID string) string {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok || sess.recorder == nil {
		return ""
	}
	return sess.recorder.CurrentLine()
}

// Resize records the latest terminal dimensions even while connecting,
// and forwards WindowChange to the live SSH session once ready.
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
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
		m.markDisconnected(s, "应用关闭")
	}
}

// SetSessionCWD updates the tracked working directory for a session (e.g. from SFTP browsing).
func (m *Manager) SetSessionCWD(sessionID, remotePath string) {
	p := strings.TrimSpace(remotePath)
	if p == "" {
		return
	}
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	clean := path.Clean(p)
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	sess.mu.Lock()
	sess.currentDir = clean
	sess.mu.Unlock()
}

// GetSessionCWD returns the session's tracked current working directory.
func (m *Manager) GetSessionCWD(sessionID string) string {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return "/"
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.currentDir != "" {
		return sess.currentDir
	}
	if sess.info.HomeDir != "" {
		return sess.info.HomeDir
	}
	return "/"
}

// CachedDirEntries returns SFTP entries for dir with a 5-second cache for snappy path autocomplete.
func (m *Manager) CachedDirEntries(sessionID, dir string) []models.SFTPFileEntry {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil
	}

	sess.mu.Lock()
	sc := sess.sftpClient
	homeDir := sess.info.HomeDir
	curDir := sess.currentDir
	if curDir == "" {
		curDir = homeDir
	}
	if homeDir == "" {
		homeDir = "/"
	}
	if curDir == "" {
		curDir = "/"
	}
	target := resolveRemoteDir(dir, curDir, homeDir)
	if cached, found := sess.dirCache[target]; found && time.Since(cached.fetchedAt) < dirCacheTTL {
		out := make([]models.SFTPFileEntry, len(cached.entries))
		copy(out, cached.entries)
		sess.mu.Unlock()
		return out
	}
	sess.mu.Unlock()

	if sc == nil {
		return nil
	}
	infos, err := sc.ReadDir(target)
	if err != nil {
		return nil
	}

	entries := make([]models.SFTPFileEntry, 0, len(infos))
	for _, info := range infos {
		full := path.Join(target, info.Name())
		isSymlink := info.Mode()&os.ModeSymlink != 0
		isDir := info.IsDir()
		if isSymlink {
			if st, stErr := sc.Stat(full); stErr == nil && st.IsDir() {
				isDir = true
			}
		}
		entries = append(entries, models.SFTPFileEntry{
			Name:      info.Name(),
			Path:      full,
			IsDir:     isDir,
			IsSymlink: isSymlink,
			Size:      info.Size(),
			Mode:      info.Mode().String(),
			ModTime:   info.ModTime(),
		})
	}

	sess.mu.Lock()
	if sess.dirCache == nil {
		sess.dirCache = make(map[string]dirCacheEntry)
	}
	sess.dirCache[target] = dirCacheEntry{entries: entries, fetchedAt: time.Now()}
	sess.mu.Unlock()
	return entries
}

func (m *Manager) updateDirCache(sessionID, dir string, entries []models.SFTPFileEntry) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	cp := make([]models.SFTPFileEntry, len(entries))
	copy(cp, entries)
	sess.mu.Lock()
	if sess.dirCache == nil {
		sess.dirCache = make(map[string]dirCacheEntry)
	}
	sess.dirCache[dir] = dirCacheEntry{entries: cp, fetchedAt: time.Now()}
	sess.mu.Unlock()
}

func (m *Manager) invalidateDirCache(sessionID, dir string) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	sess.mu.Lock()
	delete(sess.dirCache, path.Clean(dir))
	sess.mu.Unlock()
}

func resolveRemoteDir(dir, curDir, homeDir string) string {
	d := strings.TrimSpace(dir)
	if d == "" {
		d = curDir
	} else if d == "~" {
		d = homeDir
	} else if strings.HasPrefix(d, "~/") {
		d = path.Join(homeDir, d[2:])
	} else if !strings.HasPrefix(d, "/") {
		d = path.Join(curDir, d)
	}
	clean := path.Clean(d)
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}

func (m *Manager) updateCWDFromCommand(sess *sessionState, cmd string) {
	segments := strings.FieldsFunc(cmd, func(r rune) bool { return r == ';' })
	for _, seg := range segments {
		for _, sub := range strings.Split(seg, "&&") {
			part := strings.TrimSpace(sub)
			if part != "cd" && !strings.HasPrefix(part, "cd ") && !strings.HasPrefix(part, "cd\t") {
				continue
			}
			arg := strings.TrimSpace(strings.TrimPrefix(part, "cd"))
			arg = strings.Trim(arg, `"'`)
			if arg == "-" {
				continue
			}
			sess.mu.Lock()
			homeDir := sess.info.HomeDir
			if homeDir == "" {
				homeDir = "/"
			}
			curDir := sess.currentDir
			if curDir == "" {
				curDir = homeDir
			}
			if arg == "" || arg == "~" {
				sess.currentDir = homeDir
			} else {
				sess.currentDir = resolveRemoteDir(arg, curDir, homeDir)
			}
			sess.mu.Unlock()
		}
	}
}
