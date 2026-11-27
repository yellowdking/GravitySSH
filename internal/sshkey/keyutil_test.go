package sshkey

import (
	"crypto/rand"
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

	if info.Signer != nil {
		t.Fatal("expected GenerateKeyPair info.Signer to be nil after private key zeroization")
	}

	parsed, err := ParsePrivateKey(pemStr, "")
	if err != nil {
		t.Fatalf("ParsePrivateKey failed: %v", err)
	}
	if parsed.Fingerprint != info.Fingerprint {
		t.Fatalf("fingerprint mismatch: %s vs %s", parsed.Fingerprint, info.Fingerprint)
	}
	sig, err := parsed.Signer.Sign(rand.Reader, []byte("gravityssh-sign-check"))
	if err != nil {
		t.Fatalf("parsed.Signer.Sign failed: %v", err)
	}
	if err := parsed.Signer.PublicKey().Verify([]byte("gravityssh-sign-check"), sig); err != nil {
		t.Fatalf("parsed.Signer.PublicKey().Verify failed: %v", err)
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

func TestGenerateSanitizesMultilineComment(t *testing.T) {
	_, info, err := GenerateKeyPair("ed25519", "user@host\r\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA evil", "")
	if err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}
	if strings.ContainsAny(info.PublicKey, "\r\n") {
		t.Fatalf("expected sanitized single-line public key, got %q", info.PublicKey)
	}
}

func TestSanitizeCommentStripsAllControlChars(t *testing.T) {
	got := SanitizeComment("  alice\x00@host\t\x1b[31m\x7f\u0085\r\nbob  ")
	if got != "alice@host[31mbob" {
		t.Fatalf("SanitizeComment = %q, want %q", got, "alice@host[31mbob")
	}
}
