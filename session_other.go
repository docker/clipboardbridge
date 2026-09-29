//go:build unix && !linux

package clipboardbridge

import (
	"net"
	"os"
)

func peerHostSession(_ *net.UnixConn) string { return os.Getenv(hostSessionIDEnv) }
