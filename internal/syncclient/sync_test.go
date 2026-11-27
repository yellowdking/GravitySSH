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

func TestGoldenDualKeyAndPairRequestVectors(t *testing.T) {
	t1, err := GenerateHexToken32()
	if err != nil || len(t1) != 64 {
		t.Fatalf("GenerateHexToken32 failed: %q err=%v", t1, err)
	}
	t2, err := GenerateHexToken32()
	if err != nil || len(t2) != 64 || t1 == t2 {
		t.Fatalf("GenerateHexToken32 uniqueness/format failed: t1=%q t2=%q err=%v", t1, t2, err)
	}

	sendCode := "K7M9-P4X2"
	recvCode := "M8N2-Q5R7"
	requestID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	salt, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	nonce, _ := hex.DecodeString("a0a1a2a3a4a5a6a7a8a9aaab")
	fixedTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	dualSecret, err := DeriveDualSecret("k7m9-p4x2", "m8n2q5r7")
	if err != nil || dualSecret != "K7M9P4X2:M8N2Q5R7" {
		t.Fatalf("DeriveDualSecret mismatch: %q err=%v", dualSecret, err)
	}
	dualAAD := DeriveDualAAD(goldenChannelID, requestID, "M8N2Q5R7")
	wantAAD := goldenChannelID + ":" + requestID + ":M8N2Q5R7"
	if dualAAD != wantAAD {
		t.Fatalf("DeriveDualAAD mismatch: got %q want %q", dualAAD, wantAAD)
	}
	dualKeyHex := hex.EncodeToString(DeriveAESKey(dualSecret, salt))

	// 1. PairRequest deterministic roundtrip
	pairReq := PairRequestPayload{
		ReceiverCode:     recvCode,
		ReceiverDevice:   "Pixel-9",
		ReceiverPlatform: "android",
		RequestedAt:      "2026-01-02T03:04:05Z",
	}
	reqPayload, err := EncryptPairRequestDeterministic(sendCode, pairReq, salt, nonce)
	if err != nil {
		t.Fatalf("EncryptPairRequestDeterministic failed: %v", err)
	}
	if reqPayload.ChannelID != goldenChannelID {
		t.Fatalf("pairRequest channelId mismatch: %s", reqPayload.ChannelID)
	}
	decReq, err := DecryptPairRequest("k7m9-p4x2", reqPayload)
	if err != nil {
		t.Fatalf("DecryptPairRequest failed: %v", err)
	}
	if decReq != pairReq {
		t.Fatalf("DecryptPairRequest mismatch: got %+v want %+v", decReq, pairReq)
	}
	if _, err := DecryptPairRequest("2222-3333", reqPayload); err == nil {
		t.Fatal("expected DecryptPairRequest failure with wrong sendCode")
	}

	// 2. Dual-key SyncBundle deterministic roundtrip
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

	dualPayload, err := EncryptBundleDualDeterministic(sendCode, recvCode, requestID, bundle, salt, nonce)
	if err != nil {
		t.Fatalf("EncryptBundleDualDeterministic failed: %v", err)
	}
	if dualPayload.ChannelID != goldenChannelID {
		t.Fatalf("dualPayload channelId mismatch: %s", dualPayload.ChannelID)
	}

	decDual, err := DecryptBundleDual("k7m9p4x2", "m8n2q5r7", requestID, dualPayload)
	if err != nil {
		t.Fatalf("DecryptBundleDual failed: %v", err)
	}
	if decDual.SourceDevice != "WIN-DEV-01" || len(decDual.Servers) != 1 || decDual.Servers[0].Password != "golden-pass-!@#" {
		t.Fatalf("decrypted dual bundle mismatch: %+v", decDual)
	}
	if _, err := DecryptBundleDual("2222-3333", recvCode, requestID, dualPayload); err == nil {
		t.Fatal("expected DecryptBundleDual failure with wrong sendCode")
	}
	if _, err := DecryptBundleDual(sendCode, "2222-3333", requestID, dualPayload); err == nil {
		t.Fatal("expected DecryptBundleDual failure with wrong recvCode")
	}
	if _, err := DecryptBundleDual(sendCode, recvCode, strings.Repeat("f", 64), dualPayload); err == nil {
		t.Fatal("expected DecryptBundleDual failure with wrong requestID (AAD mismatch)")
	}
	tampered := dualPayload
	tampered.Ciphertext = "AAAA" + dualPayload.Ciphertext[4:]
	if _, err := DecryptBundleDual(sendCode, recvCode, requestID, tampered); err == nil {
		t.Fatal("expected DecryptBundleDual failure with tampered ciphertext")
	}

	t.Logf("GOLDEN_V2_CHANNEL_ID=%s", goldenChannelID)
	t.Logf("GOLDEN_V2_DUAL_SECRET=%s", dualSecret)
	t.Logf("GOLDEN_V2_REQUEST_ID=%s", requestID)
	t.Logf("GOLDEN_V2_DUAL_AAD=%s", dualAAD)
	t.Logf("GOLDEN_V2_DUAL_AES_KEY_HEX=%s", dualKeyHex)
	t.Logf("GOLDEN_V2_SALT_B64=%s", dualPayload.Salt)
	t.Logf("GOLDEN_V2_NONCE_B64=%s", dualPayload.Nonce)
	t.Logf("GOLDEN_V2_PAIR_REQ_CT_B64=%s", reqPayload.Ciphertext)
	t.Logf("GOLDEN_V2_DUAL_BUNDLE_CT_B64=%s", dualPayload.Ciphertext)
}
