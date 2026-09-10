//go:build windows

package main

// muteStdout on Windows is a best-effort no-op: silencing fd 1 there needs
// golang.org/x/sys SetStdHandle, not worth a dependency for dev-time cosmetics.
// Windows devs keep the swag progress lines; everything else behaves the same.
func muteStdout() (restore func(), err error) {
	return func() {}, nil
}
