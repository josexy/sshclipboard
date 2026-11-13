package sshtest

import (
	"errors"
	"runtime"
	"syscall"
)

// IsConnectionRefused reports whether a dial failed because the peer refused
// the TCP connection, independent of the OS error message.
func IsConnectionRefused(err error) bool {
	if runtime.GOOS == "windows" {
		const wsaeconnrefused syscall.Errno = 10061 // Winsock WSAECONNREFUSED
		return errors.Is(err, wsaeconnrefused)
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}
