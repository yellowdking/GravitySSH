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
	"syscall"
	"time"
)

func main() {
	initOnly := flag.Bool("init", false, "generate TLS cert, SPKI pin, and app-gate key if missing, then exit")
	baseDir := flag.String("dir", "/opt/sshsync", "directory for certs/ and app-gate.key")
	listenAddr := flag.String("listen", ":54843", "TLS listen address")
	sanIP := flag.String("san-ip", "107.172.98.169", "SAN IP address for generated certificate")
	flag.Parse()

	assets, err := ensureAssets(*baseDir, *sanIP)
	if err != nil {
		log.Fatalf("init assets failed: %v", err)
	}
	if *initOnly {
		fmt.Printf("CERT=%s\nKEY=%s\nSPKI_SHA256=%s\nAPP_GATE_KEY_FILE=%s\n",
			assets.CertPath, assets.KeyPath, assets.SPKIHex, assets.AppKeyPath)
		return
	}

	cert, err := tls.LoadX509KeyPair(assets.CertPath, assets.KeyPath)
	if err != nil {
		log.Fatalf("load tls keypair: %v", err)
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}

	rawLn, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", *listenAddr, err)
	}
	ln := tls.NewListener(newLimitListener(rawLn, maxActiveConns), tlsCfg)

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

	handler := newRelayServer(store, guard, assets.AppGateKey, nil)
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
		_ = srv.Shutdown(ctx)
		store.Close()
	}()

	log.Printf("sshsync listening on %s (TLS 1.3, SPKI=%s)", *listenAddr, assets.SPKIHex)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve error: %v", err)
	}
}
