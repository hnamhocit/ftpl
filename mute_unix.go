//go:build linux || darwin

package main

import (
	"os"
	"syscall"
)

// muteStdout redirects fd 1 to /dev/null.
// fd-level redirection is the ONLY reliable mute: swag's gen package binds
// its own logger to os.Stdout at package-init, so swapping the os.Stdout
// variable or calling log.SetOutput cannot reach it — but every write to fd 1,
// through any captured *os.File, lands in /dev/null while muted.
//
// Child processes (the dev server) are unaffected: they have their own fd table.
func muteStdout() (restore func(), err error) {
	noop := func() {}
	orig := os.Stdout

	saved, err := syscall.Dup(1)
	if err != nil {
		return noop, err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0o666)
	if err != nil {
		syscall.Close(saved)
		return noop, err
	}
	if err := syscall.Dup2(int(devNull.Fd()), 1); err != nil {
		syscall.Close(saved)
		devNull.Close()
		return noop, err
	}
	os.Stdout = devNull

	return func() {
		_ = syscall.Dup2(saved, 1)
		syscall.Close(saved)
		devNull.Close()
		os.Stdout = orig
	}, nil
}
