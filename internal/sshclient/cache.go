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
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
)

var (
	knownHostsMu       sync.Mutex
	knownHostsPathFunc = DefaultKnownHostsPath
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
	nonWS := func(r rune) bool { return !unicode.IsSpace(r) }
	i := strings.IndexFunc(s, nonWS)
	if i < 0 || s[i] == '#' {
		return 0, 0, false
	}
	endToken := func(start int) int {
		if k := strings.IndexFunc(s[start:], unicode.IsSpace); k >= 0 {
			return start + k
		}
		return len(s)
	}
	j := endToken(i)
	if s[i] == '@' {
		k := strings.IndexFunc(s[j:], nonWS)
		if k < 0 {
			return 0, 0, false
		}
		i = j + k
		j = endToken(i)
	}
	if i >= j {
		return 0, 0, false
	}
	return i, j, true
}

func atomicWriteFile(filePath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".known_hosts.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Chmod(perm)
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func matchesHostKeyPattern(pat string, targets []string) bool {
	p := strings.TrimSpace(pat)
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "|1|") {
		return matchesKnownHostPattern(p, "", 0, targets)
	}
	lp := strings.ToLower(p)
	if strings.ContainsAny(lp, "*?") {
		for _, t := range targets {
			if ok, err := path.Match(lp, t); err == nil && ok {
				return true
			}
		}
		return false
	}
	return slices.Contains(targets, lp)
}

func hostFieldMatchesTargets(hostField string, targets []string) bool {
	matched := false
	for _, pat := range strings.Split(hostField, ",") {
		p := strings.TrimSpace(pat)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "!") {
			if matchesHostKeyPattern(p[1:], targets) {
				return false
			}
			continue
		}
		if matchesHostKeyPattern(p, targets) {
			matched = true
		}
	}
	return matched
}

func defaultTOFUHostKeyCallback() ssh.HostKeyCallback {
	return tofuHostKeyCallback(knownHostsPathFunc())
}

// tofuHostKeyCallback implements Trust-On-First-Use SSH host key verification backed
// by known_hosts: first-seen hosts are atomically recorded, matching keys succeed,
// and mismatched or revoked keys are rejected.
func tofuHostKeyCallback(khPath string) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		if key == nil {
			return errors.New("服务器未提供有效的主机公钥")
		}
		if strings.TrimSpace(khPath) == "" {
			return errors.New("无法确定 known_hosts 路径，拒绝建立未校验的 SSH 连接")
		}

		host, port := normalizeHostAndPort(hostname, 0)
		if host == "" {
			return fmt.Errorf("无效的目标主机地址: %s", hostname)
		}
		if port <= 0 {
			port = 22
		}

		var (
			targets     []string
			recordToken string
			displayHost string
		)
		if port == 22 {
			targets = []string{host, fmt.Sprintf("[%s]:22", host)}
			recordToken = host
			displayHost = host
		} else {
			bracket := fmt.Sprintf("[%s]:%d", host, port)
			targets = []string{bracket}
			recordToken = bracket
			displayHost = bracket
		}

		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()

		raw, err := os.ReadFile(khPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("读取 known_hosts 失败: %w", err)
		}

		wantBytes := key.Marshal()
		hostKnown := false
		keyMatched := false

		if len(raw) > 0 {
			txt := strings.ReplaceAll(string(raw), "\r\n", "\n")
			for _, line := range strings.Split(txt, "\n") {
				s, e, ok := findHostFieldSpan(line)
				if !ok {
					continue
				}
				trimmed := strings.TrimSpace(line)
				marker := ""
				if strings.HasPrefix(trimmed, "@") {
					if sp := strings.IndexFunc(trimmed, unicode.IsSpace); sp > 0 {
						marker = trimmed[:sp]
					}
				}
				if marker == "@cert-authority" {
					continue
				}
				if !hostFieldMatchesTargets(line[s:e], targets) {
					continue
				}
				keyPart := strings.TrimSpace(line[e:])
				if keyPart == "" {
					continue
				}
				parsedKey, _, _, _, parseErr := ssh.ParseAuthorizedKey([]byte(keyPart))
				if parseErr != nil {
					continue
				}
				if marker == "@revoked" {
					if bytes.Equal(parsedKey.Marshal(), wantBytes) {
						return fmt.Errorf("服务器主机密钥已被吊销 (%s，指纹 %s)，已拒绝连接", displayHost, ssh.FingerprintSHA256(key))
					}
					continue
				}
				hostKnown = true
				if bytes.Equal(parsedKey.Marshal(), wantBytes) {
					keyMatched = true
				}
			}
		}

		if keyMatched {
			return nil
		}
		if hostKnown {
			return fmt.Errorf(
				"主机密钥指纹不匹配 (%s，当前指纹 %s)：远程服务器身份已变更，可能存在中间人攻击或服务器已重装。如果确认服务器已重装，请在「设置 → SSH 缓存管理」中清理该主机的 SSH 缓存后重试",
				displayHost,
				ssh.FingerprintSHA256(key),
			)
		}

		// First sight (TOFU): atomically append host key to known_hosts.
		entryLine := recordToken + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + "\n"
		var next bytes.Buffer
		if len(raw) > 0 {
			next.Write(raw)
			if raw[len(raw)-1] != '\n' {
				next.WriteByte('\n')
			}
		}
		next.WriteString(entryLine)
		if err := atomicWriteFile(khPath, next.Bytes(), 0o600); err != nil {
			return fmt.Errorf("保存主机密钥到 known_hosts 失败: %w", err)
		}
		return nil
	}
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
	return removed, atomicWriteFile(filePath, []byte(out), 0600)
}

func ClearKnownHostsHost(khPath, host string, port int) (int, error) {
	nh, np := normalizeHostAndPort(host, port)
	if nh == "" {
		return 0, errors.New("请输入要清理的主机地址或 IP")
	}
	if khPath == "" {
		return 0, nil
	}
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
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
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	n := 0
	if raw, err := os.ReadFile(khPath); err == nil {
		n += countKnownHostsBytes(raw)
		if len(raw) > 0 {
			if _, e := os.Stat(khPath + ".gssh.bak"); os.IsNotExist(e) {
				_ = atomicWriteFile(khPath+".gssh.bak", raw, 0600)
			}
		}
		if err = atomicWriteFile(khPath, []byte{}, 0600); err != nil {
			return 0, fmt.Errorf("清空 known_hosts 失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("读取 known_hosts 失败: %w", err)
	}
	if oldRaw, err := os.ReadFile(khPath + ".old"); err == nil {
		n += countKnownHostsBytes(oldRaw)
		_ = atomicWriteFile(khPath+".old", []byte{}, 0600)
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
			s.dirCacheGen++
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
