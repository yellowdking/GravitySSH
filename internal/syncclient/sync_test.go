package syncclient

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const (
	goldenChannelID = "dda9c60ab5d048fe1299e0815ff885d13047578a81a4b6969b676820a0371c7e"
	goldenAESKeyHex = "b91a0aab1e53ea797042d28deb026dc6c4264ede7a76ea37cdaaf9e224eaa936"
	goldenHMACSig   = "02e518b092e36f3a78d7e0f782bfa85a186171b74bf062b9f3d10c90a35cba5c"
)

func TestGenerateAndCanonicalizeCode(t *testing.T) {
	code, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode failed: %v", err)
	}
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("unexpected code format: %q", code)
	}
	canonical, err := CanonicalizeCode("  " + strings.ToLower(code) + " ")
	if err != nil || len(canonical) != 8 {
		t.Fatalf("CanonicalizeCode failed: %q err=%v", canonical, err)
	}
	qr, err := FormatQRText(code)
	if err != nil || qr != "gssh-sync://v1?code="+code {
		t.Fatalf("FormatQRText mismatch: %q err=%v", qr, err)
	}
	fromQR, err := CanonicalizeCode(qr)
	if err != nil || fromQR != canonical {
		t.Fatalf("QR parse mismatch: got %q want %q err=%v", fromQR, canonical, err)
	}
	for _, bad := range []string{"", "1234-5678", "OOOO-IIII", "ABCD-EFG", "ABCD-EFGHJ"} {
		if _, err := CanonicalizeCode(bad); err == nil {
			t.Fatalf("expected error for invalid code %q", bad)
		}
	}
}

func TestGoldenVectorAndTamperRejection(t *testing.T) {
	code := "K7M9-P4X2"
	canonical := "K7M9P4X2"
	salt, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	nonce, _ := hex.DecodeString("a0a1a2a3a4a5a6a7a8a9aaab")
	fixedTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	bundle := models.SyncBundle{
		SchemaVersion:  1,
		ExportedAt:     "2026-01-02T03:04:05Z",
		SourcePlatform: "windows",
		SourceDevice:   "WIN-DEV-01",
		Servers: []models.ServerConfig{
			{
				ID: "srv-golden-1", Name: "Prod Node", Group: "Prod", Color: "#6366f1",
				Host: "192.0.2.10", Port: 22, Username: "root",
				AuthType: models.AuthPassword, Password: "golden-pass-!@#",
				Proxy: models.ProxyConfig{Type: models.ProxyNone},
				JumpHost: models.JumpHostConfig{Mode: models.JumpNone},
				TimeoutSeconds: 15, KeepAliveSeconds: 30,
				CreatedAt: fixedTime, UpdatedAt: fixedTime,
			},
		},
		SSHKeys: []models.SSHKeyRecord{},
		Shortcuts: []models.CommandShortcut{
			{
				ID: "sc-golden-1", Name: "Disk", Command: "df -h", Category: "Sys",
				AutoExecute: true, Pinned: true, CreatedAt: fixedTime, UpdatedAt: fixedTime,
			},
		},
		Templates: []models.BuiltinTemplate{},
	}

	payload, err := EncryptBundleDeterministic(code, bundle, salt, nonce)
	if err != nil {
		t.Fatalf("EncryptBundleDeterministic failed: %v", err)
	}
	if payload.ChannelID != goldenChannelID {
		t.Fatalf("channelId mismatch: got %s want %s", payload.ChannelID, goldenChannelID)
	}
	if got := hex.EncodeToString(DeriveAESKey(canonical, salt)); got != goldenAESKeyHex {
		t.Fatalf("aesKey mismatch: got %s want %s", got, goldenAESKeyHex)
	}
	sig := SignRequest("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "POST", "/v1/pickup", "1767323045", []byte(`{"channelId":"abc"}`))
	if sig != goldenHMACSig {
		t.Fatalf("HMAC sig mismatch: got %s want %s", sig, goldenHMACSig)
	}

	dec, err := DecryptBundle("k7m9p4x2", payload)
	if err != nil {
		t.Fatalf("DecryptBundle failed: %v", err)
	}
	if dec.SourceDevice != "WIN-DEV-01" || len(dec.Servers) != 1 || dec.Servers[0].Password != "golden-pass-!@#" {
		t.Fatalf("decrypted bundle mismatch: %+v", dec)
	}
	if _, err := DecryptBundle("2222-3333", payload); err == nil {
		t.Fatal("expected decryption failure with wrong sync code")
	}
	tampered := payload
	tampered.Ciphertext = "AAAA" + payload.Ciphertext[4:]
	if _, err := DecryptBundle(code, tampered); err == nil {
		t.Fatal("expected decryption failure with tampered ciphertext")
	}
}
