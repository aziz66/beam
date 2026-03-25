package hub

import (
	"encoding/json"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/aziz66/beam/internal/namegen"
	"github.com/aziz66/beam/internal/protocol"
	"github.com/aziz66/beam/internal/room"
)

const (
	maxMessageSize = 10 * 1024 * 1024 // 10MB
	pongWait       = 60 * time.Second
	rateMsgPerSec  = 30.0 // token refill rate
	rateBurst      = 60.0 // initial token bucket size
)

func newUpgrader(trustedProxy bool) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // non-browser clients (CLI)
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			host := r.Host
			// When behind a trusted reverse proxy, prefer X-Forwarded-Host so the
			// origin check matches the public-facing hostname the browser used,
			// not the internal upstream address that the proxy rewrites Host to.
			if trustedProxy {
				if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
					host = strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0])
				}
			}
			return u.Host == host
		},
	}
}

const maxDeviceLabelLen = 64

type wsConnEntry struct {
	count    int
	windowAt time.Time
}

type Hub struct {
	manager      *room.Manager
	gracePeriod  time.Duration
	trustedProxy bool
	upgrader     websocket.Upgrader
	wsConnMu     sync.Mutex
	wsConnCount  map[string]*wsConnEntry
}

func New(manager *room.Manager, gracePeriod time.Duration, trustedProxy bool) *Hub {
	return &Hub{
		manager:      manager,
		gracePeriod:  gracePeriod,
		trustedProxy: trustedProxy,
		upgrader:     newUpgrader(trustedProxy),
		wsConnCount:  make(map[string]*wsConnEntry),
	}
}

// wsConnAllowed limits WebSocket connections to 20 per IP per minute.
func (h *Hub) wsConnAllowed(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if h.trustedProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			// Use the rightmost entry — it was appended by our trusted proxy
			// and reflects the real client IP. The leftmost is client-controlled.
			parts := strings.Split(fwd, ",")
			ip = strings.TrimSpace(parts[len(parts)-1])
		}
	}

	const maxPerMinute = 20
	const maxTrackedIPs = 5000
	now := time.Now()

	h.wsConnMu.Lock()
	defer h.wsConnMu.Unlock()

	// Evict stale entries
	for k, e := range h.wsConnCount {
		if now.Sub(e.windowAt) > time.Minute {
			delete(h.wsConnCount, k)
		}
	}
	if len(h.wsConnCount) >= maxTrackedIPs {
		// Stale entries were just evicted above; if still at capacity (many concurrent
		// active IPs), prune down to 75% to preserve rate-limit state for existing IPs
		// rather than wiping everything and letting attackers bypass the limiter.
		target := maxTrackedIPs * 3 / 4
		for k := range h.wsConnCount {
			if len(h.wsConnCount) <= target {
				break
			}
			delete(h.wsConnCount, k)
		}
	}

	entry, ok := h.wsConnCount[ip]
	if !ok || now.Sub(entry.windowAt) > time.Minute {
		h.wsConnCount[ip] = &wsConnEntry{count: 1, windowAt: now}
		return true
	}
	if entry.count >= maxPerMinute {
		return false
	}
	entry.count++
	return true
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Extract room code from URL path: /ws/{roomCode}
	path := strings.TrimPrefix(r.URL.Path, "/ws/")
	roomCode := strings.TrimSuffix(path, "/")
	if roomCode == "" || !namegen.Validate(roomCode) {
		http.Error(w, "invalid room code", http.StatusBadRequest)
		return
	}

	if !h.wsConnAllowed(r) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}

	deviceID := uuid.New().String()
	log.Printf("new connection: device=%s room=%s", deviceID, roomCode)

	h.handleConnection(conn, deviceID, roomCode)
}

func (h *Hub) handleConnection(conn *websocket.Conn, deviceID, roomCode string) {
	conn.SetReadLimit(maxMessageSize)

	// Read the join message with a short deadline BEFORE allocating a room slot.
	// This prevents exhausting MaxRooms by opening connections without ever joining.
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		return
	}

	_, firstMsg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	var joinEnv protocol.Envelope
	var joinPayload protocol.JoinPayload
	if err := json.Unmarshal(firstMsg, &joinEnv); err != nil || joinEnv.Type != protocol.TypeJoin {
		h.sendError(conn, "protocol_error", "expected join message")
		conn.Close()
		return
	}
	joinEnv.ParsePayload(&joinPayload) // best-effort; empty label is fine

	// Now get or create the room (slot only consumed after client proved it can talk).
	rm, err := h.manager.GetOrCreateRoom(roomCode)
	if err != nil {
		h.sendError(conn, "room_full", err.Error())
		conn.Close()
		return
	}

	// If we return early (passphrase fail, AddClient fail) before the client is
	// registered, a freshly-created non-pinned room could linger empty until the
	// next cleanup tick (~60s). Schedule a grace timer so it is reaped promptly.
	clientAdded := false
	defer func() {
		if !clientAdded && rm.IsEmpty() && !rm.Pinned {
			rm.StartGraceTimerIfNone(h.gracePeriod, func() {
				if rm.IsEmpty() {
					h.manager.DeleteRoomIfMatch(roomCode, rm)
				}
			})
		}
	}()

	// Enforce passphrase for rooms that have one (pinned rooms only).
	// Use the semaphore-gated ComparePassphrase so concurrent WS joins cannot
	// saturate all CPU cores with bcrypt work.
	if rm.Pinned && rm.Passphrase != "" {
		if err := room.ComparePassphrase(rm.Passphrase, joinPayload.Passphrase); err != nil {
			h.sendError(conn, "auth_required", "invalid passphrase")
			conn.Close()
			return
		}
	}

	// Reset deadline to full pong-wait for normal operation.
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		conn.Close()
		return
	}
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	deviceLabel := ""
	if joinPayload.DeviceLabel != "" {
		deviceLabel = sanitizeLabel(joinPayload.DeviceLabel)
		if len([]rune(deviceLabel)) > maxDeviceLabelLen {
			deviceLabel = string([]rune(deviceLabel)[:maxDeviceLabelLen])
		}
	}

	client := room.NewClient(deviceID, deviceLabel, conn)

	if !rm.AddClient(client) {
		h.sendError(conn, "room_full", "room is at capacity")
		conn.Close()
		return
	}
	clientAdded = true

	rm.CancelGraceTimer()

	// Start write pump
	go client.WritePump()

	// Send JOINED
	h.sendJoined(client, roomCode, deviceID)

	// Send recent items
	for _, itemData := range rm.GetRecentItems() {
		select {
		case client.Send <- itemData:
		default:
		}
	}

	// Broadcast device list
	h.broadcastDeviceList(rm)

	// Read pump (blocks until disconnect)
	h.readPump(conn, client, rm)

	// Cleanup on disconnect — remove from room first so no new sends reach this
	// client's (now-closing) channel, then signal WritePump to drain and exit.
	rm.RemoveClient(deviceID)
	client.Close()
	log.Printf("disconnected: device=%s room=%s", deviceID, roomCode)

	h.broadcastDeviceList(rm)

	if rm.IsEmpty() && !rm.Pinned {
		rm.StartGraceTimer(h.gracePeriod, func() {
			if rm.IsEmpty() {
				h.manager.DeleteRoomIfMatch(roomCode, rm)
			}
		})
	}
}

func (h *Hub) readPump(conn *websocket.Conn, client *room.Client, rm *room.Room) {
	// Do NOT defer conn.Close() here — WritePump owns the connection lifecycle.
	// Calling conn.Close() concurrently with WritePump's in-progress write panics.

	// Token bucket rate limiter (per-connection, single goroutine — no mutex needed)
	tokens := rateBurst
	lastRefill := time.Now()
	// Throttle rate-limit log messages to at most one per second per connection
	// so a flooding client cannot fill the disk with log entries.
	var lastRateLimitLog time.Time

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("read error: device=%s: %v", client.DeviceID, err)
			}
			return
		}

		// Refill tokens
		now := time.Now()
		tokens = math.Min(rateBurst, tokens+now.Sub(lastRefill).Seconds()*rateMsgPerSec)
		lastRefill = now
		if tokens < 1 {
			if time.Since(lastRateLimitLog) > time.Second {
				log.Printf("rate limit exceeded: device=%s, dropping messages", client.DeviceID)
				lastRateLimitLog = time.Now()
			}
			continue
		}
		tokens--

		var env protocol.Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			log.Printf("invalid message from device=%s: %v", client.DeviceID, err)
			continue
		}

		env.DeviceID = client.DeviceID
		env.Ts = time.Now().UnixMilli()

		h.routeMessage(&env, message, client, rm)
	}
}

func (h *Hub) routeMessage(env *protocol.Envelope, raw []byte, sender *room.Client, rm *room.Room) {
	switch env.Type {
	case protocol.TypeJoin:
		var payload protocol.JoinPayload
		if err := env.ParsePayload(&payload); err != nil {
			return
		}
		if payload.DeviceLabel != "" {
			label := sanitizeLabel(payload.DeviceLabel)
			if len([]rune(label)) > maxDeviceLabelLen {
				label = string([]rune(label)[:maxDeviceLabelLen])
			}
			sender.SetDeviceLabel(label)
			h.broadcastDeviceList(rm)
		}

	case protocol.TypeItem:
		// Re-marshal with server-set fields
		data := h.remarshal(env)
		if data == nil {
			return
		}
		ttl := rm.DefaultTTL
		var itemPayload protocol.ItemPayload
		if env.ParsePayload(&itemPayload) == nil && itemPayload.TTL > 0 && itemPayload.TTL <= 86400 {
			// Cap at 24 h (86400 s) to prevent integer overflow in time.Duration arithmetic
			clientTTL := time.Duration(itemPayload.TTL) * time.Second
			if clientTTL < rm.DefaultTTL {
				ttl = clientTTL
			}
		}
		rm.StoreItem(data, ttl)
		h.relayToRoom(data, sender, rm)

	case protocol.TypeFileMeta, protocol.TypeFileComplete, protocol.TypeFileCancel:
		data := h.remarshal(env)
		if data == nil {
			return
		}
		h.relayToRoom(data, sender, rm)

	case protocol.TypeFileChunk:
		data := h.remarshal(env)
		if data == nil {
			return
		}
		h.relayToRoom(data, sender, rm)

	case protocol.TypeSignalOffer, protocol.TypeSignalAnswer, protocol.TypeSignalICE:
		if env.TargetID != "" {
			data := h.remarshal(env)
			if data == nil {
				return
			}
			h.relayToDevice(data, env.TargetID, rm)
		}

	case protocol.TypePing:
		pong, _ := protocol.NewEnvelope(protocol.TypePong, nil)
		data, _ := json.Marshal(pong)
		sender.TrySend(data)
	}
}

func (h *Hub) remarshal(env *protocol.Envelope) []byte {
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("marshal error: %v", err)
		return nil
	}
	return data
}

func (h *Hub) relayToRoom(data []byte, sender *room.Client, rm *room.Room) {
	if data == nil {
		return
	}
	for _, c := range rm.GetClients() {
		if c.DeviceID == sender.DeviceID {
			continue
		}
		if !c.TrySend(data) {
			log.Printf("send buffer full or client closed for device=%s, dropping message", c.DeviceID)
		}
	}
}

func (h *Hub) relayToDevice(data []byte, targetID string, rm *room.Room) {
	if data == nil {
		return
	}
	c := rm.GetClient(targetID)
	if c == nil {
		return
	}
	if !c.TrySend(data) {
		log.Printf("send buffer full or client closed for device=%s, dropping message", targetID)
	}
}

func (h *Hub) broadcastDeviceList(rm *room.Room) {
	clients := rm.GetClients()
	devices := make([]protocol.DeviceInfo, 0, len(clients))
	for _, c := range clients {
		devices = append(devices, protocol.DeviceInfo{
			DeviceID:    c.DeviceID,
			DeviceLabel: c.GetDeviceLabel(),
			JoinedAt:    c.JoinedAt.UnixMilli(),
		})
	}

	env, err := protocol.NewEnvelope(protocol.TypeDeviceList, protocol.DeviceListPayload{
		Devices: devices,
	})
	if err != nil {
		return
	}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}

	for _, c := range clients {
		c.TrySend(data)
	}
}

func (h *Hub) sendJoined(client *room.Client, roomCode, deviceID string) {
	env, err := protocol.NewEnvelope(protocol.TypeJoined, protocol.JoinedPayload{
		RoomCode: roomCode,
		DeviceID: deviceID,
	})
	if err != nil {
		return
	}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	client.TrySend(data)
}

func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !unicode.IsControl(r) && !isBidiOverride(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isBidiOverride returns true for Unicode bidirectional override/isolate
// characters that are not classified as control chars but can visually spoof
// device labels (e.g. U+202E RIGHT-TO-LEFT OVERRIDE).
func isBidiOverride(r rune) bool {
	return (r >= 0x200E && r <= 0x200F) || // LRM, RLM
		(r >= 0x202A && r <= 0x202E) || // LRE, RLE, PDF, LRO, RLO
		(r >= 0x2066 && r <= 0x2069) || // LRI, RLI, FSI, PDI
		r == 0x061C // Arabic Letter Mark
}

func (h *Hub) sendError(conn *websocket.Conn, code, message string) {
	env, err := protocol.NewEnvelope(protocol.TypeError, protocol.ErrorPayload{
		Code:    code,
		Message: message,
	})
	if err != nil {
		return
	}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("sendError write failed: %v", err)
	}
}
