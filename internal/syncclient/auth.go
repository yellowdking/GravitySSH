package syncclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

func parseValidLeafCert(rawCerts [][]byte, now time.Time) (*x509.Certificate, error) {
	if len(rawCerts) == 0 {
		return nil, errors.New("relay presented no TLS certificate")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return nil, fmt.Errorf("parse relay certificate: %w", err)
	}
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, errors.New("relay TLS certificate is expired or not yet valid")
	}
	return cert, nil
}

func verifyPinnedSPKI(rawCerts [][]byte, expectedHex string, now time.Time) error {
	cert, err := parseValidLeafCert(rawCerts, now)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	gotHex := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(gotHex), []byte(expectedHex)) != 1 {
		return errors.New("中继服务器证书指纹校验失败（SPKI Pin 不匹配）")
	}
	return nil
}

func verifySelfSignedLeafCert(rawCerts [][]byte, now time.Time) error {
	_, err := parseValidLeafCert(rawCerts, now)
	return err
}

func buildTLSConfig(mode RelayTLSMode, spkiSHA256 string) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	switch mode {
	case RelayTLSModePinned:
		pin := strings.ToLower(strings.TrimSpace(spkiSHA256))
		if len(pin) != 64 {
			return nil, errors.New("内置中继 SPKI 指纹未配置")
		}
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyPinnedSPKI(rawCerts, pin, time.Now())
		}
	case RelayTLSModeSelfSignedIP:
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifySelfSignedLeafCert(rawCerts, time.Now())
		}
	case RelayTLSModeSystemCA:
		// Standard TLS 1.3 verification using system root CAs.
	default:
		return nil, fmt.Errorf("不支持的中继 TLS 校验模式: %d", mode)
	}
	return tlsCfg, nil
}

// SignRequest computes the X-GSSH-Sig HMAC-SHA256 signature for gssh-sync-v1.
func SignRequest(appGateKey, method, path, ts string, rawBody []byte) string {
	bodySum := sha256.Sum256(rawBody)
	msg := method + "\n" + path + "\n" + ts + "\n" + hex.EncodeToString(bodySum[:])
	mac := hmac.New(sha256.New, []byte(appGateKey))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}
