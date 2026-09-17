//go:build !windows

package probe

import "syscall"

// disableReuseAddr clears SO_REUSEADDR so that, on BSD-derived systems, a bind
// to a specific loopback address fails when a wildcard listener holds the port.
func disableReuseAddr(network, address string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
	})
	if err != nil {
		return err
	}
	return serr
}
