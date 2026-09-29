# clipboard-bridge

A clipboard bridge for [Docker Sandboxes][docker-sandboxes]: it lets a
coding agent running inside a sandbox paste host screenshots with `Ctrl+V`
and copy text to the host clipboard.

Some agents read clipboard images through a native Wayland library
([`wl-clipboard-rs`][wl-clipboard-rs] / [`arboard`][arboard]) rather than the
terminal. Those reads don't cross the sandbox boundary on their own. This bridge
listens on the sandbox's Wayland socket, speaks just enough of the
[`wlr-data-control`][wlr-data-control] protocol to answer a paste, and fetches the
image from the host over the sandbox proxy's clipboard endpoint. Image reads
remain subject to the host's `clipboard.imagePaste` setting.

Native text copies, including Codex `/copy` and transcript selections, use the
proxy's clipboard-write endpoint. When a client offers both HTML and plain text,
the bridge copies the plain text. Text reads and image writes are not supported.
Copy transfers have a five-second deadline and a 1 MiB limit on both the raw text
and encoded request body. Clipboard contents are never logged.

It's not a compositor and doesn't try to be; there are no surfaces, input, or
rendering.

## Install

Grab a binary from [Releases][releases], or build it:

```bash
make build      # -> ./clipboard-bridge
```

Releases also ship `SHA256SUMS` and a build-provenance attestation you can check
with `gh attestation verify <binary> --repo dvdksn/clipboardbridge`.

## Usage

Run it inside the sandbox; it serves until interrupted:

```bash
clipboard-bridge
```

It needs to see the same `WAYLAND_DISPLAY` / `XDG_RUNTIME_DIR` as the agent that
reads the clipboard. A few optional env vars:

- `CLIPBOARD_BRIDGE_PROXY_URL` — host clipboard endpoint to fetch from
  (default: `http://gateway.docker.internal:3128/_sbx/clipboard`).
- `CLIPBOARD_BRIDGE_WRITE_PROXY_URL` — host text clipboard write endpoint
  (default: `http://gateway.docker.internal:3128/_sbx/clipboard-write`).
- `CLIPBOARD_BRIDGE_DEBUG=1` — verbose logging (otherwise it stays quiet).

## Develop

```bash
make test       # unit tests
make build-all  # static linux/amd64 + linux/arm64 into dist/
```

Releases are cut by pushing a `vX.Y.Z` tag, which builds both architectures and
publishes them via the [release workflow](./.github/workflows/release.yml).

[docker-sandboxes]: https://docs.docker.com/ai/sandboxes/
[wlr-data-control]: https://wayland.app/protocols/wlr-data-control-unstable-v1
[wl-clipboard-rs]: https://github.com/YaLTeR/wl-clipboard-rs
[arboard]: https://github.com/1Password/arboard
[releases]: https://github.com/dvdksn/clipboardbridge/releases
