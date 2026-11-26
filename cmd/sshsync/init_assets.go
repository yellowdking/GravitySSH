package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type relayAssets struct {
	CertPath   string
	KeyPath    string
	SPKIPath   string
	AppKeyPath string
	SPKIHex    string
	AppGateKey string
}

func ensureAssets(baseDir, sanIP string) (*relayAssets, error) {
	certsDir := filepath.Join(baseDir, "certs")
	a := &relayAssets{
		CertPath:   filepath.Join(certsDir, "server.crt"),
		KeyPath:    filepath.Join(certsDir, "server.key"),
		SPKIPath:   filepath.Join(certsDir, "spki-sha256.txt"),
		AppKeyPath: filepath.Join(baseDir, "app-gate.key"),
	}

	certExists := fileExists(a.CertPath)
	keyExists := fileExists(a.KeyPath)
	if !certExists || !keyExists {
		if err := os.MkdirAll(certsDir, 0o750); err != nil {
			return nil, fmt.Errorf("mkdir certs: %w", err)
		}
		spkiHex, err := generateSelfSignedCert(a.CertPath, a.KeyPath, sanIP)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(a.SPKIPath, []byte(spkiHex+"\n"), 0o644); err != nil {
			return nil, fmt.Errorf("write spki: %w", err)
		}
		a.SPKIHex = spkiHex
	} else {
		spkiHex, err := extractSPKIHex(a.CertPath)
		if err != nil {
			return nil, err
		}
		a.SPKIHex = spkiHex
		if !fileExists(a.SPKIPath) {
			_ = os.WriteFile(a.SPKIPath, []byte(spkiHex+"\n"), 0o644)
		}
	}

	if !fileExists(a.AppKeyPath) {
		if err := os.MkdirAll(baseDir, 0o750); err != nil {
			return nil, fmt.Errorf("mkdir base: %w", err)
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("rand app key: %w", err)
		}
		keyHex := hex.EncodeToString(raw)
		clear(raw)
		if err := os.WriteFile(a.AppKeyPath, []byte(keyHex+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("write app-gate.key: %w", err)
		}
		a.AppGateKey = keyHex
	} else {
		b, err := os.ReadFile(a.AppKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read app-gate.key: %w", err)
		}
		keyHex := strings.TrimSpace(string(b))
		clear(b)
		if !isLowerHex64(keyHex) {
			return nil, errors.New("app-gate.key must be 64 lowercase hex chars")
		}
		a.AppGateKey = keyHex
	}
	return a, nil
}

func generateSelfSignedCertBytes(sanHost string) ([]byte, []byte, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", fmt.Errorf("gen ecdsa key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, "", fmt.Errorf("gen serial: %w", err)
	}
	cn := strings.TrimSpace(sanHost)
	if cn == "" {
		cn = "sshsync"
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(cn); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{cn}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, "", fmt.Errorf("create cert: %w", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, "", fmt.Errorf("parse cert: %w", err)
	}
	sum := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	spkiHex := hex.EncodeToString(sum[:])

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, "", fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	clear(keyDER)
	return certPEM, keyPEM, spkiHex, nil
}

func generateSelfSignedCert(certPath, keyPath, sanIP string) (string, error) {
	certPEM, keyPEM, spkiHex, err := generateSelfSignedCertBytes(sanIP)
	if err != nil {
		return "", err
	}
	defer clear(keyPEM)
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return "", fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return "", fmt.Errorf("write key: %w", err)
	}
	return spkiHex, nil
}

func extractSPKIHex(certPath string) (string, error) {
	b, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("read cert: %w", err)
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "", errors.New("invalid PEM certificate")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse cert: %w", err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:]), nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isLowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
