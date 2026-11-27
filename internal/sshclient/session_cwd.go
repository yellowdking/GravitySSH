package sshclient

import (
	"path"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const dirCacheTTL = 20 * time.Second

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

func putDirCacheIfGen(s *sessionState, target string, entries []models.SFTPFileEntry, expectedGen uint64) {
	cp := cloneDirEntries(entries)
	s.mu.Lock()
	if s.dirCacheGen == expectedGen {
		if s.dirCache == nil {
			s.dirCache = make(map[string]dirCacheEntry)
		}
		s.dirCache[target] = dirCacheEntry{entries: cp, fetchedAt: time.Now()}
	}
	s.mu.Unlock()
}

func (m *Manager) getDirCacheGen(sid string) (uint64, bool) {
	s := m.getSession(sid)
	if s == nil {
		return 0, false
	}
	s.mu.Lock()
	gen := s.dirCacheGen
	s.mu.Unlock()
	return gen, true
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
		if _, loaded := m.dirWarmupInFlight.LoadOrStore(key, struct{}{}); !loaded {
			go func() {
				defer m.dirWarmupInFlight.Delete(key)
				_ = m.CachedDirEntries(sid, target)
			}()
		}
	}
	return nil
}

// CachedDirEntries returns SFTP entries (including dotfiles) with a 20s cache.
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
	gen := s.dirCacheGen
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
	entries, bErr := buildSFTPEntries(sc, target, infos)
	if bErr != nil {
		if isSFTPConnError(bErr) {
			m.invalidateSFTP(sid, sc)
		}
		return nil
	}
	putDirCacheIfGen(s, target, entries, gen)
	return entries
}

func (m *Manager) updateDirCacheIfGen(sid, dir string, entries []models.SFTPFileEntry, expectedGen uint64) {
	if s := m.getSession(sid); s != nil {
		putDirCacheIfGen(s, dir, entries, expectedGen)
	}
}

func (m *Manager) invalidateDirCache(sid, dir string) {
	if s := m.getSession(sid); s != nil {
		s.mu.Lock()
		s.dirCacheGen++
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
	if clean == "/" {
		prefix = "/"
	}
	s.mu.Lock()
	s.dirCacheGen++
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

func splitShellCommandSegments(cmd string) []string {
	var (
		segs    []string
		cur     strings.Builder
		inS     bool
		inD     bool
		escaped bool
	)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			segs = append(segs, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if escaped {
			cur.WriteByte(ch)
			escaped = false
			continue
		}
		if inS {
			if ch == '\'' {
				inS = false
			}
			cur.WriteByte(ch)
			continue
		}
		if ch == '\\' {
			cur.WriteByte(ch)
			escaped = true
			continue
		}
		if inD {
			if ch == '"' {
				inD = false
			}
			cur.WriteByte(ch)
			continue
		}
		switch ch {
		case '\'':
			inS = true
			cur.WriteByte(ch)
		case '"':
			inD = true
			cur.WriteByte(ch)
		case ';', '\n':
			flush()
		case '&':
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				flush()
				i++
			} else {
				cur.WriteByte(ch)
			}
		case '|':
			if i+1 < len(cmd) && cmd[i+1] == '|' {
				flush()
				i++
			} else {
				cur.WriteByte(ch)
			}
		default:
			cur.WriteByte(ch)
		}
	}
	flush()
	return segs
}

type cdToken struct {
	val    string
	quoted bool
	squote bool
}

func hasUnquotedPipe(seg string) bool {
	inS, inD, esc := false, false, false
	for i := 0; i < len(seg); i++ {
		ch := seg[i]
		if esc {
			esc = false
			continue
		}
		if inS {
			if ch == '\'' {
				inS = false
			}
			continue
		}
		if ch == '\\' {
			esc = true
			continue
		}
		if inD {
			if ch == '"' {
				inD = false
			}
			continue
		}
		switch ch {
		case '\'':
			inS = true
		case '"':
			inD = true
		case '|':
			return true
		}
	}
	return false
}

func tokenizeSimpleCommand(seg string) []cdToken {
	var (
		out    []cdToken
		cur    strings.Builder
		hasTok bool
		quoted bool
		squote bool
		inS    bool
		inD    bool
		esc    bool
	)
	flush := func() {
		if hasTok {
			out = append(out, cdToken{val: cur.String(), quoted: quoted, squote: squote})
			cur.Reset()
			hasTok = false
			quoted = false
			squote = false
		}
	}
	for i := 0; i < len(seg); i++ {
		ch := seg[i]
		if esc {
			cur.WriteByte(ch)
			hasTok = true
			esc = false
			continue
		}
		if inS {
			if ch == '\'' {
				inS = false
			} else {
				cur.WriteByte(ch)
			}
			continue
		}
		if inD {
			if ch == '\\' && i+1 < len(seg) && (seg[i+1] == '"' || seg[i+1] == '\\' || seg[i+1] == '$' || seg[i+1] == '`') {
				i++
				cur.WriteByte(seg[i])
			} else if ch == '"' {
				inD = false
			} else {
				cur.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case ' ', '\t':
			flush()
		case '\\':
			esc = true
			hasTok = true
		case '\'':
			inS = true
			hasTok = true
			quoted = true
			squote = true
		case '"':
			inD = true
			hasTok = true
			quoted = true
		default:
			cur.WriteByte(ch)
			hasTok = true
		}
	}
	flush()
	return out
}

func isCDFlagCluster(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' {
		return false
	}
	for i := 1; i < len(tok); i++ {
		switch tok[i] {
		case 'L', 'P', 'e', '@':
		default:
			return false
		}
	}
	return true
}

func isShellRedirToken(tok string) bool {
	if tok == "" {
		return false
	}
	s := tok
	if s[0] >= '0' && s[0] <= '9' {
		s = s[1:]
	} else if s[0] == '&' {
		s = s[1:]
	}
	return strings.HasPrefix(s, ">") || strings.HasPrefix(s, "<")
}

func parseCDTarget(seg string) (string, bool) {
	if hasUnquotedPipe(seg) {
		return "", false
	}
	toks := tokenizeSimpleCommand(seg)
	if len(toks) == 0 || toks[0].val != "cd" {
		return "", false
	}
	afterDashDash := false
	var args []cdToken
	for i := 1; i < len(toks); i++ {
		tk := toks[i]
		if !tk.quoted && isShellRedirToken(tk.val) {
			// Skip standalone redirection operator and its following filename token.
			if tk.val == ">" || tk.val == ">>" || tk.val == "<" || tk.val == "2>" || tk.val == "2>>" || tk.val == "&>" || tk.val == "1>" {
				i++
			}
			continue
		}
		if !afterDashDash && !tk.quoted {
			if tk.val == "--" {
				afterDashDash = true
				continue
			}
			if tk.val == "-" {
				return "", false
			}
			if strings.HasPrefix(tk.val, "-") && len(tk.val) > 1 {
				if isCDFlagCluster(tk.val) {
					continue
				}
				return "", false
			}
		}
		args = append(args, tk)
	}
	if len(args) == 0 {
		return "~", true
	}
	if len(args) > 1 {
		return "", false
	}
	target := args[0]
	if target.val == "" {
		return "", false
	}
	if !target.squote && (strings.Contains(target.val, "$") || strings.Contains(target.val, "`")) {
		return "", false
	}
	if !target.quoted && strings.ContainsAny(target.val, "*?") {
		return "", false
	}
	return target.val, true
}

func (m *Manager) updateCWDFromCommand(s *sessionState, cmd string) {
	for _, seg := range splitShellCommandSegments(cmd) {
		arg, ok := parseCDTarget(seg)
		if !ok {
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
	if !isReadOnlyCommand(cmd) {
		s.mu.Lock()
		delete(s.dirCache, sessionDirTargetLocked(s, ""))
		s.dirCacheGen++
		s.mu.Unlock()
	}
}

var readOnlyBaseCommands = map[string]bool{
	"cd": true, "ls": true, "ll": true, "la": true, "l": true, "dir": true,
	"pwd": true, "cat": true, "head": true, "tail": true, "less": true, "more": true,
	"bat": true, "grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true,
	"fd": true, "locate": true, "which": true, "whereis": true, "type": true,
	"stat": true, "file": true, "wc": true, "tree": true, "du": true, "df": true,
	"free": true, "top": true, "htop": true, "ps": true, "pgrep": true,
	"uptime": true, "uname": true, "hostname": true, "whoami": true, "id": true,
	"w": true, "who": true, "last": true, "date": true, "cal": true,
	"env": true, "printenv": true, "echo": true, "printf": true, "history": true,
	"clear": true, "man": true, "help": true, "netstat": true, "ss": true,
	"ifconfig": true, "ping": true, "dig": true, "nslookup": true, "traceroute": true,
	"lsof": true, "dmesg": true, "journalctl": true, "nvidia-smi": true,
}

func isReadOnlyCommand(cmd string) bool {
	s := strings.TrimSpace(cmd)
	if s == "" {
		return true
	}
	// Any output redirection or subshell substitution may mutate the filesystem.
	if strings.ContainsAny(s, ">`") || strings.Contains(s, "$(") {
		return false
	}
	// Split across command separators ;, &&, ||, |
	normalized := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n").Replace(s)
	for _, seg := range strings.Split(normalized, "\n") {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		// Skip leading env assignments (e.g. LC_ALL=C ls)
		idx := 0
		for idx < len(fields) && strings.Contains(fields[idx], "=") && !strings.HasPrefix(fields[idx], "-") {
			idx++
		}
		if idx >= len(fields) {
			continue
		}
		base := fields[idx]
		rest := fields[idx+1:]
		if readOnlyBaseCommands[base] {
			continue
		}
		switch base {
		case "find":
			for _, arg := range rest {
				if arg == "-delete" || arg == "-exec" || arg == "-execdir" || arg == "-ok" {
					return false
				}
			}
			continue
		case "git":
			if len(rest) > 0 {
				switch rest[0] {
				case "status", "log", "diff", "show", "rev-parse", "remote":
					continue
				}
			}
			return false
		case "docker":
			if len(rest) > 0 {
				switch rest[0] {
				case "ps", "images", "logs", "inspect", "stats", "info", "version":
					continue
				}
			}
			return false
		case "systemctl":
			if len(rest) > 0 {
				switch rest[0] {
				case "status", "is-active", "is-enabled", "list-units", "list-unit-files":
					continue
				}
			}
			return false
		default:
			return false
		}
	}
	return true
}
