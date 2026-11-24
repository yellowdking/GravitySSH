package sshclient

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const dirCacheTTL = 5 * time.Second

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
		if isSFTPConnError(err) {
			m.invalidateSFTP(sessionID, sc)
		}
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

func (m *Manager) invalidateDirTreeCache(sessionID, target string) {
	m.mu.RLock()
	sess, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	clean := path.Clean(target)
	prefix := clean + "/"
	sess.mu.Lock()
	delete(sess.dirCache, path.Dir(clean))
	delete(sess.dirCache, clean)
	for k := range sess.dirCache {
		if strings.HasPrefix(k, prefix) {
			delete(sess.dirCache, k)
		}
	}
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
			var changedTo string
			sess.mu.Lock()
			homeDir := sess.info.HomeDir
			if homeDir == "" {
				homeDir = "/"
			}
			curDir := sess.currentDir
			if curDir == "" {
				curDir = homeDir
			}
			nextDir := homeDir
			if arg != "" && arg != "~" {
				nextDir = resolveRemoteDir(arg, curDir, homeDir)
			}
			if nextDir != sess.currentDir {
				sess.currentDir = nextDir
				changedTo = nextDir
			}
			sid := sess.info.SessionID
			sess.mu.Unlock()
			if changedTo != "" && sid != "" {
				m.emit("session:cwd", map[string]string{
					"sessionId": sid,
					"path":      changedTo,
				})
			}
		}
	}
}
