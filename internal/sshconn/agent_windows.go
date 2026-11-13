//go:build windows

package sshconn

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// agentPipe is the named pipe of the Windows OpenSSH Authentication Agent
// service.
const agentPipe = `\\.\pipe\openssh-ssh-agent`

// DialAgent connects to the Windows OpenSSH agent.
func DialAgent() (net.Conn, error) {
	timeout := 3 * time.Second
	return winio.DialPipe(agentPipe, &timeout)
}
