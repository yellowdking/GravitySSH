package sshclient

import (
	"path"
	"strings"
	"sync"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const dirCacheTTL = 5 * time.Second

var dirWarmupInFlight sync.Map

func (m *Manager) getSession(id string) *sessionState {
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	return s
}

func (m *Manager) SetSessionCWD(sid, remote string) {
	p := strings.TrimSpace(remote)
	s := m.getSession(sid)
	if p == "" || s == nil {
		return
	}
	clean := cleanAbsPath(p)
	s.mu.Lock()
	s.currentDir = clean
	s.mu.Unlock()
}

func (m *Manager) GetSessionCWD(sid string) string {
	s := m.getSession(sid)
	if s == nil {
		return "/"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentDir != "" {
		return s.currentDir
	}
	if s.info.HomeDir != "" {
		return s.info.HomeDir
	}
	return "/"
}

func (m *Manager) GetSessionHomeDir(sid string) string {
	s := m.getSession(sid)
	if s == nil {
		return "/"
	}
	s.mu.Lock()
	h := s.info.HomeDir
	s.mu.Unlock()
	if h == "" {
		return "/"
	}
	return h
}

func sessionDirTargetLocked(s *sessionState, dir string) string {
	home, cur := s.info.HomeDir, s.currentDir
	if home == "" {
		home = "/"
	}
	if cur == "" {
		cur = home
	}
	return resolveRemoteDir(dir, cur, home)
}

func cloneDirEntries(src []models.SFTPFileEntry) []models.SFTPFileEntry {
	out := make([]models.SFTPFileEntry, len(src))
	copy(out, src)
	return out
}

func peekCacheLocked(s *sessionState, target string) ([]models.SFTPFileEntry, bool) {
	c, ok := s.dirCache[target]
	if !ok || time.Since(c.fetchedAt) >= dirCacheTTL {
		return nil, false
	}
	return cloneDirEntries(c.entries), true
}

func putDirCache(s *sessionState, target string, entries []models.SFTPFileEntry) {
	cp := cloneDirEntries(entries)
	s.mu.Lock()
	if s.dirCache == nil {
		s.dirCache = make(map[string]dirCacheEntry)
	}
	s.dirCache[target] = dirCacheEntry{entries: cp, fetchedAt: time.Now()}
	s.mu.Unlock()
}

// PeekCachedDirEntries returns fresh cached entries without blocking on SFTP,
// and triggers a deduped background warm-up on miss when connected.
func (m *Manager) PeekCachedDirEntries(sid, dirPath string) []models.SFTPFileEntry {
	s := m.getSession(sid)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	target := sessionDirTargetLocked(s, dirPath)
	if out, ok := peekCacheLocked(s, target); ok {
		s.mu.Unlock()
		return out
	}
	canWarm := !s.closed && !s.userClosed && s.sftpClient != nil
	s.mu.Unlock()
	if canWarm {
		key := sid + "\x00" + target
		if _, loaded := dirWarmupInFlight.LoadOrStore(key, struct{}{}); !loaded {
			go func() {
				defer dirWarmupInFlight.Delete(key)
				_ = m.CachedDirEntries(sid, target)
			}()
		}
	}
	return nil
}

// CachedDirEntries returns SFTP entries (including dotfiles) with a 5s cache.
func (m *Manager) CachedDirEntries(sid, dir string) []models.SFTPFileEntry {
	s := m.getSession(sid)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	sc := s.sftpClient
	target := sessionDirTargetLocked(s, dir)
	if out, ok := peekCacheLocked(s, target); ok {
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()
	if sc == nil {
		return nil
	}
	infos, err := sc.ReadDir(target)
	if err != nil {
		if isSFTPConnError(err) {
			m.invalidateSFTP(sid, sc)
		}
		return nil
	}
	entries := buildSFTPEntries(sc, target, infos)
	putDirCache(s, target, entries)
	return entries
}

func (m *Manager) updateDirCache(sid, dir string, entries []models.SFTPFileEntry) {
	if s := m.getSession(sid); s != nil {
		putDirCache(s, dir, entries)
	}
}

func (m *Manager) invalidateDirCache(sid, dir string) {
	if s := m.getSession(sid); s != nil {
		s.mu.Lock()
		delete(s.dirCache, path.Clean(dir))
		s.mu.Unlock()
	}
}

func (m *Manager) invalidateDirTreeCache(sid, target string) {
	s := m.getSession(sid)
	if s == nil {
		return
	}
	clean := path.Clean(target)
	prefix := clean + "/"
	s.mu.Lock()
	delete(s.dirCache, path.Dir(clean))
	delete(s.dirCache, clean)
	for k := range s.dirCache {
		if strings.HasPrefix(k, prefix) {
			delete(s.dirCache, k)
		}
	}
	s.mu.Unlock()
}

func cleanAbsPath(p string) string {
	clean := path.Clean(p)
	if !strings.HasPrefix(clean, "/") {
		return "/" + clean
	}
	return clean
}

func resolveRemoteDir(dir, curDir, homeDir string) string {
	d := strings.TrimSpace(dir)
	switch {
	case d == "":
		d = curDir
	case d == "~":
		d = homeDir
	case strings.HasPrefix(d, "~/"):
		d = path.Join(homeDir, d[2:])
	case !strings.HasPrefix(d, "/"):
		d = path.Join(curDir, d)
	}
	return cleanAbsPath(d)
}

func (m *Manager) updateCWDFromCommand(s *sessionState, cmd string) {
	for _, seg := range strings.FieldsFunc(cmd, func(r rune) bool { return r == ';' }) {
		for _, sub := range strings.Split(seg, "&&") {
			part := strings.TrimSpace(sub)
			if part != "cd" && !strings.HasPrefix(part, "cd ") && !strings.HasPrefix(part, "cd\t") {
				continue
			}
			arg := strings.Trim(strings.TrimSpace(strings.TrimPrefix(part, "cd")), `"'`)
			if arg == "-" {
				continue
			}
			var changedTo string
			s.mu.Lock()
			nextDir := sessionDirTargetLocked(s, "~")
			if arg != "" && arg != "~" {
				nextDir = sessionDirTargetLocked(s, arg)
			}
			if nextDir != s.currentDir {
				s.currentDir = nextDir
				changedTo = nextDir
			}
			sid := s.info.SessionID
			s.mu.Unlock()
			if changedTo != "" && sid != "" {
				m.emit("session:cwd", map[string]string{"sessionId": sid, "path": changedTo})
			}
		}
	}
	s.mu.Lock()
	clear(s.dirCache)
	s.mu.Unlock()
}
