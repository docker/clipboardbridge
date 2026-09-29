package clipboardbridge

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sessionClipboard struct{ sessions chan string }

func (s sessionClipboard) imagePNG(ctx context.Context) ([]byte, error) {
	s.sessions <- hostSession(ctx)
	return nil, nil
}
func (s sessionClipboard) writeText(ctx context.Context, _ string) error {
	s.sessions <- hostSession(ctx)
	return nil
}

func TestClientSession(t *testing.T) {
	if path := os.Getenv("CLIPBOARD_TEST_SOCKET"); path != "" {
		c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
		require.NoError(t, err)
		defer c.Close()
		f := &fakeClient{t: t, uc: c, c: newConn(c)}
		f.handshake()
		f.selectSource("text/plain")
		f.sendSelection("client text")
		f.readUntil(clSource, sourceEvtCancelled)
		return
	}
	t.Setenv(hostSessionIDEnv, "wrong-server-session")
	for _, session := range []string{"first-attach", "second-attach", ""} {
		t.Run("session="+session, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "wayland")
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
			require.NoError(t, err)
			defer listener.Close()
			require.NoError(t, listener.SetDeadline(time.Now().Add(10*time.Second)))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClientSession$")
			child.Env = append(os.Environ(), "CLIPBOARD_TEST_SOCKET="+path, hostSessionIDEnv+"="+session)
			require.NoError(t, child.Start())
			c, err := listener.AcceptUnix()
			require.NoError(t, err)
			host := sessionClipboard{sessions: make(chan string, 2)}
			done := make(chan struct{})
			go func() { defer close(done); newServer(host, nil).serve(ctx, c) }()
			require.NoError(t, child.Wait())
			<-done
			require.Len(t, host.sessions, 2, "image advertisement and text copy must both use the peer session")
			require.Equal(t, session, <-host.sessions)
			require.Equal(t, session, <-host.sessions)
		})
	}
}
