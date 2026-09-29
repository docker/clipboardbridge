//go:build unix

package clipboardbridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type copyClipboard struct {
	stubClipboard
	writes chan string
}

func (s copyClipboard) writeText(_ context.Context, text string) error {
	s.writes <- text
	return nil
}

const clSource = 6

func (f *fakeClient) selectSource(mimes ...string) {
	var source eventBody
	source.uint32(clSource)
	f.req(clManager, managerReqCreateDataSource, source.bytes())
	for _, mime := range mimes {
		var offer eventBody
		offer.string(mime)
		f.req(clSource, sourceReqOffer, offer.bytes())
	}
	f.req(clDevice, deviceReqSetSelection, source.bytes())
}

func (f *fakeClient) sendSelection(text string) string {
	send := f.readUntil(clSource, sourceEvtSend)
	mime, ok := newArgReader(send.args).string()
	require.True(f.t, ok)
	fd, ok := f.c.popFD()
	require.True(f.t, ok, "copy send event must carry the destination fd")
	w := os.NewFile(uintptr(fd), "clipboard-copy")
	_, err := w.WriteString(text)
	require.NoError(f.t, err)
	require.NoError(f.t, w.Close())
	return mime
}

func TestCopyFlow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mimes    []string
		text     string
		wantMIME string
	}{
		{"plain", []string{"text/plain"}, "hello\nworld ☕\n", "text/plain"},
		{"codex markdown", []string{"text/html", "text/plain", "UTF8_STRING", "text/plain;charset=utf-8"}, "# Markdown\n\n**text**", "text/plain;charset=utf-8"},
		{"large", []string{"text/plain"}, strings.Repeat("x", 256<<10), "text/plain"},
		{"empty", []string{"text/plain"}, "", "text/plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := copyClipboard{writes: make(chan string, 1)}
			f := newTestServer(t, host)
			f.handshake()
			f.selectSource(tc.mimes...)
			require.Equal(t, tc.wantMIME, f.sendSelection(tc.text))
			f.readUntil(clSource, sourceEvtCancelled)
			require.Equal(t, tc.text, <-host.writes)
			f.req(clSource, sourceReqDestroy, nil)
			f.readUntil(displayID, displayEvtDeleteID)
			// A second copy must be able to reuse the released source ID.
			f.selectSource("text/plain")
			f.sendSelection("next")
			f.readUntil(clSource, sourceEvtCancelled)
			require.Equal(t, "next", <-host.writes)
		})
	}
}

func TestCopyUnsupportedMIMEPreservesClipboard(t *testing.T) {
	host := copyClipboard{writes: make(chan string, 1)}
	f := newTestServer(t, host)
	f.handshake()
	f.selectSource("image/png", "text/html")
	f.readUntil(clSource, sourceEvtCancelled)
	require.Empty(t, host.writes)
}

func TestCopyNullSelectionClearsClipboard(t *testing.T) {
	host := copyClipboard{writes: make(chan string, 1)}
	f := newTestServer(t, host)
	f.handshake()
	var selection eventBody
	selection.uint32(0)
	f.req(clDevice, deviceReqSetSelection, selection.bytes())
	select {
	case text := <-host.writes:
		require.Empty(t, text)
	case <-time.After(time.Second):
		t.Fatal("clipboard clear did not reach host")
	}
}

func TestCopyFailureClosesConnection(t *testing.T) {
	f := newTestServer(t, stubClipboard{err: errors.New("host unavailable")})
	f.handshake()
	f.selectSource("text/plain")
	f.sendSelection("text")
	_, err := f.c.readMessage()
	require.Error(t, err)
}

func TestCopyInvalidUTF8DoesNotWriteHost(t *testing.T) {
	host := copyClipboard{writes: make(chan string, 1)}
	f := newTestServer(t, host)
	f.handshake()
	f.selectSource("text/plain")
	f.sendSelection("\xff")
	_, err := f.c.readMessage()
	require.Error(t, err)
	require.Empty(t, host.writes)
}

func TestCopyOversizedSelectionDoesNotWriteHost(t *testing.T) {
	host := copyClipboard{writes: make(chan string, 1)}
	f := newTestServer(t, host)
	f.handshake()
	f.selectSource("text/plain")
	f.readUntil(clSource, sourceEvtSend)
	fd, ok := f.c.popFD()
	require.True(t, ok)
	w := os.NewFile(uintptr(fd), "clipboard-copy")
	_, _ = io.WriteString(w, strings.Repeat("x", clipboardCopyMaxBytes+1))
	require.NoError(t, w.Close())
	_, err := f.c.readMessage()
	require.Error(t, err)
	require.Empty(t, host.writes)
}

func TestCopyPipeCancellation(t *testing.T) {
	f := newTestServer(t, stubClipboard{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := receiveText(ctx, newConn(f.uc), clSource, "text/plain")
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("copy pipe read survived cancellation")
	}
}

func TestSlowCopyDoesNotOverwriteNewerSelection(t *testing.T) {
	host := copyClipboard{writes: make(chan string, 2)}
	srv := newServer(host, nil)
	first := connectTestClient(t, srv)
	first.handshake()
	first.selectSource("text/plain")
	first.readUntil(clSource, sourceEvtSend)
	fd, ok := first.c.popFD()
	require.True(t, ok)
	delayed := os.NewFile(uintptr(fd), "delayed-copy")
	defer delayed.Close()

	second := connectTestClient(t, srv)
	second.handshake()
	second.selectSource("text/plain")
	second.sendSelection("newer")
	second.readUntil(clSource, sourceEvtCancelled)
	require.Equal(t, "newer", <-host.writes)

	_, err := delayed.WriteString("older")
	require.NoError(t, err)
	require.NoError(t, delayed.Close())
	first.readUntil(clSource, sourceEvtCancelled)
	require.Empty(t, host.writes)
}

type capturedLog chan string

func (c capturedLog) Write(p []byte) (int, error) { c <- string(p); return len(p), nil }

func TestCopyDeniedWarning(t *testing.T) {
	logs := make(capturedLog, 1)
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	f := connectTestClient(t, newServer(stubClipboard{err: errTextCopyDisabled}, logger))
	f.handshake()
	f.selectSource("text/plain")
	f.sendSelection("private clipboard contents")
	require.NoError(t, f.uc.SetReadDeadline(time.Now().Add(3*time.Second)))
	for {
		_, err := f.c.readMessage()
		if err != nil {
			break
		}
	}
	select {
	case message := <-logs:
		require.Contains(t, message, "sbx settings set clipboard.textCopy true")
		require.NotContains(t, message, "private clipboard contents")
	case <-time.After(3 * time.Second):
		t.Fatal("missing consent warning")
	}
}
