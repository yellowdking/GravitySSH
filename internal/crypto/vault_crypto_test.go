package crypto

import (
	"bytes"
	"errors"
	"testing"
)

type mockOtherMachineProtector struct{}

func (m *mockOtherMachineProtector) Protect(p []byte) ([]byte, error) { return append([]byte{1}, p...), nil }
func (m *mockOtherMachineProtector) Unprotect(c []byte) ([]byte, error) {
	return nil, errors.New("different machine/user cannot unprotect")
}
func (m *mockOtherMachineProtector) EngineName() string   { return "mock-other" }
func (m *mockOtherMachineProtector) IsMachineBound() bool { return true }
func (m *mockOtherMachineProtector) IsUserBound() bool    { return true }

func TestEnvelopeSealAndOpenRoundTrip(t *testing.T) {
	enc := NewEnvelopeCrypto(NewDefaultProtector())
	secret := []byte(`{"sshKeys":[{"privateKeyPem":"-----BEGIN OPENSSH PRIVATE KEY-----\nSUPERSECRET\n-----END OPENSSH PRIVATE KEY-----"}]}`)

	sealed, err := enc.Seal(secret)
	if err != nil {
		t.Fatalf("Seal failed: %v", err)
	}
	if bytes.Contains(sealed, []byte("SUPERSECRET")) || bytes.Contains(sealed, []byte("BEGIN OPENSSH")) {
		t.Fatal("plaintext leaked into sealed vault blob")
	}

	opened, err := enc.Open(sealed)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if !bytes.Equal(opened, secret) {
		t.Fatalf("decrypted mismatch: got %q, want %q", opened, secret)
	}
}

func TestEnvelopeRejectsTampering(t *testing.T) {
	enc := NewEnvelopeCrypto(NewDefaultProtector())
	sealed, err := enc.Seal([]byte("sensitive-shortcuts-and-keys"))
	if err != nil {
		t.Fatalf("Seal failed: %v", err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := enc.Open(tampered); err == nil {
		t.Fatal("expected tampered ciphertext to fail decryption")
	}

	badMagic := append([]byte(nil), sealed...)
	badMagic[0] = 'X'
	if _, err := enc.Open(badMagic); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat for bad magic, got %v", err)
	}
}

func TestEnvelopeRejectsCopiedFileOnAnotherMachine(t *testing.T) {
	machineA := NewEnvelopeCrypto(NewDefaultProtector())
	sealed, err := machineA.Seal([]byte("private-key-material"))
	if err != nil {
		t.Fatalf("Seal failed: %v", err)
	}

	machineB := NewEnvelopeCrypto(&mockOtherMachineProtector{})
	if _, err := machineB.Open(sealed); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("expected ErrDecryptFailed on foreign machine, got %v", err)
	}
}
