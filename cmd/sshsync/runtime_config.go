package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const defaultAppGateKey = "fb6f39e339dffe1141537e5e8154491cce0044c7bc2ecb26b17828571b9d5aae"

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func resolveGateKey(explicit string, candidateFiles ...string) (string, error) {
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
	log.Printf("WARNING: sshsync is using the built-in default app-gate key; set SSHSYNC_APP_GATE_KEY or provide app-gate.key for private deployments")
	return defaultAppGateKey, nil
}

func loadRuntimeTLS(certFile, keyFile, sanHost string) (tls.Certificate, string, bool, error) {
	if certFile != "" && keyFile != "" && fileExists(certFile) && fileExists(keyFile) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return tls.Certificate{}, "", false, fmt.Errorf("load tls keypair: %w", err)
		}
		spkiHex, err := spkiFromTLSCert(cert)
		if err != nil {
			return tls.Certificate{}, "", false, err
		}
		return cert, spkiHex, true, nil
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

func spkiFromTLSCert(cert tls.Certificate) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", errors.New("empty tls certificate chain")
	}
	return spkiHexFromDER(cert.Certificate[0])
}
