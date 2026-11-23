package sshkey

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerateAndParseEd25519(t *testing.T) {
	pemStr, info, err := GenerateKeyPair("ed25519", "test@gravityssh", "")
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	if info.KeyType != "ssh-ed25519" {
		t.Fatalf("unexpected key type: %s", info.KeyType)
	}
	if !strings.HasPrefix(info.Fingerprint, "SHA256:") {
		t.Fatalf("unexpected fingerprint: %s", info.Fingerprint)
	}
	if !strings.Contains(info.PublicKey, "test@gravityssh") {
		t.Fatalf("expected comment in public key: %s", info.PublicKey)
	}

	parsed, err := ParsePrivateKey(pemStr, "")
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v", err)
	}
	if parsed.Fingerprint != info.Fingerprint {
		t.Fatalf("fingerprint mismatch: %s vs %s", parsed.Fingerprint, info.Fingerprint)
	}
}

func TestGenerateAndParseEncryptedKey(t *testing.T) {
	pemStr, info, err := GenerateKeyPair("ed25519", "enc@gravityssh", "my-pass-123")
	if err != nil {
		t.Fatalf("GenerateKeyPair with passphrase failed: %v", err)
	}

	if _, err := ParsePrivateKey(pemStr, ""); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("expected ErrPassphraseRequired when passphrase omitted, got %v", err)
	}
	if _, err := ParsePrivateKey(pemStr, "wrong-pass"); err == nil {
		t.Fatal("expected error with wrong passphrase")
	}

	parsed, err := ParsePrivateKey(pemStr, "my-pass-123")
	if err != nil {
		t.Fatalf("ParsePrivateKey with correct passphrase failed: %v", err)
	}
	if parsed.Fingerprint != info.Fingerprint {
		t.Fatalf("fingerprint mismatch")
	}
}

func TestGenerateRSA2048(t *testing.T) {
	pemStr, info, err := GenerateKeyPair("rsa2048", "rsa@gravityssh", "")
	if err != nil {
		t.Fatalf("GenerateKeyPair rsa2048 failed: %v", err)
	}
	if info.KeyType != "ssh-rsa" {
		t.Fatalf("unexpected key type: %s", info.KeyType)
	}
	if _, err := ParsePrivateKey(pemStr, ""); err != nil {
		t.Fatalf("ParsePrivateKey rsa failed: %v", err)
	}
}
