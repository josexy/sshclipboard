//go:build !windows

package sshconn

import (
	"errors"
	"net"
	"os"
	"time"
)

// DialAgent connects to the agent at $SSH_AUTH_SOCK, so development
// builds on other systems work.
func DialAgent() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	return net.DialTimeout("unix", sock, 3*time.Second)
}
