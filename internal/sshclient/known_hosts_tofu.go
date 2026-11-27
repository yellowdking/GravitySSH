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
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/crypto/ssh"
)

var (
	knownHostsMu       sync.Mutex
	knownHostsPathFunc = DefaultKnownHostsPath

	// defaultPreferredHostKeyAlgos matches OpenSSH's preference for modern Ed25519
	// host keys first while still supporting ECDSA, RSA-SHA2, legacy RSA, and certs.
	defaultPreferredHostKeyAlgos = []string{
		ssh.KeyAlgoED25519,
		ssh.KeyAlgoECDSA256,
		ssh.KeyAlgoECDSA384,
		ssh.KeyAlgoECDSA521,
		ssh.KeyAlgoRSASHA512,
		ssh.KeyAlgoRSASHA256,
		ssh.KeyAlgoRSA,
		ssh.CertAlgoED25519v01,
		ssh.CertAlgoECDSA256v01,
		ssh.CertAlgoECDSA384v01,
		ssh.CertAlgoECDSA521v01,
		ssh.CertAlgoRSASHA512v01,
		ssh.CertAlgoRSASHA256v01,
		ssh.CertAlgoRSAv01,
	}
)

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

func knownHostTargets(hostname string) (targets []string, recordToken, displayHost string, ok bool) {
	host, port := normalizeHostAndPort(hostname, 0)
	if host == "" {
		return nil, "", "", false
	}
	if port <= 0 {
		port = 22
	}
	if port == 22 {
		return []string{host, fmt.Sprintf("[%s]:22", host)}, host, host, true
	}
	bracket := fmt.Sprintf("[%s]:%d", host, port)
	return []string{bracket}, bracket, bracket, true
}

func expandHostKeyAlgorithms(keyType string) []string {
	switch keyType {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	case ssh.CertAlgoRSAv01:
		return []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01}
	default:
		if slices.Contains(defaultPreferredHostKeyAlgos, keyType) {
			return []string{keyType}
		}
		return nil
	}
}

func defaultPreferredHostKeyAlgorithms(hostname string) []string {
	return preferredHostKeyAlgorithms(knownHostsPathFunc(), hostname)
}

// preferredHostKeyAlgorithms reads non-revoked known_hosts entries for hostname
// and prioritizes their host key algorithms ahead of defaultPreferredHostKeyAlgos
// so multi-key servers negotiate the key already recorded (e.g. by OpenSSH).
// Unknown hosts prefer ssh-ed25519 first.
func preferredHostKeyAlgorithms(khPath, hostname string) []string {
	fallback := slices.Clone(defaultPreferredHostKeyAlgos)
	if strings.TrimSpace(khPath) == "" {
		return fallback
	}
	targets, _, _, ok := knownHostTargets(hostname)
	if !ok {
		return fallback
	}

	knownHostsMu.Lock()
	raw, err := os.ReadFile(khPath)
	knownHostsMu.Unlock()
	if err != nil || len(raw) == 0 {
		return fallback
	}

	type matchedKey struct {
		keyType string
		blob    []byte
	}
	var (
		matched     []matchedKey
		revokedBlob [][]byte
	)
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
		blob := parsedKey.Marshal()
		if marker == "@revoked" {
			revokedBlob = append(revokedBlob, blob)
			continue
		}
		matched = append(matched, matchedKey{keyType: parsedKey.Type(), blob: blob})
	}

	if len(matched) == 0 {
		return fallback
	}

	isRevoked := func(blob []byte) bool {
		for _, r := range revokedBlob {
			if bytes.Equal(r, blob) {
				return true
			}
		}
		return false
	}

	prioritized := make([]string, 0, len(defaultPreferredHostKeyAlgos))
	seen := make(map[string]bool, len(defaultPreferredHostKeyAlgos))
	addAlgo := func(algo string) {
		if algo != "" && !seen[algo] {
			seen[algo] = true
			prioritized = append(prioritized, algo)
		}
	}

	for _, mk := range matched {
		if isRevoked(mk.blob) {
			continue
		}
		for _, algo := range expandHostKeyAlgorithms(mk.keyType) {
			addAlgo(algo)
		}
	}
	if len(prioritized) == 0 {
		return fallback
	}
	for _, algo := range defaultPreferredHostKeyAlgos {
		addAlgo(algo)
	}
	return prioritized
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

		targets, recordToken, displayHost, ok := knownHostTargets(hostname)
		if !ok {
			return fmt.Errorf("无效的目标主机地址: %s", hostname)
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
