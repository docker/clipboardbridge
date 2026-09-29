package clipboardbridge

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func peerHostSession(c *net.UnixConn) string {
	raw, err := c.SyscallConn()
	if err != nil {
		return ""
	}
	var cred *unix.Ucred
	var credentialErr error
	if err = raw.Control(func(fd uintptr) {
		cred, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || credentialErr != nil || cred == nil {
		return ""
	}
	file, err := os.Open(fmt.Sprintf("/proc/%d/environ", cred.Pid))
	if err != nil {
		return ""
	}
	defer file.Close()
	const maxEnvironmentBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxEnvironmentBytes+1))
	if err != nil || len(data) > maxEnvironmentBytes {
		return ""
	}
	for entry := range bytes.SplitSeq(data, []byte{0}) {
		if value, ok := bytes.CutPrefix(entry, []byte(hostSessionIDEnv+"=")); ok {
			return string(value)
		}
	}
	return ""
}
