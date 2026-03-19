package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/beam-sh/beam/internal/protocol"
	"github.com/beam-sh/beam/internal/room"
)

const (
	maxMessageSize = 10 * 1024 * 1024 // 10MB
	pongWait       = 60 * time.Second
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for now
	},
}

type Hub struct {
	manager *room.Manager
}

func New(manager *room.Manager) *Hub {
	return &Hub{manager: manager}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Extract room code from URL path: /ws/{roomCode}
	path := strings.TrimPrefix(r.URL.Path, "/ws/")
	roomCode := strings.TrimSuffix(path, "/")
	if roomCode == "" {
		http.Error(w, "room code required", http.StatusBadRequest)
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
		rm.StartGraceTimer(5*time.Minute, func() {
			if rm.IsEmpty() {
				h.manager.DeleteRoom(roomCode)
			}
		})
	}
}

func (h *Hub) readPump(conn *websocket.Conn, client *room.Client, rm *room.Room) {
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("read error: device=%s: %v", client.DeviceID, err)
			}
			return
		}

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
			sender.DeviceLabel = payload.DeviceLabel
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
