# Beam

**Encrypted ephemeral sharing between any devices.**

No install on the receiving end. No accounts. No persistence by default. The server is a blind relay that only sees encrypted blobs.

## Why Beam?

- **Zero install** — the receiver just opens a browser tab
- **Zero trust** — E2E encrypted, the server never sees your data
- **Zero accounts** — no sign-up, no login, no tracking
- **Single binary** — one command to self-host

## Quick Start

### Docker

```bash
docker run -p 8080:8080 ghcr.io/aziz66/beam
```

### Binary

```bash
# Download from releases, then:
./beam --port 8080
```

### From Source

```bash
git clone https://github.com/aziz66/beam.git
cd beam
go build -ldflags "-s -w" -o beam .
./beam
```

Open `http://localhost:8080` — you'll get a room with a QR code and link. Open that link on any other device to start sharing.

## How It Works

1. Visit the server — a room is created with a unique code like `coral-tiger-88`
2. An encryption key is generated in your browser and placed in the URL fragment (`#key`)
3. Share the link (QR code, copy, etc.) — the `#key` part **never leaves the browser**
4. Drop files, paste text, share links — everything is encrypted before leaving your device
5. The server relays encrypted blobs between devices. It cannot read your data.

```
https://your-server.com/r/coral-tiger-88#E2EKeyHere
                         └── sent to server  └── never sent to server
```

## Features

- **Text & clipboard** — paste text, links, code snippets
- **File streaming** — drag-and-drop files, streamed in 64KB encrypted chunks
- **Link previews** — OG tag extraction for shared URLs
- **P2P upgrade** — WebRTC direct connection for 2-device rooms on LAN
- **Pinned rooms** — persistent rooms with optional passphrase protection
- **PWA** — installable on mobile, share target support on Android
- **CLI client** — `beam send`, `beam receive`, `beam new`
- **Desktop agent** — Tauri tray app for clipboard auto-sync
- **Dark/light theme** — follows system preference

## Configuration

All settings can be set via flags or `BEAM_*` environment variables.

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--port` | `BEAM_PORT` | `8080` | Server port |
| `--host` | `BEAM_HOST` | `0.0.0.0` | Bind address |
| `--max-room-size` | `BEAM_MAX_ROOM_SIZE` | `10` | Max devices per room |
| `--default-ttl` | `BEAM_DEFAULT_TTL` | `30m` | Default item TTL |
| `--max-file-buffer` | `BEAM_MAX_FILE_BUFFER` | `268435456` | Max file buffer (bytes) |
| `--enable-pinned-rooms` | `BEAM_ENABLE_PINNED_ROOMS` | `true` | Allow pinned rooms |
| `--data-dir` | `BEAM_DATA_DIR` | `./data` | Data directory |
| `--tls-cert` | `BEAM_TLS_CERT` | | TLS certificate path |
| `--tls-key` | `BEAM_TLS_KEY` | | TLS key path |
| `--grace-period` | `BEAM_GRACE_PERIOD` | `5m` | Room grace period |
| `--max-rooms` | `BEAM_MAX_ROOMS` | `1000` | Max concurrent rooms |

## API

```bash
# Health check
curl http://localhost:8080/api/health

# Create a room
curl -X POST http://localhost:8080/api/rooms

# Create a pinned room
curl -X POST http://localhost:8080/api/rooms \
  -H "Content-Type: application/json" \
  -d '{"pinned": true, "passphrase": "secret"}'

# Get room info
curl http://localhost:8080/api/rooms/coral-tiger-88

# Send an item (encrypted client-side)
curl -X POST http://localhost:8080/api/rooms/coral-tiger-88/items \
  -H "Content-Type: application/json" \
  -d '{"kind": "text", "encrypted_data": "...", "nonce": "..."}'

# Get link preview
curl "http://localhost:8080/api/preview?url=https://example.com"
```

## CLI

```bash
# Create a new room
beam new --server http://localhost:8080

# Send a file
beam send ./report.pdf --room coral-tiger-88 --key <base64key>

# Send text from stdin
echo "hello world" | beam send --room coral-tiger-88 --key <base64key>

# Receive (watch room, download files)
beam receive --room coral-tiger-88 --key <base64key>
```

Build the CLI: `go build -o beam-cli ./cli/`

## Self-Hosting

### With TLS (recommended)

```bash
./beam --tls-cert /path/to/cert.pem --tls-key /path/to/key.pem --port 443
```

### Behind Nginx

```nginx
server {
    listen 443 ssl;
    server_name beam.example.com;

    ssl_certificate /etc/letsencrypt/live/beam.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/beam.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### Behind Caddy

```
beam.example.com {
    reverse_proxy localhost:8080
}
```

### Docker Compose

```yaml
services:
  beam:
    image: ghcr.io/aziz66/beam:latest
    ports:
      - "8080:8080"
    volumes:
      - beam-data:/app/data
    restart: unless-stopped

volumes:
  beam-data:
```

## Security

- All content is encrypted client-side using **TweetNaCl** (secretbox: XSalsa20-Poly1305)
- The encryption key lives in the URL fragment (`#key`) which **browsers never send to servers**
- The server only sees encrypted blobs, room codes, and device IDs
- Pinned room passphrases are hashed with **bcrypt** (cost 12)
- Link preview endpoint has **SSRF protection** (blocks private IP ranges)
- All randomness uses `crypto/rand` (Go) or `crypto.getRandomValues` (browser)

## Architecture

```
beam/
├── main.go                  # Entry point, HTTP routing, go:embed
├── internal/
│   ├── config/              # Configuration (flags + env vars)
│   ├── hub/                 # WebSocket connection manager
│   ├── room/                # Room lifecycle, BadgerDB store
│   ├── relay/               # Message forwarding
│   ├── signaling/           # WebRTC SDP/ICE relay
│   ├── api/                 # REST API handlers
│   ├── preview/             # Link OG-tag fetcher
│   ├── protocol/            # Wire protocol message types
│   └── namegen/             # Room code generator
├── web/                     # Frontend (embedded via go:embed)
├── cli/                     # CLI client
└── agent/                   # Desktop tray agent (Tauri)
```

## License

MIT
