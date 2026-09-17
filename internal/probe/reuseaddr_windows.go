//go:build windows

package probe

import "syscall"

// disableReuseAddr is a no-op on Windows: Go does not set SO_REUSEADDR there
// and a bind to a port in use fails on its own. Windows support is unverified
// beyond compiling (see docs/ROADMAP.md M3).
func disableReuseAddr(network, address string, c syscall.RawConn) error { return nil }
