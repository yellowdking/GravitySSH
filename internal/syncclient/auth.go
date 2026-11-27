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
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxCustomRelayTOFUPins = 64

var (
	customRelayTOFUMu   sync.Mutex
	customRelayTOFUPins = make(map[string]string)
)

// ClearCustomRelayTOFUPins clears cached in-memory TOFU SPKI pins for custom raw-IP relays.
func ClearCustomRelayTOFUPins() {
	customRelayTOFUMu.Lock()
	clear(customRelayTOFUPins)
	customRelayTOFUMu.Unlock()
}

func spkiSHA256Hex(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

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

func verifyPinnedSPKIs(rawCerts [][]byte, expectedHexes []string, now time.Time) error {
	cert, err := parseValidLeafCert(rawCerts, now)
	if err != nil {
		return err
	}
	gotBytes := []byte(spkiSHA256Hex(cert))
	matched := 0
	for _, expected := range expectedHexes {
		matched |= subtle.ConstantTimeCompare(gotBytes, []byte(expected))
	}
	if matched != 1 {
		return errors.New("中继服务器证书指纹校验失败（SPKI Pin 不匹配）")
	}
	return nil
}

func verifyPinnedSPKI(rawCerts [][]byte, expectedHex string, now time.Time) error {
	return verifyPinnedSPKIs(rawCerts, []string{expectedHex}, now)
}

func verifySelfSignedIPLeafCert(rawCerts [][]byte, expectedIP string, now time.Time) (*x509.Certificate, error) {
	cert, err := parseValidLeafCert(rawCerts, now)
	if err != nil {
		return nil, err
	}
	// Only enforce IP SAN hostname verification when the certificate actually carries IP SANs;
	// default self-hosted bbplin/sshsync containers generate DNSNames:["sshsync"] with empty
	// IPAddresses and remain protected by TLS 1.3 + SPKI TOFU pinning / explicit #spki=.
	if expectedIP != "" && len(cert.IPAddresses) > 0 {
		if err := cert.VerifyHostname(expectedIP); err != nil {
			return nil, fmt.Errorf("自定义中继 TLS 证书 SAN 与目标 IP (%s) 不匹配: %w", expectedIP, err)
		}
	}
	return cert, nil
}

func parseRelayTargetHost(relayURL string) (hostPort, hostname string) {
	u, err := url.Parse(strings.TrimSpace(relayURL))
	if err != nil {
		return "", ""
	}
	return strings.ToLower(u.Host), u.Hostname()
}

func buildTLSConfig(mode RelayTLSMode, spkiSHA256, relayURL string) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	targetHostPort, targetHostname := parseRelayTargetHost(relayURL)
	switch mode {
	case RelayTLSModePinned:
		pins, err := ParseSPKIPinList(spkiSHA256)
		if err != nil {
			return nil, err
		}
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyPinnedSPKIs(rawCerts, pins, time.Now())
		}
	case RelayTLSModeSelfSignedIP:
		ip := net.ParseIP(targetHostname)
		if targetHostname != "" && ip == nil {
			return nil, errors.New("自签名 IP 校验模式仅适用于字面 IP 地址中继")
		}
		expectedIP := ""
		if ip != nil {
			expectedIP = ip.String()
		}
		isLoopback := ip != nil && ip.IsLoopback()
		var (
			localPinMu sync.Mutex
			localPin   string
		)
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			cert, err := verifySelfSignedIPLeafCert(rawCerts, expectedIP, time.Now())
			if err != nil {
				return err
			}
			gotHex := spkiSHA256Hex(cert)
			if isLoopback || targetHostPort == "" {
				localPinMu.Lock()
				defer localPinMu.Unlock()
				if localPin == "" {
					localPin = gotHex
					return nil
				}
				if subtle.ConstantTimeCompare([]byte(gotHex), []byte(localPin)) != 1 {
					return errors.New("自定义中继证书指纹发生变更（TOFU 校验失败），请核实中继服务器或使用 #spki= 显式固定指纹")
				}
				return nil
			}
			customRelayTOFUMu.Lock()
			defer customRelayTOFUMu.Unlock()
			if existing, ok := customRelayTOFUPins[targetHostPort]; ok {
				if subtle.ConstantTimeCompare([]byte(gotHex), []byte(existing)) != 1 {
					return errors.New("自定义中继证书指纹发生变更（TOFU 校验失败），请核实中继服务器或使用 #spki= 显式固定指纹")
				}
				return nil
			}
			if len(customRelayTOFUPins) >= maxCustomRelayTOFUPins {
				clear(customRelayTOFUPins)
			}
			customRelayTOFUPins[targetHostPort] = gotHex
			return nil
		}
	case RelayTLSModeSystemCA:
		// Standard TLS 1.3 verification using system root CAs.
		if targetHostname != "" {
			tlsCfg.ServerName = targetHostname
		}
	default:
		return nil, fmt.Errorf("不支持的中继 TLS 校验模式: %d", mode)
	}
	return tlsCfg, nil
}

// SignRequest computes the X-GSSH-Sig HMAC-SHA256 signature for gssh-sync-v2.
func SignRequest(appGateKey, method, path, ts string, rawBody []byte) string {
	bodySum := sha256.Sum256(rawBody)
	msg := method + "\n" + path + "\n" + ts + "\n" + hex.EncodeToString(bodySum[:])
	mac := hmac.New(sha256.New, []byte(appGateKey))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}
