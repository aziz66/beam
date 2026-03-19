# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Beam is a self-hosted, E2E encrypted, ephemeral shared space between devices. No install on the receiving end — everything works in a browser. No accounts, no persistence by default. The server is a blind relay that only sees encrypted blobs.

URL format: `https://server/r/coral-tiger-88#E2EKeyHere` — the fragment (`#key`) is the decryption key and never leaves the browser.

## Build & Run Commands

```bash
# Build
go build -ldflags "-s -w" -o beam .

# Run dev server
./beam --port 8080

# Run tests
go test ./... -v -race

# Docker
docker build -t beam .
docker run -p 8080:8080 beam

# Build Tauri agent
cd agent && cargo tauri build
```

## Architecture

**Go server** (single binary via `go:embed`): WebSocket hub routes messages to rooms, REST API for programmatic access, embedded frontend served from binary.

- `main.go` — Entry point, flag parsing, starts server, embeds `web/` via `go:embed`
- `internal/config/` — Config struct, env var + flag parsing (all `BEAM_*` prefixed)
- `internal/hub/` — WebSocket connection manager, routes messages to rooms
- `internal/room/` — Room lifecycle (create/join/leave/cleanup), TTL, grace periods. BadgerDB store for pinned rooms
- `internal/relay/` — Message forwarding logic: broadcast items, stream file chunks, targeted WebRTC signaling
- `internal/signaling/` — WebRTC SDP/ICE relay (signaling only, no TURN)
- `internal/protocol/` — Wire protocol message types (JSON envelopes over WebSocket)
- `internal/namegen/` — Room code generator (`adjective-noun-NN` format)
- `internal/api/` — REST endpoints under `/api/`
- `internal/preview/` — URL OG-tag fetcher with LRU cache

**Frontend** (`web/`): Vanilla JS ES modules, no framework, no bundler, no transpilation.

- `js/app.js` — State machine: INIT→CREATING→JOINING→CONNECTED→DISCONNECTED→RECONNECTING
- `js/crypto.js` — TweetNaCl wrapper for E2E encryption (key in URL fragment)
- `js/transport.js` — WebSocket client with auto-reconnect (exponential backoff)
- `js/webrtc.js` — P2P upgrade for LAN (2-device rooms only), falls back to WS relay
- `js/stream.js` — File chunking (64KB chunks), encrypt-per-chunk, progress tracking
- `js/clipboard.js` — Paste capture, auto-detect content kind (text/link/code/image)
- Vendored libs in `web/lib/`: TweetNaCl.js, QRCode.js — no CDN deps

**CLI** (`cli/main.go`): Shares `internal/protocol` with server. Commands: `beam send`, `beam receive`, `beam new`.

**Desktop agent** (`agent/`): Tauri (Rust) tray app for clipboard auto-sync.

## Key Design Constraints

- Server never sees plaintext — all content is encrypted client-side before sending
- No external runtime dependencies — single Go binary embeds everything
- WebRTC is best-effort optimization; WebSocket relay is the reliable fallback
- Frontend must work without JS frameworks — vanilla JS only
- All randomness must use `crypto/rand`, not `math/rand`
- Room codes: curated word lists (no offensive/ambiguous words)
- SSRF protection on `/api/preview` — block private IP ranges

## Code Style

- **Go**: Standard `gofmt`/`go vet`. Lowercase error messages, no punctuation.
- **JS**: ES modules, `const` default, no semicolons, single quotes, 2-space indent.
- **CSS**: BEM-like naming, mobile-first, `prefers-color-scheme` for dark/light.
- **Git**: Conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).

## Build Phases

The project spec (`docs/SPEC.md`) defines 11 phases to be followed in order. Each phase should result in a testable system. The implementation order prioritizes core functionality first (WebSocket + rooms + basic UI) before layering on encryption, file streaming, WebRTC, persistence, PWA, CLI, and the desktop agent.
