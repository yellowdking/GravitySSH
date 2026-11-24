package sshclient

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type SSHCacheInfo struct {
	KnownHostsPath string `json:"knownHostsPath"`
	KnownHostCount int    `json:"knownHostCount"`
	DirCacheCount  int    `json:"dirCacheCount"`
}

type SSHCacheClearResult struct {
	RemovedKnownHosts int    `json:"removedKnownHosts"`
	ClearedDirCaches  int    `json:"clearedDirCaches"`
	KnownHostsPath    string `json:"knownHostsPath"`
}

func DefaultKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func CountKnownHosts(p string) int {
	raw, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	return countKnownHostsBytes(raw)
}

func countKnownHostsBytes(raw []byte) int {
	n := 0
	for _, l := range strings.Split(string(raw), "\n") {
		if t := strings.TrimSpace(l); t != "" && t[0] != '#' {
			n++
		}
	}
	return n
}

func normalizeHostAndPort(host string, port int) (string, int) {
	h := strings.TrimSpace(host)
	if (strings.HasPrefix(h, "[") && strings.Contains(h, "]:")) || strings.Count(h, ":") == 1 {
		if ph, pp, err := net.SplitHostPort(h); err == nil {
			h = ph
			if p, err := strconv.Atoi(pp); port <= 0 && err == nil && p > 0 {
				port = p
			}
		}
	}
	return strings.ToLower(strings.Trim(h, "[] ")), port
}

func matchesKnownHostPattern(pat, host string, port int, targets []string) bool {
	p := strings.TrimSpace(pat)
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "|1|") {
		parts := strings.Split(p, "|")
		if len(parts) != 4 {
			return false
		}
		salt, e1 := base64.StdEncoding.DecodeString(parts[2])
		want, e2 := base64.StdEncoding.DecodeString(parts[3])
		if e1 != nil || e2 != nil || len(salt) == 0 || len(want) == 0 {
			return false
		}
		for _, t := range targets {
			mac := hmac.New(sha1.New, salt)
			mac.Write([]byte(t))
			if hmac.Equal(mac.Sum(nil), want) {
				return true
			}
		}
		return false
	}
	lp := strings.ToLower(p)
	return slices.Contains(targets, lp) || (port <= 0 && strings.HasPrefix(lp, "["+host+"]:"))
}

func findHostFieldSpan(line string) (int, int, bool) {
	s := strings.TrimRight(line, "\r")
	nonWS := func(r rune) bool { return r != ' ' && r != '\t' }
	i := strings.IndexFunc(s, nonWS)
	if i < 0 || s[i] == '#' {
		return 0, 0, false
	}
	j := i + len(strings.Fields(s[i:])[0])
	if s[i] == '@' {
		k := strings.IndexFunc(s[j:], nonWS)
		if k < 0 {
			return 0, 0, false
		}
		i = j + k
		j = i + len(strings.Fields(s[i:])[0])
	}
	return i, j, true
}

func removeHostFromFile(filePath, host string, port int) (int, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	eol := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		eol = "\r\n"
	}
	txt := strings.ReplaceAll(string(raw), "\r\n", "\n")
	tailNL := strings.HasSuffix(txt, "\n")
	if txt = strings.TrimSuffix(txt, "\n"); txt == "" {
		return 0, nil
	}
	pt := port
	if pt <= 0 {
		pt = 22
	}
	targets, removed := []string{host, fmt.Sprintf("[%s]:%d", host, pt)}, 0
	var keptLines []string
	for _, line := range strings.Split(txt, "\n") {
		s, e, ok := findHostFieldSpan(line)
		if !ok {
			keptLines = append(keptLines, line)
			continue
		}
		var kept []string
		hit := false
		for _, pat := range strings.Split(line[s:e], ",") {
			if matchesKnownHostPattern(pat, host, port, targets) {
				hit = true
			} else {
				kept = append(kept, pat)
			}
		}
		if !hit {
			keptLines = append(keptLines, line)
		} else if removed++; len(kept) > 0 {
			keptLines = append(keptLines, line[:s]+strings.Join(kept, ",")+line[e:])
		}
	}
	if removed == 0 {
		return 0, nil
	}
	out := strings.Join(keptLines, eol)
	if len(keptLines) > 0 && tailNL {
		out += eol
	}
	return removed, os.WriteFile(filePath, []byte(out), 0600)
}

func ClearKnownHostsHost(khPath, host string, port int) (int, error) {
	nh, np := normalizeHostAndPort(host, port)
	if nh == "" {
		return 0, errors.New("请输入要清理的主机地址或 IP")
	}
	if khPath == "" {
		return 0, nil
	}
	n, err := removeHostFromFile(khPath, nh, np)
	if err != nil {
		return 0, fmt.Errorf("清理 known_hosts 失败: %w", err)
	}
	if oldN, e := removeHostFromFile(khPath+".old", nh, np); e == nil {
		n += oldN
	}
	return n, nil
}

func ClearAllKnownHosts(khPath string) (int, error) {
	if khPath == "" {
		return 0, nil
	}
	n := 0
	if raw, err := os.ReadFile(khPath); err == nil {
		n += countKnownHostsBytes(raw)
		if len(raw) > 0 {
			if _, e := os.Stat(khPath + ".gssh.bak"); os.IsNotExist(e) {
				_ = os.WriteFile(khPath+".gssh.bak", raw, 0600)
			}
		}
		if err = os.WriteFile(khPath, []byte{}, 0600); err != nil {
			return 0, fmt.Errorf("清空 known_hosts 失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("读取 known_hosts 失败: %w", err)
	}
	if oldRaw, err := os.ReadFile(khPath + ".old"); err == nil {
		n += countKnownHostsBytes(oldRaw)
		_ = os.WriteFile(khPath+".old", []byte{}, 0600)
	}
	return n, nil
}

func (m *Manager) clearSessionCaches(host string) int {
	nh, _ := normalizeHostAndPort(host, 0)
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, s := range m.sessions {
		s.mu.Lock()
		if nh == "" || strings.EqualFold(strings.TrimSpace(s.info.Host), nh) {
			n += len(s.dirCache)
			s.dirCache = map[string]dirCacheEntry{}
			s.prevCPU, s.prevNet, s.prevSampleAt = nil, nil, time.Time{}
		}
		s.mu.Unlock()
	}
	return n
}

func (m *Manager) GetSSHCacheInfo() SSHCacheInfo {
	p, dirs := DefaultKnownHostsPath(), 0
	m.mu.RLock()
	for _, s := range m.sessions {
		s.mu.Lock()
		dirs += len(s.dirCache)
		s.mu.Unlock()
	}
	m.mu.RUnlock()
	return SSHCacheInfo{p, CountKnownHosts(p), dirs}
}

func (m *Manager) ClearSSHHostCache(host string, port int) (SSHCacheClearResult, error) {
	p := DefaultKnownHostsPath()
	n, err := ClearKnownHostsHost(p, host, port)
	if err != nil {
		return SSHCacheClearResult{KnownHostsPath: p}, err
	}
	return SSHCacheClearResult{n, m.clearSessionCaches(host), p}, nil
}

func (m *Manager) ClearAllSSHCache() (SSHCacheClearResult, error) {
	p := DefaultKnownHostsPath()
	n, err := ClearAllKnownHosts(p)
	if err != nil {
		return SSHCacheClearResult{KnownHostsPath: p}, err
	}
	return SSHCacheClearResult{n, m.clearSessionCaches(""), p}, nil
}
