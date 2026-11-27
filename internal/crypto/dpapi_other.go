//go:build !windows

package crypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	keyringServiceName = "GravitySSH"
	keyringAccountName = "vault-device-key-v1"
	deviceKeySize      = 32
	keyringCmdTimeout  = 3 * time.Second
)

var (
	errDeviceKeyNotFound = errors.New("device key not found")
	defaultProtectorMu   sync.Mutex
)

type deviceKeyStore interface {
	EngineName() string
	Load() ([]byte, error)
	Store(key []byte) error
}

// deviceKeyProtector wraps vault master keys with a 256-bit CSPRNG device key
// stored in the OS Keychain (macOS), Secret Service (Linux desktop), or a
// per-user 0600 CSPRNG key file under os.UserConfigDir()/GravitySSH/.device_key.
type deviceKeyProtector struct {
	mu       sync.Mutex
	backends []deviceKeyStore
	key      []byte
	engine   string
}

// NewDefaultProtector returns a non-Windows device key protector backed by OS
// Keychain / Secret Service with a per-user 0600 CSPRNG key file fallback.
func NewDefaultProtector() KeyProtector {
	return newDeviceKeyProtector(defaultNonWindowsBackends())
}

func newDeviceKeyProtector(backends []deviceKeyStore) *deviceKeyProtector {
	return &deviceKeyProtector{
		backends: backends,
	}
}

func defaultNonWindowsBackends() []deviceKeyStore {
	var backends []deviceKeyStore
	switch runtime.GOOS {
	case "darwin":
		if path, err := exec.LookPath("security"); err == nil && path != "" {
			backends = append(backends, &darwinKeychainStore{binPath: path})
		}
	case "linux":
		if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
			if path, err := exec.LookPath("secret-tool"); err == nil && path != "" {
				backends = append(backends, &linuxSecretServiceStore{binPath: path})
			}
		}
	}
	backends = append(backends, &fileDeviceKeyStore{})
	return backends
}

func parseDeviceKeyHex(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) != deviceKeySize*2 {
		return nil, errors.New("invalid device key length")
	}
	key, err := hex.DecodeString(trimmed)
	if err != nil || len(key) != deviceKeySize {
		return nil, errors.New("invalid device key hex encoding")
	}
	return key, nil
}

func generateRandomDeviceKey() ([]byte, error) {
	key := make([]byte, deviceKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate random device key: %w", err)
	}
	return key, nil
}

func (p *deviceKeyProtector) ensureKey() ([]byte, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.key) == deviceKeySize {
		return p.key, p.engine, nil
	}

	// Serialize cross-instance lazy initialization within the same process so
	// concurrent callers share the exact same resolved key without races.
	defaultProtectorMu.Lock()
	defer defaultProtectorMu.Unlock()

	var (
		writable []deviceKeyStore
		lastErr  error
	)
	for _, b := range p.backends {
		key, err := b.Load()
		if err == nil && len(key) == deviceKeySize {
			p.key = key
			p.engine = b.EngineName()
			return p.key, p.engine, nil
		}
		if errors.Is(err, errDeviceKeyNotFound) {
			writable = append(writable, b)
		} else {
			lastErr = err
		}
	}

	candidate, err := generateRandomDeviceKey()
	if err != nil {
		return nil, "", err
	}
	for _, b := range writable {
		if err := b.Store(candidate); err == nil {
			// Re-load if another process won the creation race (e.g. atomic file link).
			if persisted, lErr := b.Load(); lErr == nil && len(persisted) == deviceKeySize {
				clear(candidate)
				p.key = persisted
				p.engine = b.EngineName()
				return p.key, p.engine, nil
			}
			p.key = candidate
			p.engine = b.EngineName()
			return p.key, p.engine, nil
		} else {
			lastErr = err
		}
	}
	clear(candidate)
	if lastErr != nil {
		return nil, "", fmt.Errorf("initialize device key store: %w", lastErr)
	}
	return nil, "", errors.New("no usable device key store available")
}

func (p *deviceKeyProtector) Protect(plaintext []byte) ([]byte, error) {
	key, _, err := p.ensureKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, []byte("GSV1-WRAP")), nil
}

func (p *deviceKeyProtector) Unprotect(ciphertext []byte) ([]byte, error) {
	key, _, err := p.ensureKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("wrapped key too short")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	return gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], []byte("GSV1-WRAP"))
}

func (p *deviceKeyProtector) EngineName() string {
	if _, engine, err := p.ensureKey(); err == nil && engine != "" {
		return engine
	}
	if len(p.backends) > 0 {
		return p.backends[len(p.backends)-1].EngineName()
	}
	return "Device KeyStore + AES-256-GCM"
}

func (p *deviceKeyProtector) IsMachineBound() bool { return true }
func (p *deviceKeyProtector) IsUserBound() bool    { return true }

// darwinKeychainStore stores the random 256-bit device key in the macOS Login Keychain
// via /usr/bin/security, passing secrets over stdin (`security -i`) so they never appear in argv.
type darwinKeychainStore struct {
	binPath string
}

func (d *darwinKeychainStore) EngineName() string {
	return "macOS Keychain + AES-256-GCM"
}

func (d *darwinKeychainStore) Load() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyringCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.binPath, "find-generic-password", "-s", keyringServiceName, "-a", keyringAccountName, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		errText := strings.ToLower(stderr.String())
		if strings.Contains(errText, "could not be found") || strings.Contains(errText, "item not found") {
			return nil, errDeviceKeyNotFound
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 44 {
			return nil, errDeviceKeyNotFound
		}
		return nil, fmt.Errorf("macOS keychain lookup failed: %w", err)
	}
	key, parseErr := parseDeviceKeyHex(stdout.String())
	clear(stdout.Bytes())
	return key, parseErr
}

func (d *darwinKeychainStore) Store(key []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), keyringCmdTimeout)
	defer cancel()
	hexKey := hex.EncodeToString(key)
	stdinCmd := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", keyringServiceName, keyringAccountName, hexKey)
	stdinBuf := []byte(stdinCmd)
	defer clear(stdinBuf)

	cmd := exec.CommandContext(ctx, d.binPath, "-i")
	cmd.Stdin = bytes.NewReader(stdinBuf)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("macOS keychain store failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// linuxSecretServiceStore stores the random 256-bit device key via Freedesktop Secret Service
// (`secret-tool`), passing secrets exclusively over stdin.
type linuxSecretServiceStore struct {
	binPath string
}

func (l *linuxSecretServiceStore) EngineName() string {
	return "Linux Secret Service + AES-256-GCM"
}

func (l *linuxSecretServiceStore) Load() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyringCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, l.binPath, "lookup", "service", keyringServiceName, "account", keyringAccountName)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(stderr.String()) == "" {
			return nil, errDeviceKeyNotFound
		}
		return nil, fmt.Errorf("secret-tool lookup failed: %w", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		return nil, errDeviceKeyNotFound
	}
	key, parseErr := parseDeviceKeyHex(stdout.String())
	clear(stdout.Bytes())
	return key, parseErr
}

func (l *linuxSecretServiceStore) Store(key []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), keyringCmdTimeout)
	defer cancel()
	hexBytes := []byte(hex.EncodeToString(key))
	defer clear(hexBytes)

	cmd := exec.CommandContext(ctx, l.binPath, "store", "--label=GravitySSH Vault Device Key", "service", keyringServiceName, "account", keyringAccountName)
	cmd.Stdin = bytes.NewReader(hexBytes)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("secret-tool store failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// fileDeviceKeyStore stores a per-user 0600 CSPRNG key under
// os.UserConfigDir()/GravitySSH/.device_key for headless environments.
type fileDeviceKeyStore struct {
	pathOverride string
}

func (f *fileDeviceKeyStore) EngineName() string {
	return "Per-User CSPRNG KeyFile (0600) + AES-256-GCM"
}

func (f *fileDeviceKeyStore) keyPath() (string, error) {
	if strings.TrimSpace(f.pathOverride) != "" {
		return f.pathOverride, nil
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(cfgDir) == "" {
		home, hErr := os.UserHomeDir()
		if hErr != nil || strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		cfgDir = filepath.Join(home, ".config")
	}
	return filepath.Join(cfgDir, keyringServiceName, ".device_key"), nil
}

func (f *fileDeviceKeyStore) Load() ([]byte, error) {
	p, err := f.keyPath()
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errDeviceKeyNotFound
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("device key path is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(p, 0o600)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errDeviceKeyNotFound
		}
		return nil, err
	}
	defer clear(raw)
	return parseDeviceKeyHex(string(raw))
}

func (f *fileDeviceKeyStore) Store(key []byte) error {
	p, err := f.keyPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create device key directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".device_key-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp device key: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp device key: %w", err)
	}
	encoded := []byte(hex.EncodeToString(key) + "\n")
	defer clear(encoded)
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp device key: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp device key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp device key: %w", err)
	}

	// Atomically publish only if .device_key does not yet exist so concurrent processes
	// never clobber an already-initialized key.
	if err := os.Link(tmpPath, p); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		// Fallback for filesystems that disallow hard links.
		excl, oErr := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if oErr != nil {
			if errors.Is(oErr, os.ErrExist) {
				return nil
			}
			return fmt.Errorf("commit device key file: %w", err)
		}
		_, wErr := excl.Write(encoded)
		sErr := excl.Sync()
		cErr := excl.Close()
		if wErr != nil || sErr != nil || cErr != nil {
			_ = os.Remove(p)
			return fmt.Errorf("write device key file: %v / %v / %v", wErr, sErr, cErr)
		}
	}
	return nil
}
