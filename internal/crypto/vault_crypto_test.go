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
	if !bytes.Equal(sealed[:4], vaultMagicV2) || sealed[4] != ModeDPAPI {
		t.Fatalf("expected GSV2 ModeDPAPI header, got %x", sealed[:5])
	}
	if bytes.Contains(sealed, []byte("SUPERSECRET")) || bytes.Contains(sealed, []byte("BEGIN OPENSSH")) {
		t.Fatal("plaintext leaked into sealed vault blob")
	}

	isPw, err := enc.IsPasswordProtected(sealed)
	if err != nil || isPw {
		t.Fatalf("expected ModeDPAPI not password-protected, got (%v, %v)", isPw, err)
	}

	opened, err := enc.Open(sealed)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if !bytes.Equal(opened, secret) {
		t.Fatalf("decrypted mismatch: got %q, want %q", opened, secret)
	}
}

func TestEnvelopeGSV1BackwardCompatibility(t *testing.T) {
	enc := NewEnvelopeCrypto(NewDefaultProtector())
	legacySecret := []byte(`{"version":1,"servers":[{"name":"legacy-srv"}]}`)

	v1Blob, err := enc.sealV1(legacySecret)
	if err != nil {
		t.Fatalf("sealV1 failed: %v", err)
	}
	if !bytes.Equal(v1Blob[:4], vaultMagicV1) {
		t.Fatalf("expected GSV1 magic, got %q", v1Blob[:4])
	}

	isPw, err := enc.IsPasswordProtected(v1Blob)
	if err != nil || isPw {
		t.Fatalf("expected GSV1 IsPasswordProtected=false, got (%v, %v)", isPw, err)
	}

	opened, err := enc.Open(v1Blob)
	if err != nil {
		t.Fatalf("Open GSV1 failed: %v", err)
	}
	if !bytes.Equal(opened, legacySecret) {
		t.Fatalf("GSV1 decrypted mismatch: got %q, want %q", opened, legacySecret)
	}
}

func TestEnvelopePasswordDualLayer(t *testing.T) {
	enc := NewEnvelopeCrypto(NewDefaultProtector())
	secret := []byte(`{"version":2,"servers":[{"name":"prod-db","password":"top-secret"}]}`)

	sealed, err := enc.SealWithPassword(secret, "MyStrongPass!123")
	if err != nil {
		t.Fatalf("SealWithPassword failed: %v", err)
	}
	if !bytes.Equal(sealed[:4], vaultMagicV2) || sealed[4] != ModePasswordDPAPI {
		t.Fatalf("expected GSV2 ModePasswordDPAPI header, got %x", sealed[:5])
	}

	isPw, err := enc.IsPasswordProtected(sealed)
	if err != nil || !isPw {
		t.Fatalf("expected IsPasswordProtected=true, got (%v, %v)", isPw, err)
	}

	// Empty password must fail with ErrPasswordRequired
	if _, err := enc.Open(sealed); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("expected ErrPasswordRequired for empty password, got %v", err)
	}

	// Wrong password must fail with ErrWrongPassword
	if _, err := enc.OpenWithPassword(sealed, "wrong-pass"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword for wrong password, got %v", err)
	}

	// Correct password unlocks cleanly
	opened, err := enc.OpenWithPassword(sealed, "MyStrongPass!123")
	if err != nil {
		t.Fatalf("OpenWithPassword failed: %v", err)
	}
	if !bytes.Equal(opened, secret) {
		t.Fatalf("decrypted mismatch: got %q, want %q", opened, secret)
	}

	// Even with correct password, foreign machine fails DPAPI layer
	foreign := NewEnvelopeCrypto(&mockOtherMachineProtector{})
	if _, err := foreign.OpenWithPassword(sealed, "MyStrongPass!123"); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("expected ErrDecryptFailed on foreign machine, got %v", err)
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
