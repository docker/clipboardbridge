//go:build unix

package clipboardbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProxyClipboardRequestBody pins the request the bridge sends against each
// environment the sandbox can hand it: the session ID relayed as session_id,
// the older SBX_HOST_* values as host_env, and neither when the bridge was not
// started from an attach.
func TestProxyClipboardRequestBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want map[string]any
	}{
		{
			name: "relays the session id",
			env:  map[string]string{hostSessionIDEnv: "sess-token-123"},
			want: map[string]any{"type": clipboardImageType, "session_id": "sess-token-123"},
		},
		{
			name: "falls back to host_env for a sandbox that names the session itself",
			env: map[string]string{
				"SBX_HOST_DISPLAY":         ":0",
				"SBX_HOST_WAYLAND_DISPLAY": "wayland-0",
			},
			want: map[string]any{
				"type":     clipboardImageType,
				"host_env": map[string]any{"DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0"},
			},
		},
		{
			name: "sends neither when not started from an attach",
			want: map[string]any{"type": clipboardImageType},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(raw, &got))
			}))
			defer srv.Close()
			t.Setenv(proxyURLEnv, srv.URL)
			// Clear both schemes so each case starts from a known environment
			// and an exact match on the body means what it says.
			t.Setenv(hostSessionIDEnv, "")
			for _, envKey := range hostEnvKeys {
				t.Setenv(envKey, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := newProxyClipboard().imagePNG(context.Background())

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestProxyClipboardWrite(t *testing.T) {
	t.Setenv(hostSessionIDEnv, "session-123")
	const text = "hello\nworld ☕\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/_sbx/clipboard-write", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, map[string]any{"text": text, "session_id": "session-123"}, body)
	}))
	defer srv.Close()
	t.Setenv(proxyWriteURLEnv, srv.URL+"/_sbx/clipboard-write")
	require.NoError(t, newProxyClipboard().writeText(context.Background(), text))
}

func TestProxyClipboardWriteErrorsDoNotExposeContents(t *testing.T) {
	const secret = "private clipboard contents"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, secret, http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv(proxyWriteURLEnv, srv.URL)
	err := newProxyClipboard().writeText(context.Background(), secret)
	require.EqualError(t, err, "clipboard copy returned HTTP 500")
}

func TestProxyClipboardWriteLimitsEncodedBody(t *testing.T) {
	// JSON escaping can make a valid raw selection exceed the proxy's limit.
	text := strings.Repeat("\x00", clipboardCopyMaxBytes/2)
	err := newProxyClipboard().writeText(context.Background(), text)
	require.EqualError(t, err, "clipboard copy too large")
}
