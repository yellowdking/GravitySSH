package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAppGateKey = "fb6f39e339dffe1141537e5e8154491cce0044c7bc2ecb26b17828571b9d5aae"
	certExpiryWarnTTL = 30 * 24 * time.Hour
)

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func resolveCertKeyPaths(certFlag, keyFlag, baseDir string) (string, string, error) {
	certPath := strings.TrimSpace(certFlag)
	keyPath := strings.TrimSpace(keyFlag)
	if (certPath == "") != (keyPath == "") {
		return "", "", errors.New("both --cert (SSHSYNC_CERT_FILE) and --key (SSHSYNC_KEY_FILE) must be set together")
	}
	if certPath == "" && keyPath == "" {
		certPath = filepath.Join(baseDir, "certs", "server.crt")
		keyPath = filepath.Join(baseDir, "certs", "server.key")
	}
	return certPath, keyPath, nil
}

func resolveGateKey(explicit string, candidateFiles ...string) (string, error) {
	return resolveGateKeyQuiet(explicit, true, candidateFiles...)
}

func resolveGateKeyQuiet(explicit string, warnDefault bool, candidateFiles ...string) (string, error) {
	if k := strings.TrimSpace(explicit); k != "" {
		if !isLowerHex64(k) {
			return "", errors.New("app gate key must be 64 lowercase hex chars")
		}
		return k, nil
	}
	for _, path := range candidateFiles {
		if path == "" || !fileExists(path) {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		k := strings.TrimSpace(string(b))
		clear(b)
		if !isLowerHex64(k) {
			return "", fmt.Errorf("%s must contain 64 lowercase hex chars", path)
		}
		return k, nil
	}
	if warnDefault {
		log.Printf("INFO: sshsync using built-in default app-gate key (required for standard GravitySSH clients; override via SSHSYNC_APP_GATE_KEY only with matching custom client builds)")
	}
	return defaultAppGateKey, nil
}

func loadRuntimeTLS(certFile, keyFile, sanHost string) (tls.Certificate, string, bool, error) {
	return loadRuntimeTLSAt(certFile, keyFile, sanHost, time.Now().UTC())
}

func loadRuntimeTLSAt(certFile, keyFile, sanHost string, now time.Time) (tls.Certificate, string, bool, error) {
	certFile = strings.TrimSpace(certFile)
	keyFile = strings.TrimSpace(keyFile)
	if (certFile == "") != (keyFile == "") {
		return tls.Certificate{}, "", false, errors.New("both cert and key paths must be provided together")
	}
	if certFile != "" && keyFile != "" {
		certExists := fileExists(certFile)
		keyExists := fileExists(keyFile)
		if certExists != keyExists {
			return tls.Certificate{}, "", false, fmt.Errorf("incomplete tls keypair on disk (cert exists=%v, key exists=%v)", certExists, keyExists)
		}
		if certExists && keyExists {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return tls.Certificate{}, "", false, fmt.Errorf("load tls keypair: %w", err)
			}
			spkiHex, err := validateAndExtractSPKI(cert, now)
			if err != nil {
				return tls.Certificate{}, "", false, err
			}
			return cert, spkiHex, true, nil
		}
	}

	certPEM, keyPEM, spkiHex, err := generateSelfSignedCertBytes(sanHost)
	if err != nil {
		return tls.Certificate{}, "", false, err
	}
	defer clear(keyPEM)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", false, fmt.Errorf("parse generated keypair: %w", err)
	}

	persisted := false
	if certFile != "" && keyFile != "" {
		if err := persistKeyPair(certFile, keyFile, certPEM, keyPEM); err != nil {
			log.Printf("sshsync using in-memory self-signed TLS cert (cannot write %s: %v)", certFile, err)
		} else {
			persisted = true
		}
	}
	return cert, spkiHex, persisted, nil
}

func persistKeyPair(certFile, keyFile string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(filepath.Dir(certFile), 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		_ = os.Remove(certFile)
		return err
	}
	return nil
}

func validateAndExtractSPKI(cert tls.Certificate, now time.Time) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", errors.New("empty tls certificate chain")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return "", fmt.Errorf("parse cert: %w", err)
	}
	nowUTC := now.UTC()
	if nowUTC.Before(leaf.NotBefore) {
		return "", fmt.Errorf("tls certificate is not yet valid (NotBefore=%s, now=%s)",
			leaf.NotBefore.UTC().Format(time.RFC3339), nowUTC.Format(time.RFC3339))
	}
	if !nowUTC.Before(leaf.NotAfter) {
		return "", fmt.Errorf("tls certificate has expired (NotAfter=%s, now=%s)",
			leaf.NotAfter.UTC().Format(time.RFC3339), nowUTC.Format(time.RFC3339))
	}
	if remaining := leaf.NotAfter.Sub(nowUTC); remaining < certExpiryWarnTTL {
		log.Printf("WARNING: tls certificate expires in %s (NotAfter=%s)",
			remaining.Round(time.Minute), leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:]), nil
}

func spkiFromTLSCert(cert tls.Certificate) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", errors.New("empty tls certificate chain")
	}
	return spkiHexFromDER(cert.Certificate[0])
}
