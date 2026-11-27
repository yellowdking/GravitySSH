//go:build !windows

package crypto

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type memDeviceKeyStore struct {
	mu      sync.Mutex
	engine  string
	key     []byte
	loadErr error
	saveErr error
}

func (m *memDeviceKeyStore) EngineName() string { return m.engine }

func (m *memDeviceKeyStore) Load() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if len(m.key) == 0 {
		return nil, errDeviceKeyNotFound
	}
	out := make([]byte, len(m.key))
	copy(out, m.key)
	return out, nil
}

func (m *memDeviceKeyStore) Store(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.key = make([]byte, len(key))
	copy(m.key, key)
	return nil
}

func TestDeviceKeyProtectorKeyringAndFileFallback(t *testing.T) {
	// 1. Primary OS keystore (e.g. macOS Keychain / Linux Secret Service) stores random key & round-trips.
	primary := &memDeviceKeyStore{engine: "Mock OS Keyring + AES-256-GCM"}
	fallbackPath := filepath.Join(t.TempDir(), "GravitySSH", ".device_key")
	fallback := &fileDeviceKeyStore{pathOverride: fallbackPath}

	p1 := newDeviceKeyProtector([]deviceKeyStore{primary, fallback})
	ct, err := p1.Protect([]byte("master-secret-32-bytes"))
	if err != nil {
		t.Fatalf("Protect via primary keystore failed: %v", err)
	}
	if p1.EngineName() != primary.engine {
		t.Fatalf("expected engine %q, got %q", primary.engine, p1.EngineName())
	}
	if _, err := os.Stat(fallbackPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected fallback file unused when primary succeeded, stat err=%v", err)
	}

	// Second protector instance using same keystore unwraps cleanly.
	p2 := newDeviceKeyProtector([]deviceKeyStore{primary, fallback})
	pt, err := p2.Unprotect(ct)
	if err != nil || string(pt) != "master-secret-32-bytes" {
		t.Fatalf("Unprotect via second instance failed: pt=%q err=%v", pt, err)
	}

	// 2. Headless fallback: when OS keyring is unavailable, uses 0600 CSPRNG file key (never static!).
	brokenKeyring := &memDeviceKeyStore{
		engine:  "Unavailable Keyring",
		loadErr: errors.New("dbus unavailable"),
		saveErr: errors.New("dbus unavailable"),
	}
	fileA := filepath.Join(t.TempDir(), "GravitySSH", ".device_key")
	fileB := filepath.Join(t.TempDir(), "GravitySSH", ".device_key")

	hpA := newDeviceKeyProtector([]deviceKeyStore{brokenKeyring, &fileDeviceKeyStore{pathOverride: fileA}})
	hpB := newDeviceKeyProtector([]deviceKeyStore{brokenKeyring, &fileDeviceKeyStore{pathOverride: fileB}})

	ctA, err := hpA.Protect([]byte("vault-key-material"))
	if err != nil {
		t.Fatalf("Protect via file fallback A failed: %v", err)
	}
	if _, err := hpB.Protect([]byte("vault-key-material")); err != nil {
		t.Fatalf("Protect via file fallback B failed: %v", err)
	}

	// File permissions must be 0600 and keys across independent installs must be random & distinct.
	info, err := os.Stat(fileA)
	if err != nil {
		t.Fatalf("stat .device_key failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected .device_key mode 0600, got %o", perm)
	}
	rawA, _ := os.ReadFile(fileA)
	rawB, _ := os.ReadFile(fileB)
	if bytes.Equal(rawA, rawB) {
		t.Fatal("two fresh .device_key files produced identical keys (static key regression!)")
	}

	// Cross-protector decrypt must fail because keys are per-user random CSPRNG.
	if _, err := hpB.Unprotect(ctA); err == nil {
		t.Fatal("expected distinct random .device_key to reject foreign ciphertext")
	}

	// Concurrent Protect/Unprotect on same file path must be race-free.
	fileC := filepath.Join(t.TempDir(), "GravitySSH", ".device_key")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := newDeviceKeyProtector([]deviceKeyStore{&fileDeviceKeyStore{pathOverride: fileC}})
			c, err := p.Protect([]byte("concurrent-secret"))
			if err != nil {
				errs <- err
				return
			}
			out, err := p.Unprotect(c)
			if err != nil || string(out) != "concurrent-secret" {
				errs <- errors.New("concurrent roundtrip mismatch")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent fileDeviceKeyStore error: %v", err)
	}

	// Non-regular file (directory) at key path must be rejected cleanly.
	dirPath := filepath.Join(t.TempDir(), "dir-as-key")
	if err := os.MkdirAll(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	badP := newDeviceKeyProtector([]deviceKeyStore{&fileDeviceKeyStore{pathOverride: dirPath}})
	if _, err := badP.Protect([]byte("x")); err == nil {
		t.Fatal("expected directory at .device_key path to fail")
	}
}
