# Beam — Project Instructions for Claude Code

> **What is this document?** This is a complete specification and step-by-step build guide for an AI coding agent (Claude Code) to create the Beam project from scratch. Follow these instructions sequentially. Each phase builds on the previous one. Do not skip phases.

---

## Project Overview

**Beam** is an open-source, self-hosted, encrypted, ephemeral shared space between any devices — anywhere, with nothing to install on the receiving end.

It is NOT just a file transfer tool. It is a **universal bridge** — a temporary encrypted room where files, clipboard content, links, code snippets, and images flow between devices in real-time.

### Core Philosophy

- **Zero install on the receiving end** — everything works in a browser tab
- **Zero accounts** — no sign-up, no login
- **Zero persistence by default** — everything is ephemeral
- **Zero trust on the server** — E2E encrypted, the server is a blind relay
- **Single binary deployment** — one command to run

### What Makes This Different

| Existing Tool | Limitation Beam Solves |
|---|---|
| LocalSend | Requires app on both devices, LAN only |
| PairDrop / Snapdrop | LAN only, no clipboard, no persistence |
| Firefox Send clones | Upload-then-download model, not streaming |
| Magic Wormhole | Requires CLI on receiving end |
| WeTransfer | Cloud-hosted, accounts, size limits |
| Apple Universal Clipboard | Apple ecosystem only |

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────┐
│                     CLIENTS                          │
├──────────┬──────────┬──────────┬─────────────────────┤
│  Browser │  Browser │  Tray    │  CLI / API          │
│  (any)   │  (any)   │  Agent   │  (curl, scripts)    │
└────┬─────┴────┬─────┴────┬─────┴────┬────────────────┘
     │          │          │          │
     │    WebSocket + E2E Encryption  │
     │          │          │          │
┌────▼──────────▼──────────▼──────────▼────────────────┐
│                   Go Server                          │
├──────────────────────────────────────────────────────┤
│  WebSocket Hub ──── Room Manager ──── REST API       │
│       │                  │               │           │
│  Message Relay      TTL / Cleanup   File Stream      │
│       │                  │           Router          │
│  WebRTC Signaling   Pinned Room                      │
│  Server              Storage                         │
│                     (BadgerDB)                       │
├──────────────────────────────────────────────────────┤
│  Embedded Frontend (go:embed)                        │
│  Static files served from binary                     │
└──────────────────────────────────────────────────────┘
```

### Encryption Model

When a room is created, an encryption key is generated in the browser. The key is placed in the URL fragment (after `#`), which is **never** sent to the server by browsers. The server is a blind relay — it only sees encrypted blobs.

```
https://your-server.com/r/coral-tiger-88#E2EKeyHere
```

- `coral-tiger-88` — room identifier (sent to server)
- `#E2EKeyHere` — decryption key (never sent to server)

### Streaming Model

```
Device A                    Server                    Device B
   │                          │                          │
   │── WS: encrypted ────────▶│── WS: encrypted ────────▶│
   │      chunk [1/N]         │      chunk [1/N]         │
   │── WS: encrypted ────────▶│── WS: encrypted ────────▶│
   │      chunk [2/N]         │      chunk [2/N]         │
   │                          │                          │
   │   (server holds nothing) │   (reassembles chunks)   │
```

### WebRTC P2P Upgrade (LAN)

```
Device A                Server              Device B
   │                      │                      │
   │── WS: SDP offer ────▶│── WS: SDP offer ────▶│
   │◀── WS: SDP answer ──│◀── WS: SDP answer ──│
   │                      │                      │
   │◀════ Direct P2P (bypasses server) ════════▶│
   │                                             │
   │  Fallback: if P2P fails, stay on WS relay   │
```

---

## Tech Stack

| Layer | Technology | Rationale |
|---|---|---|
| Backend | Go 1.22+ | Single binary, great concurrency, WebSocket native, `go:embed` for frontend |
| Real-time transport | WebSocket (gorilla/websocket) | Reliable, works behind NATs/proxies, simpler than WebRTC for relay |
| P2P transport | WebRTC (Pion) | LAN optimization, server acts only as signaling relay |
| Embedded KV store | BadgerDB | For pinned rooms — embedded, no external deps, survives restarts |
| Frontend | Vanilla JS + CSS | No framework — UI is simple, must load instantly, keep bundle tiny |
| Encryption | TweetNaCl.js (browser), nacl (Go CLI) | E2E encryption, well-audited, small |
| Tray agent | Tauri (Rust + webview) | Tiny footprint, clipboard access, system tray, cross-platform |
| CLI client | Go (same module as server) | Shares crypto and protocol code with server |
| Containerization | Docker (multi-stage build) | Single image, no compose needed |

---

## Project Structure

Create this exact directory structure:

```
beam/
├── go.mod
├── go.sum
├── main.go                    # Entry point: flags, config, starts server
├── Makefile                   # Build targets for all platforms
├── Dockerfile                 # Multi-stage: build Go + embed frontend
├── docker-compose.yml         # Simple single-service compose
├── README.md                  # Project README with badges, screenshots, install
├── LICENSE                    # MIT License
├── .github/
│   └── workflows/
│       └── release.yml        # GoReleaser + Docker build on tag
│
├── internal/
│   ├── config/
│   │   └── config.go          # Configuration struct, env/flag parsing
│   ├── hub/
│   │   └── hub.go             # WebSocket connection hub, routing
│   ├── room/
│   │   ├── room.go            # Room struct, lifecycle, participants
│   │   ├── manager.go         # Room creation, lookup, cleanup goroutine
│   │   └── store.go           # BadgerDB interface for pinned rooms
│   ├── relay/
│   │   └── relay.go           # Message forwarding, chunk streaming
│   ├── signaling/
│   │   └── signaling.go       # WebRTC signaling (SDP offer/answer/ICE)
│   ├── api/
│   │   └── api.go             # REST API handlers
│   ├── preview/
│   │   └── preview.go         # Link OG preview fetching, caching
│   ├── protocol/
│   │   └── messages.go        # Message type definitions, serialization
│   └── namegen/
│       └── namegen.go         # Human-readable room code generator
│
├── web/                       # Frontend (embedded via go:embed)
│   ├── index.html             # Single page shell
│   ├── manifest.json          # PWA manifest
│   ├── sw.js                  # Service worker (PWA, share target)
│   ├── favicon.svg            # Simple SVG favicon
│   ├── css/
│   │   └── style.css          # All styles, dark/light theme via prefers-color-scheme
│   ├── js/
│   │   ├── app.js             # Main entry, room state machine, UI orchestration
│   │   ├── crypto.js          # TweetNaCl wrapper: keygen, encrypt, decrypt
│   │   ├── transport.js       # WebSocket connection, reconnect, message dispatch
│   │   ├── webrtc.js          # P2P connection setup, datachannel, fallback
│   │   ├── stream.js          # File chunking (sender), reassembly (receiver), progress
│   │   ├── clipboard.js       # Paste handler, clipboard read/write
│   │   ├── preview.js         # Client-side link/code/image rendering
│   │   ├── ui.js              # DOM helpers, drag/drop, notifications, QR rendering
│   │   └── device.js          # Device fingerprint/label detection
│   └── lib/
│       ├── tweetnacl.min.js   # Vendored TweetNaCl
│       └── qrcode.min.js      # Vendored QR code generator
│
├── agent/                     # Desktop tray agent (Tauri)
│   ├── src-tauri/
│   │   ├── Cargo.toml
│   │   ├── tauri.conf.json
│   │   └── src/
│   │       ├── main.rs        # Tauri entry, tray setup
│   │       ├── clipboard.rs   # OS clipboard watcher
│   │       ├── tray.rs        # System tray icon, menu
│   │       └── room.rs        # WebSocket connection to Beam server
│   └── src/
│       ├── index.html         # Tray agent settings UI
│       └── settings.js        # Room config, auto-send toggle
│
├── cli/
│   └── main.go                # CLI client: `beam send`, `beam receive`
│
└── scripts/
    ├── generate-wordlist.go   # Generates word list for room codes
    └── dev.sh                 # Dev mode: rebuild + restart on change
```

---

## Phase 1: Project Initialization

### Step 1.1: Initialize Go Module

```bash
mkdir beam && cd beam
go mod init github.com/beam-sh/beam
```

### Step 1.2: Install Go Dependencies

```bash
go get github.com/gorilla/websocket@v1.5.3
go get github.com/dgraph-io/badger/v4
go get github.com/pion/webrtc/v4
```

### Step 1.3: Create Configuration

Create `internal/config/config.go`:

```go
package config

import (
    "flag"
    "os"
    "time"
)

type Config struct {
    Port             int
    Host             string
    MaxRoomSize      int
    DefaultTTL       time.Duration
    MaxFileBuffer    int64       // bytes
    EnablePinnedRooms bool
    DataDir          string
    TLSCert          string
    TLSKey           string
    GracePeriod      time.Duration // time to keep room alive after last disconnect
    MaxRooms         int
}

func Load() *Config {
    c := &Config{}
    flag.IntVar(&c.Port, "port", envInt("BEAM_PORT", 8080), "Server port")
    flag.StringVar(&c.Host, "host", envStr("BEAM_HOST", "0.0.0.0"), "Bind address")
    flag.IntVar(&c.MaxRoomSize, "max-room-size", envInt("BEAM_MAX_ROOM_SIZE", 10), "Max devices per room")
    flag.DurationVar(&c.DefaultTTL, "default-ttl", envDuration("BEAM_DEFAULT_TTL", 30*time.Minute), "Default item TTL")
    flag.Int64Var(&c.MaxFileBuffer, "max-file-buffer", envInt64("BEAM_MAX_FILE_BUFFER", 256*1024*1024), "Max file buffer in bytes")
    flag.BoolVar(&c.EnablePinnedRooms, "enable-pinned-rooms", envBool("BEAM_ENABLE_PINNED_ROOMS", true), "Allow pinned rooms")
    flag.StringVar(&c.DataDir, "data-dir", envStr("BEAM_DATA_DIR", "./data"), "Data directory for pinned rooms")
    flag.StringVar(&c.TLSCert, "tls-cert", envStr("BEAM_TLS_CERT", ""), "TLS certificate path")
    flag.StringVar(&c.TLSKey, "tls-key", envStr("BEAM_TLS_KEY", ""), "TLS key path")
    flag.DurationVar(&c.GracePeriod, "grace-period", envDuration("BEAM_GRACE_PERIOD", 5*time.Minute), "Room grace period after last disconnect")
    flag.IntVar(&c.MaxRooms, "max-rooms", envInt("BEAM_MAX_ROOMS", 1000), "Maximum concurrent rooms")
    flag.Parse()
    return c
}
```

Implement the `envStr`, `envInt`, `envInt64`, `envBool`, `envDuration` helper functions that read from environment variables with fallback to defaults.

### Step 1.4: Create Room Code Generator

Create `internal/namegen/namegen.go`:

- Embed two word lists: ~200 adjectives and ~200 nouns (animals, objects, nature)
- Generate codes in format: `adjective-noun-NN` (e.g., `coral-tiger-88`)
- The number suffix is random 10-99
- Provide a `Generate()` function and a `Validate(code string) bool` function
- Use `crypto/rand` for randomness, not `math/rand`
- Word lists should be curated: no offensive words, no ambiguous words, easy to spell and speak aloud

---

## Phase 2: WebSocket Hub & Room Management

### Step 2.1: Protocol Messages

Create `internal/protocol/messages.go`:

Define all message types as Go structs with JSON tags. These are the wire protocol messages between clients and server:

```go
// MessageType enum
const (
    TypeJoin         = "join"          // Client → Server: join a room
    TypeJoined       = "joined"        // Server → Client: successfully joined
    TypeDeviceList   = "device_list"   // Server → All: updated device list
    TypeItem         = "item"          // Client → Server → All: a shared item
    TypeFileMeta     = "file_meta"     // Client → Server → All: file transfer starting
    TypeFileChunk    = "file_chunk"    // Client → Server → Target: encrypted file chunk
    TypeFileComplete = "file_complete" // Client → Server → All: file transfer done
    TypeFileCancel   = "file_cancel"   // Either direction: cancel transfer
    TypeSignalOffer  = "signal_offer"  // Client → Server → Target: WebRTC SDP offer
    TypeSignalAnswer = "signal_answer" // Client → Server → Target: WebRTC SDP answer
    TypeSignalICE    = "signal_ice"    // Client → Server → Target: ICE candidate
    TypePing         = "ping"          // Keepalive
    TypePong         = "pong"          // Keepalive response
    TypeError        = "error"         // Server → Client: error message
    TypeRoomInfo     = "room_info"     // Server → Client: room metadata
    TypeItemExpired  = "item_expired"  // Server → All: item TTL expired
)

// Envelope is the top-level message wrapper
type Envelope struct {
    Type      string          `json:"type"`
    Payload   json.RawMessage `json:"payload"`
    ID        string          `json:"id,omitempty"`        // message ID for ack
    Timestamp int64           `json:"ts"`                  // unix ms
    DeviceID  string          `json:"device_id,omitempty"` // sender device
    TargetID  string          `json:"target_id,omitempty"` // for P2P signaling
}

// ItemPayload represents a shared item (text, link, code, image)
type ItemPayload struct {
    ItemID           string `json:"item_id"`
    Kind             string `json:"kind"`              // "text", "link", "code", "image"
    EncryptedData    string `json:"encrypted_data"`    // base64 encrypted content
    Nonce            string `json:"nonce"`             // base64 encryption nonce
    TTL              int    `json:"ttl"`               // seconds
    DeviceLabel      string `json:"device_label"`      // "Chrome on MacBook"
}

// FileMetaPayload describes an incoming file transfer
type FileMetaPayload struct {
    FileID        string `json:"file_id"`
    EncryptedName string `json:"encrypted_name"` // encrypted filename
    Nonce         string `json:"nonce"`
    Size          int64  `json:"size"`           // total bytes
    ChunkSize     int    `json:"chunk_size"`     // bytes per chunk
    TotalChunks   int    `json:"total_chunks"`
    DeviceLabel   string `json:"device_label"`
}

// FileChunkPayload is a single chunk of encrypted file data
type FileChunkPayload struct {
    FileID        string `json:"file_id"`
    Index         int    `json:"index"`
    EncryptedData string `json:"encrypted_data"` // base64 encrypted chunk
    Nonce         string `json:"nonce"`
}
```

### Step 2.2: WebSocket Hub

Create `internal/hub/hub.go`:

The hub manages all WebSocket connections and routes messages to the correct rooms.

Requirements:
- Accept WebSocket upgrade at `/ws/{roomCode}`
- Each connection gets a unique `deviceID` (UUID v4)
- On connect: look up or create room, add client, broadcast updated device list
- On message: parse envelope, route to room's relay
- On disconnect: remove client from room, broadcast updated device list, start grace period timer if room is empty
- Implement ping/pong keepalive every 30 seconds
- Set read limit to 10MB per message (file chunks should be smaller)
- Use gorilla/websocket with proper close handling
- Thread-safe: use channels or mutexes for concurrent access

### Step 2.3: Room Manager

Create `internal/room/manager.go` and `internal/room/room.go`:

**Room struct:**
```go
type Room struct {
    Code        string
    CreatedAt   time.Time
    Clients     map[string]*Client  // deviceID → Client
    Items       []*StoredItem       // recent items in memory (for late joiners)
    Pinned      bool
    Passphrase  string              // bcrypt hash, empty if not pinned
    MaxSize     int
    DefaultTTL  time.Duration
    mu          sync.RWMutex
    graceTimer  *time.Timer
}

type Client struct {
    DeviceID    string
    DeviceLabel string
    Conn        *websocket.Conn
    Send        chan []byte
    JoinedAt    time.Time
}

type StoredItem struct {
    Envelope    []byte    // raw JSON, still encrypted
    ExpiresAt   time.Time
}
```

**Room Manager responsibilities:**
- `CreateRoom(pinned bool, passphrase string) (*Room, error)` — generate code, create room, start cleanup goroutine
- `GetRoom(code string) *Room` — thread-safe lookup
- `DeleteRoom(code string)` — cleanup and remove
- Background goroutine: every 60 seconds, sweep expired items from all rooms, delete empty rooms past grace period
- Enforce `MaxRooms` limit — return error if exceeded
- For pinned rooms: persist to BadgerDB on creation, restore on server start

### Step 2.4: BadgerDB Store for Pinned Rooms

Create `internal/room/store.go`:

- Open BadgerDB at `config.DataDir/rooms.db`
- Store pinned room metadata: code, passphrase hash, created time, TTL settings
- On server startup: load all pinned rooms from DB and register them in the manager
- On pinned room creation: write to DB
- On pinned room deletion: remove from DB
- Implement `Close()` for graceful shutdown
- Run BadgerDB GC periodically (every 5 minutes)

---

## Phase 3: Message Relay & File Streaming

### Step 3.1: Message Relay

Create `internal/relay/relay.go`:

The relay receives messages from the hub and distributes them to room participants.

Behavior per message type:
- **`item`**: Broadcast to all clients in room except sender. Store in room's `Items` slice for late joiners (up to 50 items, FIFO eviction). Start TTL timer for the item.
- **`file_meta`**: Broadcast to all clients except sender. Do NOT store (files are streamed, not buffered).
- **`file_chunk`**: Forward to all clients except sender. Do NOT store. If no other clients are connected, hold in a bounded in-memory buffer (configurable size, default 256MB across all rooms) with TTL.
- **`file_complete`**: Broadcast to all except sender. Clean up any buffered chunks for that fileID.
- **`file_cancel`**: Broadcast to all. Clean up buffered chunks.
- **`signal_offer`, `signal_answer`, `signal_ice`**: Forward only to the `target_id` device. These are for WebRTC signaling and should not be broadcast.
- **`ping`**: Respond with `pong` directly to sender.

### Step 3.2: File Chunk Buffer

For the case where a file transfer starts but the receiver hasn't connected yet (or reconnects mid-transfer):

- In-memory ring buffer per room, bounded by `MaxFileBuffer`
- Each buffered chunk has a TTL (same as `GracePeriod`)
- When a new client joins a room, send them any buffered file_meta + chunks for active transfers
- When buffer exceeds limit, drop oldest chunks first
- This is best-effort — if the buffer fills up, the transfer must be restarted

---

## Phase 4: Frontend — Core UI

### Step 4.1: HTML Shell

Create `web/index.html`:

Single page application. No frameworks. The HTML should contain:
- A root container div
- Three views (show/hide based on state):
  1. **Landing view**: Shows room code, QR code, share link, "Join a room" input
  2. **Room view**: The main shared space with item feed and input area
  3. **Settings modal**: TTL config, device label, pinned room options
- Load all JS as ES modules
- Include viewport meta tag for mobile
- Include PWA manifest link
- Include share target meta tags

Design requirements:
- Dark/light theme via `prefers-color-scheme` CSS media query
- CSS custom properties for all colors (easy theming)
- System font stack: `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`
- Mobile-first responsive layout
- No external fonts or CDN dependencies — everything is self-contained

### Step 4.2: CSS

Create `web/css/style.css`:

Design language:
- Clean, minimal, slightly rounded corners (8px)
- Subtle shadows for cards
- Smooth transitions (150ms ease)
- Color palette:
  - Light mode: white background, slate-gray text, blue accents
  - Dark mode: near-black background (#1a1a2e), light gray text, blue accents
- The item feed should look like cards floating on a surface, not a chat log
- Drop zone highlight: dashed blue border with subtle pulse animation
- Progress bars: thin, blue, with percentage label
- QR code: rendered in a card with rounded corners

```css
:root {
    --bg-primary: #ffffff;
    --bg-secondary: #f5f5f7;
    --bg-card: #ffffff;
    --text-primary: #1d1d1f;
    --text-secondary: #6e6e73;
    --accent: #0071e3;
    --accent-hover: #0077ED;
    --border: #d2d2d7;
    --border-light: #e8e8ed;
    --success: #34c759;
    --warning: #ff9f0a;
    --danger: #ff3b30;
    --radius: 8px;
    --radius-lg: 12px;
    --shadow: 0 1px 3px rgba(0,0,0,0.08);
    --shadow-lg: 0 4px 12px rgba(0,0,0,0.12);
    --font: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    --font-mono: "SF Mono", "Fira Code", "Cascadia Code", monospace;
    --transition: 150ms ease;
}

@media (prefers-color-scheme: dark) {
    :root {
        --bg-primary: #1a1a2e;
        --bg-secondary: #16213e;
        --bg-card: #1e1e3a;
        --text-primary: #e4e4e7;
        --text-secondary: #a1a1aa;
        --border: #2e2e4a;
        --border-light: #252545;
        --shadow: 0 1px 3px rgba(0,0,0,0.3);
        --shadow-lg: 0 4px 12px rgba(0,0,0,0.4);
    }
}
```

### Step 4.3: Room UI Layout

The room view layout:

```
┌─────────────────────────────────────────────────┐
│  Header Bar                                     │
│  ┌───────────────────────────────────────────┐  │
│  │ 🔗 coral-tiger-88  [QR] [Copy] [Settings]│  │
│  │ 2 devices connected  ●●                  │  │
│  └───────────────────────────────────────────┘  │
├─────────────────────────────────────────────────┤
│  Item Feed (scrollable, newest at bottom)       │
│                                                 │
│  ┌──────────────────────────────┐               │
│  │ 📋 Text Snippet             │ Chrome · 5m   │
│  │ "const api = await..."      │ [Copy]        │
│  └──────────────────────────────┘               │
│                                                 │
│  ┌──────────────────────────────┐               │
│  │ 🔗 https://example.com      │ Firefox · 2m  │
│  │ Example Domain — A short... │ [Open] [Copy] │
│  └──────────────────────────────┘               │
│                                                 │
│  ┌──────────────────────────────┐               │
│  │ 📄 report.pdf  4.2 MB       │ Chrome · 30s  │
│  │ ████████████████░░░ 78%     │ [Download]    │
│  └──────────────────────────────┘               │
│                                                 │
│  ┌──────────────────────────────┐               │
│  │ 🖼️ screenshot.png           │ Safari · 10s  │
│  │ [thumbnail preview]         │ [Download]    │
│  └──────────────────────────────┘               │
│                                                 │
├─────────────────────────────────────────────────┤
│  Input Area                                     │
│  ┌───────────────────────────────────────────┐  │
│  │  Drop files or paste anything here...     │  │
│  │                               [📎] [Send] │  │
│  └───────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

Implementation details:
- The ENTIRE page is a drag-and-drop zone, not just the input area. When dragging, show a full-page overlay with a dashed border and "Drop to share" text.
- The input area is a contenteditable div (not textarea) to support rich paste (images from clipboard)
- Ctrl+V / Cmd+V anywhere on the page (not just in the input) should capture clipboard content
- File input button (📎) opens native file picker as fallback
- Items auto-scroll to newest, but pause auto-scroll if user has scrolled up
- Each item card has a subtle entrance animation (fade + slide up, 200ms)
- Items show a countdown badge when TTL < 5 minutes, then fade out when expired
- Mobile: input area sticks to bottom, item feed takes remaining space

### Step 4.4: JavaScript Modules

**`web/js/app.js`** — Main orchestrator:
- State machine: `INIT → CREATING → JOINING → CONNECTED → DISCONNECTED → RECONNECTING`
- On page load:
  1. Check URL for room code (path `/r/{code}`)
  2. Check URL fragment for encryption key (`#key`)
  3. If both present: join room
  4. If neither: show landing view, generate new room
- Manage item list in memory
- Dispatch events between modules
- Handle visibility API (pause/resume when tab is hidden/shown)

**`web/js/crypto.js`** — Encryption wrapper:
- Use vendored TweetNaCl.js
- `generateKey()` → returns base64-encoded secret key
- `encrypt(plaintext, key)` → returns `{encrypted: base64, nonce: base64}`
- `decrypt(encrypted, nonce, key)` → returns plaintext
- For files: encrypt each chunk independently with a unique nonce
- Key derivation for pinned rooms: `scrypt(passphrase, roomCode)` → key

**`web/js/transport.js`** — WebSocket management:
- Connect to `ws(s)://host/ws/{roomCode}`
- Auto-reconnect with exponential backoff (1s, 2s, 4s, 8s, max 30s)
- Message queue: buffer outgoing messages while disconnected, flush on reconnect
- Keepalive: send ping every 25 seconds, expect pong within 10 seconds
- Parse incoming envelopes, dispatch to handlers registered by other modules
- Track connection state, emit events: `connected`, `disconnected`, `reconnecting`

**`web/js/webrtc.js`** — P2P upgrade:
- After WebSocket connection is established and 2+ devices are in room:
  1. Initiator creates RTCPeerConnection with STUN servers (Google's public STUN)
  2. Creates data channel named "beam"
  3. Sends SDP offer via WebSocket signaling
  4. Receives SDP answer, sets remote description
  5. Exchanges ICE candidates via WebSocket
  6. Once data channel opens: mark P2P as active
- When P2P is active: send items/chunks via data channel instead of WebSocket
- Fallback: if P2P connection fails or data channel closes, seamlessly fall back to WebSocket relay
- UI indicator: show "P2P" badge when direct connection is active
- Do NOT attempt P2P for rooms with 3+ devices (complexity vs. benefit)

**`web/js/stream.js`** — File chunking:
- Chunk size: 64KB (good balance of overhead vs. progress granularity)
- Sender side:
  1. Read file as ArrayBuffer
  2. Split into chunks
  3. Encrypt each chunk with unique nonce
  4. Send `file_meta` message
  5. Send chunks sequentially (wait for previous chunk to be sent before queuing next)
  6. Send `file_complete`
  7. Track progress: bytes sent / total bytes
- Receiver side:
  1. Receive `file_meta`, create placeholder in UI with progress bar
  2. Receive chunks, decrypt, store in ordered array
  3. On `file_complete`: combine chunks into Blob, create object URL
  4. Show download button
  5. Track progress: chunks received / total chunks
- Support multiple simultaneous transfers (track by `fileID`)
- Cancel support: user can cancel from either end

**`web/js/clipboard.js`** — Clipboard handling:
- Listen for `paste` event on `document` (captures all pastes, not just in input)
- Detect paste content type:
  - `text/plain` → send as text item
  - `text/html` → extract plain text, send as text item
  - `image/*` → read as blob, send as image item (inline preview)
  - Files → redirect to file streaming
- Auto-detect content kind from text:
  - URL pattern → kind: "link"
  - Code heuristics (brackets, semicolons, indentation) → kind: "code"
  - Otherwise → kind: "text"
- Copy to clipboard: `navigator.clipboard.writeText()` with fallback to `execCommand`

**`web/js/preview.js`** — Content rendering:
- Text: render with line breaks preserved, truncate at 500 chars with "show more"
- Links: fetch OG preview from server endpoint (`/api/preview?url=...`), render card with title, description, image
- Code: detect language (simple heuristic: file extension hints in content, keyword analysis), apply syntax highlighting using a minimal inline highlighter (no external library — use a simple token-based approach for common languages: JS, Python, Go, HTML, CSS, SQL, JSON, YAML, Bash)
- Images: render inline thumbnail (max 400px wide), click to view full size
- Files: show icon based on extension, file name, size in human-readable format

**`web/js/ui.js`** — DOM utilities:
- `renderItem(item)` → creates card element, appends to feed
- `showDropOverlay()` / `hideDropOverlay()` → full-page drag overlay
- `renderQR(data)` → uses vendored qrcode.js to render QR canvas
- `showNotification(text)` → toast notification (auto-dismiss 3s)
- `formatBytes(n)` → human-readable file size
- `formatTimeAgo(ts)` → "3m ago", "just now"
- `copyToClipboard(text)` → copy with visual feedback

**`web/js/device.js`** — Device identification:
- Detect browser + OS from user agent: "Chrome on MacBook", "Safari on iPhone", "Firefox on Windows"
- Generate a stable device ID per browser (store in `sessionStorage`, NOT `localStorage`)
- Provide a human-readable label for display

### Step 4.5: Vendored Libraries

Place these in `web/lib/`:

- **TweetNaCl.js** — Download from https://tweetnacl.js.org/
  - `tweetnacl.min.js`
  - `tweetnacl-util.min.js` (for base64/UTF8 helpers)
- **QRCode.js** — A minimal QR code generator (e.g., `qrcode-generator` or similar, minified)

Do NOT use any CDN links. Everything must be self-contained in the binary.

---

## Phase 5: REST API

### Step 5.1: API Endpoints

Create `internal/api/api.go`:

All endpoints are under `/api/`:

```
POST   /api/rooms                    → Create a new room (returns code + link)
GET    /api/rooms/{code}             → Room info (device count, created time, pinned status)
DELETE /api/rooms/{code}             → Close a room (requires passphrase for pinned rooms)
POST   /api/rooms/{code}/items       → Send an item to a room (for CLI/API usage)
GET    /api/rooms/{code}/items       → Get recent items (encrypted, for late joiners)
GET    /api/preview                  → Fetch OG preview for a URL (?url=...)
GET    /api/health                   → Health check (returns {"status": "ok"})
```

**POST `/api/rooms/{code}/items`** example:
```bash
# Send text to a room
curl -X POST https://beam.local/api/rooms/coral-tiger-88/items \
  -H "Content-Type: application/json" \
  -d '{"kind": "text", "encrypted_data": "base64...", "nonce": "base64..."}'
```

**GET `/api/preview`** — Link preview:
- Accept `?url=` parameter
- Fetch the URL server-side (with timeout: 5s, max body: 1MB)
- Extract Open Graph tags: `og:title`, `og:description`, `og:image`
- Fallback to `<title>` tag and meta description
- Cache results in memory (LRU cache, max 1000 entries, TTL 1 hour)
- Return JSON: `{"title": "...", "description": "...", "image": "...", "url": "..."}`
- Rate limit: 10 requests per minute per room

### Step 5.2: Static File Serving & Embedding

In `main.go`:

```go
//go:embed web/*
var webFS embed.FS

// Serve frontend
// Route /r/{code} to index.html (SPA client-side routing)
// Route /api/* to API handlers
// Route /ws/{code} to WebSocket upgrade
// Route everything else to embedded static files
```

---

## Phase 6: WebRTC Signaling

### Step 6.1: Signaling Server

Create `internal/signaling/signaling.go`:

This is NOT a TURN server. The Go server only relays WebRTC signaling messages (SDP offers/answers, ICE candidates) between clients via the existing WebSocket connection.

Behavior:
- When a client sends a `signal_offer` message with a `target_id`, forward it to that specific client only
- Same for `signal_answer` and `signal_ice`
- No state management needed — just message forwarding
- The actual P2P connection happens directly between browsers

STUN servers for ICE candidate gathering (configure in frontend):
```javascript
const rtcConfig = {
    iceServers: [
        { urls: "stun:stun.l.google.com:19302" },
        { urls: "stun:stun1.l.google.com:19302" }
    ]
};
```

No TURN server in v1. If P2P fails (symmetric NAT, corporate firewall), fall back to WebSocket relay gracefully.

---

## Phase 7: Desktop Tray Agent (Tauri)

### Step 7.1: Tauri Project Setup

```bash
cd agent
cargo install create-tauri-app
# Initialize Tauri project with vanilla JS frontend
```

### Step 7.2: Tauri Backend (Rust)

**`src-tauri/src/clipboard.rs`**:
- Use `arboard` crate for cross-platform clipboard access
- Poll clipboard every 500ms (compare hash of content to detect changes)
- When change detected: emit event to frontend webview
- Support text and image clipboard content
- On incoming item from room: write to system clipboard

**`src-tauri/src/tray.rs`**:
- System tray icon with menu:
  - Room status (connected/disconnected)
  - "Copy room link"
  - "Open in browser"
  - "Auto-sync clipboard" toggle
  - "Settings"
  - "Quit"
- Drag-and-drop onto tray icon: send file to room
- Tray icon changes color based on connection status (green=connected, gray=disconnected)

**`src-tauri/src/room.rs`**:
- WebSocket client connection to Beam server
- Same protocol as browser client
- Handle encryption/decryption using `sodiumoxide` or `crypto_box` crate
- Auto-reconnect on disconnect
- Store room code and passphrase in OS keyring (via `keyring` crate)

### Step 7.3: Tauri Frontend

Minimal settings UI:
- Server URL input
- Room code input (or scan QR)
- Auto-sync clipboard toggle
- Notification preferences
- Connected devices list
- Recent items list (last 10)

---

## Phase 8: CLI Client

### Step 8.1: CLI Structure

Create `cli/main.go`:

```bash
# Send a file
beam send ./report.pdf --room coral-tiger-88 --server https://beam.local

# Send text from stdin
echo "hello world" | beam send --room coral-tiger-88

# Send clipboard
beam send --clipboard --room coral-tiger-88

# Receive (watch a room, download files to current directory)
beam receive --room coral-tiger-88 --server https://beam.local

# Create a new room
beam new --server https://beam.local --pinned --passphrase "mysecret"
```

Implementation:
- Share the `internal/protocol` package with the server
- Use `gorilla/websocket` client
- Use Go's `nacl/box` or `golang.org/x/crypto/nacl` for encryption
- File sending: chunk file, encrypt, send via WebSocket (same protocol as browser)
- File receiving: reassemble chunks, decrypt, save to disk
- Text sending: read from args or stdin
- Progress bar in terminal using simple ASCII art (`████░░░░ 45%`)
- Graceful interrupt handling (Ctrl+C cancels transfer cleanly)

---

## Phase 9: PWA & Mobile Experience

### Step 9.1: Service Worker

Create `web/sw.js`:

- Cache the app shell (HTML, CSS, JS, vendored libs) for offline access
- The app won't work offline (needs WebSocket), but it should load instantly from cache
- Handle share target: when sharing from Android apps, receive the shared data and route it to the active room

### Step 9.2: PWA Manifest

Create `web/manifest.json`:

```json
{
    "name": "Beam",
    "short_name": "Beam",
    "description": "Encrypted ephemeral sharing between any devices",
    "start_url": "/",
    "display": "standalone",
    "background_color": "#1a1a2e",
    "theme_color": "#0071e3",
    "icons": [
        { "src": "/favicon.svg", "sizes": "any", "type": "image/svg+xml" }
    ],
    "share_target": {
        "action": "/share",
        "method": "POST",
        "enctype": "multipart/form-data",
        "params": {
            "title": "title",
            "text": "text",
            "url": "url",
            "files": [
                {
                    "name": "file",
                    "accept": ["*/*"]
                }
            ]
        }
    }
}
```

### Step 9.3: Mobile-Specific UI

- Touch-friendly tap targets (minimum 44px)
- Swipe to dismiss items
- Camera capture button (use `<input type="file" accept="image/*" capture="environment">`)
- Share sheet integration via Web Share Target API
- Bottom sheet for settings (not modal)
- Haptic feedback on copy (if supported: `navigator.vibrate(50)`)

---

## Phase 10: Build & Distribution

### Step 10.1: Makefile

```makefile
.PHONY: build dev clean docker release

VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"

# Development
dev:
	@scripts/dev.sh

# Build for current platform
build:
	go build $(LDFLAGS) -o beam .

# Build for all platforms
release:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/beam -linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/beam -linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/beam -darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/beam -darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/beam -windows-amd64.exe .

# Docker
docker:
	docker build -t beam:$(VERSION) .

# Clean
clean:
	rm -rf dist/ data/

# Run tests
test:
	go test ./... -v -race

# Agent (Tauri)
agent:
	cd agent && cargo tauri build
```

### Step 10.2: Dockerfile

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o beam .

# Runtime stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/beam .
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["./beam"]
```

### Step 10.3: docker-compose.yml

```yaml
version: "3.8"
services:
  beam:
    image: ghcr.io/beam-sh/beam:latest
    ports:
      - "8080:8080"
    volumes:
      - beam-data:/app/data
    environment:
      - BEAM_PORT=8080
      - BEAM_ENABLE_PINNED_ROOMS=true
    restart: unless-stopped

volumes:
  beam-data:
```

### Step 10.4: GitHub Actions Release

Create `.github/workflows/release.yml`:

On tag push (`v*`):
1. Run tests
2. Build binaries for all platforms
3. Build and push Docker image to GHCR
4. Create GitHub release with binaries attached
5. Generate changelog from commits

---

## Phase 11: README & Documentation

### Step 11.1: README.md

The README should include:

1. **Hero section**: Project name, one-line description, badges (Go version, license, Docker pulls, GitHub stars)
2. **Demo GIF**: Animated GIF showing the core flow (create room → QR → drop file → appears on other device)
3. **Why Beam**: 3-4 bullet points on what makes it different
4. **Quick Start**:
   - Single binary: `curl -L ... | tar xz && ./beam`
   - Docker: `docker run -p 8080:8080 ghcr.io/beam-sh/beam`
   - From source: `go install github.com/beam-sh/beam@latest`
5. **Features**: Brief list with icons/emoji
6. **Configuration**: Table of all flags and env vars
7. **API**: Brief overview with curl examples
8. **CLI**: Usage examples
9. **Desktop Agent**: Install instructions for each OS
10. **Security**: Explain E2E encryption model clearly
11. **Self-Hosting Guide**: Reverse proxy (nginx/Caddy), TLS, systemd service
12. **Contributing**: Guidelines, development setup
13. **License**: MIT

---

## Implementation Order

Follow this exact order. Each step should result in a working (if incomplete) system that can be tested:

1. **Phase 1** — Project init, go.mod, config, namegen
2. **Phase 2.1-2.2** — Protocol messages + WebSocket hub (test: connect two browser tabs, see connection logs)
3. **Phase 2.3** — Room manager (test: create rooms, join with code, see device lists)
4. **Phase 4.1-4.3** — Frontend HTML + CSS + basic room UI (test: visual layout, responsive design)
5. **Phase 4.4** — transport.js + clipboard.js + ui.js (test: connect to server, paste text, see it appear on both tabs)
6. **Phase 4.4** — crypto.js (test: items are encrypted/decrypted, server logs show only encrypted data)
7. **Phase 3** — Message relay + file streaming (test: drag file, see progress, download on other tab)
8. **Phase 4.4** — stream.js (test: large file transfer with progress bars)
9. **Phase 4.4** — preview.js + device.js (test: link previews, code highlighting, device labels)
10. **Phase 5** — REST API (test: curl commands work)
11. **Phase 6** — WebRTC signaling + webrtc.js (test: two tabs on same LAN use P2P)
12. **Phase 2.4** — BadgerDB for pinned rooms (test: create pinned room, restart server, room persists)
13. **Phase 9** — PWA + service worker (test: install on Android, share from other apps)
14. **Phase 8** — CLI client (test: send file from terminal)
15. **Phase 7** — Tauri agent (test: clipboard auto-sync)
16. **Phase 10** — Build system, Docker, CI/CD
17. **Phase 11** — README and documentation

---

## Testing Strategy

### Unit Tests

- `internal/namegen`: Test uniqueness, format validation, no offensive words
- `internal/protocol`: Test serialization/deserialization of all message types
- `internal/room`: Test room lifecycle, TTL expiration, client join/leave, max room size
- `internal/relay`: Test message routing, broadcast vs. targeted delivery
- `internal/preview`: Test OG tag extraction, cache behavior

### Integration Tests

- WebSocket connection lifecycle (connect, join, disconnect, reconnect)
- Two-client text sharing (send text from A, verify receipt on B)
- File transfer (send file from A, verify correct reassembly on B)
- Encryption round-trip (encrypt on client A, verify server cannot read, decrypt on client B)
- Pinned room persistence (create, restart server, verify room exists)
- Room cleanup (all clients disconnect, wait for grace period, verify room deleted)
- API endpoints (create room, send item, get items)

### Browser Tests

- Drag and drop file (verify upload starts)
- Paste text (verify item appears)
- Paste image (verify thumbnail renders)
- QR code scan (verify room join)
- Mobile responsive layout
- Dark/light theme switching

---

## Security Considerations

- **Never log decrypted content** — server logs should only contain room codes, device IDs, message types, and sizes
- **Rate limiting** — max 100 WebSocket messages per second per client, max 10 room creations per minute per IP
- **Input validation** — validate all message fields, reject oversized messages
- **CORS** — allow same-origin only (frontend is served from the same binary)
- **CSP headers** — strict Content-Security-Policy: no inline scripts, no external resources
- **URL preview** — server-side fetch must have SSRF protection: block private IPs (10.x, 192.168.x, 127.x, etc.)
- **WebSocket origin check** — verify Origin header matches server's host
- **No directory traversal** — embedded filesystem prevents this, but verify
- **Passphrase hashing** — use bcrypt with cost 12 for pinned room passphrases
- **Memory limits** — bound all in-memory buffers to prevent OOM

---

## Non-Goals (Explicitly Out of Scope)

- User accounts / authentication (beyond room passphrases)
- Persistent storage of transferred files
- Chat / messaging features
- Video / audio streaming
- Plugin system
- Multi-server federation
- Mobile native apps (PWA covers mobile)
- Admin dashboard
- Analytics / telemetry

---

## Code Style & Conventions

- **Go**: Follow standard Go conventions. `gofmt`, `golint`, `go vet`. Error messages lowercase, no punctuation.
- **JavaScript**: ES modules, no transpilation, no bundler. Use `const` by default. No semicolons. Single quotes for strings. 2-space indent.
- **CSS**: BEM-like naming for classes. No CSS preprocessors. Mobile-first media queries.
- **Git**: Conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
- **Comments**: Explain WHY, not WHAT. No obvious comments.

---

## Final Notes for Claude Code

- Build incrementally. After each phase, run and test before moving on.
- Prefer simplicity over cleverness. This is an open-source project that others will read and contribute to.
- If a decision isn't covered here, choose the simpler option.
- The frontend MUST work without JavaScript frameworks. Vanilla JS only.
- The Go server MUST compile to a single binary with zero runtime dependencies.
- Every feature must degrade gracefully: if WebRTC fails, use WebSocket. If preview fails, show raw URL. If clipboard API isn't available, show paste button.
- Keep the binary size small. Strip debug symbols in release builds.
- This project should feel delightful to use. Smooth animations, instant feedback, zero confusion.
