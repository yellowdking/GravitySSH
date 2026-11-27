//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func startReloadSignal() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	return ch, func() {
		signal.Stop(ch)
	}
}
