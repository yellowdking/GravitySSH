//go:build windows

package main

import "os"

func startReloadSignal() (<-chan os.Signal, func()) {
	return nil, func() {}
}