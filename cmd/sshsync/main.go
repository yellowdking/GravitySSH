package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	args := os.Args[1:]
	initSubcmd := false
	if len(args) > 0 && args[0] == "init" {
		initSubcmd = true
		args = args[1:]
	}

	fs := flag.NewFlagSet("sshsync", flag.ExitOnError)
	initOnly := fs.Bool("init", initSubcmd, "generate TLS cert, SPKI pin, and app-gate key if missing, then exit")
	healthcheck := fs.Bool("healthcheck", false, "probe local sshsync TLS /v1/ping endpoint and exit 0 if healthy")
	baseDir := fs.String("dir", envOr("SSHSYNC_DIR", "/opt/sshsync"), "directory for certs/ and app-gate.key")
	listenAddr := fs.String("listen", envOr("SSHSYNC_LISTEN", ":54843"), "TLS listen address")
	sanIP := fs.String("san-ip", envOr("SSHSYNC_SAN_IP", "sshsync"), "SAN IP or hostname for generated certificate")
	certFlag := fs.String("cert", os.Getenv("SSHSYNC_CERT_FILE"), "TLS certificate PEM path")
	keyFlag := fs.String("key", os.Getenv("SSHSYNC_KEY_FILE"), "TLS private key PEM path")
	gateKeyFlag := fs.String("gate-key", os.Getenv("SSHSYNC_APP_GATE_KEY"), "64-hex app gate HMAC key")
	_ = fs.Parse(args)

	if *healthcheck {
		var candidates []string
		if cp := strings.TrimSpace(*certFlag); cp != "" {
			candidates = append(candidates, filepath.Join(filepath.Dir(cp), "app-gate.key"))
		}
		candidates = append(candidates, filepath.Join(*baseDir, "app-gate.key"))
		appGateKey, err := resolveGateKeyQuiet(*gateKeyFlag, false, candidates...)
		if err != nil {
			log.Fatalf("healthcheck gate key failed: %v", err)
		}
		if err := runHealthcheck(*listenAddr, appGateKey, 3*time.Second); err != nil {
			log.Fatalf("healthcheck failed: %v", err)
		}
		return
	}

	if *initOnly {
		assets, err := ensureAssets(*baseDir, *sanIP)
		if err != nil {
			log.Fatalf("init assets failed: %v", err)
		}
		fmt.Printf("CERT=%s\nKEY=%s\nSPKI_SHA256=%s\nAPP_GATE_KEY_FILE=%s\n",
			assets.CertPath, assets.KeyPath, assets.SPKIHex, assets.AppKeyPath)
		return
	}

	certPath, keyPath, err := resolveCertKeyPaths(*certFlag, *keyFlag, *baseDir)
	if err != nil {
		log.Fatalf("tls config failed: %v", err)
	}

	cert, spkiHex, _, err := loadRuntimeTLS(certPath, keyPath, *sanIP)
	if err != nil {
		log.Fatalf("tls startup failed: %v", err)
	}

	appGateKey, err := resolveGateKey(
		*gateKeyFlag,
		filepath.Join(filepath.Dir(certPath), "app-gate.key"),
		filepath.Join(*baseDir, "app-gate.key"),
	)
	if err != nil {
		log.Fatalf("app gate key failed: %v", err)
	}

	certs := newCertStore(certPath, keyPath, cert)
	tlsCfg := &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: certs.GetCertificate,
	}

	store := newMemStore(nil)
	guard := newIPGuard(nil)

	rawLn, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", *listenAddr, err)
	}
	ln := tls.NewListener(newLimitListener(rawLn, maxActiveConns, maxConnsPerIP, guard), tlsCfg)

	stopCh := make(chan struct{})
	store.StartSweeper(stopCh)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				guard.Sweep()
			case <-stopCh:
				return
			}
		}
	}()

	reloadCh, stopReloadSignal := startReloadSignal()
	defer stopReloadSignal()
	reloadDone := startCertReloader(certs, reloadCh, stopCh, nil)

	handler := newRelayServer(store, guard, appGateKey, nil)
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8192,
		ErrorLog:          log.New(io.Discard, "", 0),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	log.Printf("sshsync listening on %s (TLS 1.3, SPKI=%s)", *listenAddr, spkiHex)
	serveErr := serveWithGracefulShutdown(srv, ln, store, stopCh, sigCh, 5*time.Second)
	stopReloadSignal()
	<-reloadDone
	if serveErr != nil {
		log.Fatalf("serve error: %v", serveErr)
	}
}

func serveWithGracefulShutdown(
	srv *http.Server,
	ln net.Listener,
	store *memStore,
	stopCh chan struct{},
	sigCh <-chan os.Signal,
	shutdownTimeout time.Duration,
) error {
	serveDone := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-sigCh:
			close(stopCh)
			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				_ = srv.Close()
			}
			store.Close()
		case <-serveDone:
			close(stopCh)
			store.Close()
		}
	}()

	err := srv.Serve(ln)
	close(serveDone)
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func healthcheckTargetAddr(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", listenAddr, err)
	}
	if port == "" {
		return "", fmt.Errorf("missing port in listen address %q", listenAddr)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return net.JoinHostPort(host, port), nil
}

func runHealthcheck(listenAddr, appGateKey string, timeout time.Duration) error {
	target, err := healthcheckTargetAddr(listenAddr)
	if err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	body := []byte("{}")
	tsStr := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	bodySum := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(appGateKey))
	mac.Write([]byte(http.MethodPost + "\n/v1/ping\n" + tsStr + "\n" + hex.EncodeToString(bodySum[:])))
	sigHex := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, "https://"+target+"/v1/ping", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(clientHeaderName, clientHeaderValue)
	req.Header.Set(tsHeaderName, tsStr)
	req.Header.Set(sigHeaderName, sigHex)
	req.Header.Set(protoHeaderName, strconv.Itoa(MaxProtoVersion))

	tr := &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true, // Local loopback self-probe for scratch container HEALTHCHECK.
		},
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s: %w", target, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("read probe response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || !out.OK {
		return fmt.Errorf("unexpected probe payload: %q", string(raw))
	}
	return nil
}
