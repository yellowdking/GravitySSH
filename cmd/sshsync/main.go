package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	baseDir := fs.String("dir", envOr("SSHSYNC_DIR", "/opt/sshsync"), "directory for certs/ and app-gate.key")
	listenAddr := fs.String("listen", envOr("SSHSYNC_LISTEN", ":54843"), "TLS listen address")
	sanIP := fs.String("san-ip", envOr("SSHSYNC_SAN_IP", "sshsync"), "SAN IP or hostname for generated certificate")
	certFlag := fs.String("cert", os.Getenv("SSHSYNC_CERT_FILE"), "TLS certificate PEM path")
	keyFlag := fs.String("key", os.Getenv("SSHSYNC_KEY_FILE"), "TLS private key PEM path")
	gateKeyFlag := fs.String("gate-key", os.Getenv("SSHSYNC_APP_GATE_KEY"), "64-hex app gate HMAC key")
	_ = fs.Parse(args)

	if *initOnly {
		assets, err := ensureAssets(*baseDir, *sanIP)
		if err != nil {
			log.Fatalf("init assets failed: %v", err)
		}
		fmt.Printf("CERT=%s\nKEY=%s\nSPKI_SHA256=%s\nAPP_GATE_KEY_FILE=%s\n",
			assets.CertPath, assets.KeyPath, assets.SPKIHex, assets.AppKeyPath)
		return
	}

	certPath, keyPath := *certFlag, *keyFlag
	if certPath == "" && keyPath == "" {
		certPath = filepath.Join(*baseDir, "certs", "server.crt")
		keyPath = filepath.Join(*baseDir, "certs", "server.key")
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

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}

	rawLn, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", *listenAddr, err)
	}
	ln := tls.NewListener(newLimitListener(rawLn, maxActiveConns, maxConnsPerIP), tlsCfg)

	store := newMemStore(nil)
	guard := newIPGuard(nil)
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
	go func() {
		<-sigCh
		close(stopCh)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			_ = srv.Close()
		}
		store.Close()
	}()

	log.Printf("sshsync listening on %s (TLS 1.3, SPKI=%s)", *listenAddr, spkiHex)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve error: %v", err)
	}
}
