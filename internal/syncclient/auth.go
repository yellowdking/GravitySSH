package syncclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

func verifyPinnedSPKI(rawCerts [][]byte, expectedHex string, now time.Time) error {
	if len(rawCerts) == 0 {
		return errors.New("relay presented no TLS certificate")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("parse relay certificate: %w", err)
	}
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return errors.New("relay TLS certificate is expired or not yet valid")
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	gotHex := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(gotHex), []byte(expectedHex)) != 1 {
		return errors.New("中继服务器证书指纹校验失败（SPKI Pin 不匹配）")
	}
	return nil
}

// SignRequest computes the X-GSSH-Sig HMAC-SHA256 signature for gssh-sync-v1.
func SignRequest(appGateKey, method, path, ts string, rawBody []byte) string {
	bodySum := sha256.Sum256(rawBody)
	msg := method + "\n" + path + "\n" + ts + "\n" + hex.EncodeToString(bodySum[:])
	mac := hmac.New(sha256.New, []byte(appGateKey))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}
