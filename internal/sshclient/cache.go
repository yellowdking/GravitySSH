package sshclient

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func (m *Manager) knownHostsPath() string {
	if m != nil && m.knownHostsPathFn != nil {
		return m.knownHostsPathFn()
	}
	return knownHostsPathFunc()
}

func CountKnownHosts(p string) int {
	if strings.TrimSpace(p) == "" {
		return 0
	}
	knownHostsMu.Lock()
	raw, err := os.ReadFile(p)
	knownHostsMu.Unlock()
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
	if err := renameWithRetry(tmpPath, filePath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// renameWithRetry retries transient Windows file-lock rename errors (AV/indexer).
func renameWithRetry(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	for _, delay := range []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
		80 * time.Millisecond,
	} {
		time.Sleep(delay)
		err = os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
	}
	return err
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
	return removed, atomicWriteFile(filePath, []byte(out), 0o600)
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
				_ = atomicWriteFile(khPath+".gssh.bak", raw, 0o600)
			}
		}
		if err = atomicWriteFile(khPath, []byte{}, 0o600); err != nil {
			return 0, fmt.Errorf("清空 known_hosts 失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("读取 known_hosts 失败: %w", err)
	}
	if oldRaw, err := os.ReadFile(khPath + ".old"); err == nil {
		n += countKnownHostsBytes(oldRaw)
		_ = atomicWriteFile(khPath+".old", []byte{}, 0o600)
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
			s.cachedCores, s.gpuChecked, s.hasGPU = 0, false, false
		}
		s.mu.Unlock()
	}
	return n
}

func (m *Manager) GetSSHCacheInfo() SSHCacheInfo {
	p, dirs := m.knownHostsPath(), 0
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
	p := m.knownHostsPath()
	n, err := ClearKnownHostsHost(p, host, port)
	if err != nil {
		return SSHCacheClearResult{KnownHostsPath: p}, err
	}
	return SSHCacheClearResult{n, m.clearSessionCaches(host), p}, nil
}

func (m *Manager) ClearAllSSHCache() (SSHCacheClearResult, error) {
	p := m.knownHostsPath()
	n, err := ClearAllKnownHosts(p)
	if err != nil {
		return SSHCacheClearResult{KnownHostsPath: p}, err
	}
	return SSHCacheClearResult{n, m.clearSessionCaches(""), p}, nil
}
