package hub

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/aziz66/beam/internal/protocol"
	"github.com/aziz66/beam/internal/room"
)

var roomCodeRe = regexp.MustCompile(`^[a-z]+-[a-z]+-\d{2,3}$`)

const (
	maxMessageSize = 10 * 1024 * 1024 // 10MB
	pongWait       = 60 * time.Second
	rateMsgPerSec  = 30.0 // token refill rate
	rateBurst      = 60.0 // initial token bucket size
)

var upgrader = websocket.Upgrader{
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
		return u.Host == r.Host
	},
}

const maxDeviceLabelLen = 64

type Hub struct {
	manager     *room.Manager
	gracePeriod time.Duration
}

func New(manager *room.Manager, gracePeriod time.Duration) *Hub {
	return &Hub{manager: manager, gracePeriod: gracePeriod}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Extract room code from URL path: /ws/{roomCode}
	path := strings.TrimPrefix(r.URL.Path, "/ws/")
	roomCode := strings.TrimSuffix(path, "/")
	if roomCode == "" || !roomCodeRe.MatchString(roomCode) {
		http.Error(w, "invalid room code", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
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
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Get or create the room
	rm, err := h.manager.GetOrCreateRoom(roomCode)
	if err != nil {
		h.sendError(conn, "room_full", err.Error())
		conn.Close()
		return
	}

	client := room.NewClient(deviceID, "", conn)

	if !rm.AddClient(client) {
		h.sendError(conn, "room_full", "room is at capacity")
		conn.Close()
		return
	}

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

	// Cleanup on disconnect
	rm.RemoveClient(deviceID)
	log.Printf("disconnected: device=%s room=%s", deviceID, roomCode)

	h.broadcastDeviceList(rm)

	if rm.IsEmpty() && !rm.Pinned {
		rm.StartGraceTimer(h.gracePeriod, func() {
			if rm.IsEmpty() {
				h.manager.DeleteRoom(roomCode)
			}
		})
	}
}

func (h *Hub) readPump(conn *websocket.Conn, client *room.Client, rm *room.Room) {
	defer conn.Close()

	// Token bucket rate limiter (per-connection, single goroutine — no mutex needed)
	tokens := rateBurst
	lastRefill := time.Now()

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
			log.Printf("rate limit exceeded: device=%s, dropping message", client.DeviceID)
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
			if len(label) > maxDeviceLabelLen {
				label = label[:maxDeviceLabelLen]
			}
			sender.DeviceLabel = label
			h.broadcastDeviceList(rm)
		}

	case protocol.TypeItem:
		// Re-marshal with server-set fields
		data := h.remarshal(env)
		rm.StoreItem(data, rm.DefaultTTL)
		h.relayToRoom(data, sender, rm)

	case protocol.TypeFileMeta, protocol.TypeFileComplete, protocol.TypeFileCancel:
		data := h.remarshal(env)
		h.relayToRoom(data, sender, rm)

	case protocol.TypeFileChunk:
		data := h.remarshal(env)
		h.relayToRoom(data, sender, rm)

	case protocol.TypeSignalOffer, protocol.TypeSignalAnswer, protocol.TypeSignalICE:
		if env.TargetID != "" {
			data := h.remarshal(env)
			h.relayToDevice(data, env.TargetID, rm)
		}

	case protocol.TypePing:
		pong, _ := protocol.NewEnvelope(protocol.TypePong, nil)
		data, _ := json.Marshal(pong)
		select {
		case sender.Send <- data:
		default:
		}
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
		select {
		case c.Send <- data:
		default:
			log.Printf("send buffer full for device=%s, dropping message", c.DeviceID)
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
	select {
	case c.Send <- data:
	default:
		log.Printf("send buffer full for device=%s, dropping message", targetID)
	}
}

func (h *Hub) broadcastDeviceList(rm *room.Room) {
	clients := rm.GetClients()
	devices := make([]protocol.DeviceInfo, 0, len(clients))
	for _, c := range clients {
		devices = append(devices, protocol.DeviceInfo{
			DeviceID:    c.DeviceID,
			DeviceLabel: c.DeviceLabel,
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
		select {
		case c.Send <- data:
		default:
		}
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
	select {
	case client.Send <- data:
	default:
	}
}

func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
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
	conn.WriteMessage(websocket.TextMessage, data)
}
