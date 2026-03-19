package room

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxStoredItems = 50
	sendBufferSize = 256
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
)

type Room struct {
	Code       string
	CreatedAt  time.Time
	Clients    map[string]*Client
	Items      []*StoredItem
	Pinned     bool
	Passphrase string // bcrypt hash
	MaxSize    int
	DefaultTTL time.Duration
	mu         sync.RWMutex
	graceTimer *time.Timer
}

type Client struct {
	DeviceID    string
	DeviceLabel string
	Conn        *websocket.Conn
	Send        chan []byte
	JoinedAt    time.Time
	mu          sync.Mutex
}

type StoredItem struct {
	Envelope  []byte
	ExpiresAt time.Time
}

func NewRoom(code string, maxSize int, defaultTTL time.Duration) *Room {
	return &Room{
		Code:       code,
		CreatedAt:  time.Now(),
		Clients:    make(map[string]*Client),
		Items:      make([]*StoredItem, 0),
		MaxSize:    maxSize,
		DefaultTTL: defaultTTL,
	}
}

func NewClient(deviceID, deviceLabel string, conn *websocket.Conn) *Client {
	return &Client{
		DeviceID:    deviceID,
		DeviceLabel: deviceLabel,
		Conn:        conn,
		Send:        make(chan []byte, sendBufferSize),
		JoinedAt:    time.Now(),
	}
}

func (r *Room) AddClient(c *Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.Clients) >= r.MaxSize {
		return false
	}
	r.Clients[c.DeviceID] = c
	return true
}

func (r *Room) RemoveClient(deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Clients, deviceID)
}

func (r *Room) GetClients() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clients := make([]*Client, 0, len(r.Clients))
	for _, c := range r.Clients {
		clients = append(clients, c)
	}
	return clients
}

func (r *Room) GetClient(deviceID string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Clients[deviceID]
}

func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Clients)
}

func (r *Room) StoreItem(data []byte, ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item := &StoredItem{
		Envelope:  data,
		ExpiresAt: time.Now().Add(ttl),
	}
	r.Items = append(r.Items, item)

	// FIFO eviction
	if len(r.Items) > maxStoredItems {
		r.Items = r.Items[len(r.Items)-maxStoredItems:]
	}
}

func (r *Room) GetRecentItems() [][]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	result := make([][]byte, 0, len(r.Items))
	for _, item := range r.Items {
		if now.Before(item.ExpiresAt) {
			result = append(result, item.Envelope)
		}
	}
	return result
}

func (r *Room) CleanExpiredItems() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	alive := make([]*StoredItem, 0, len(r.Items))
	removed := 0
	for _, item := range r.Items {
		if now.Before(item.ExpiresAt) {
			alive = append(alive, item)
		} else {
			removed++
		}
	}
	r.Items = alive
	return removed
}

func (r *Room) IsEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Clients) == 0
}

func (r *Room) StartGraceTimer(duration time.Duration, onExpire func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.graceTimer != nil {
		r.graceTimer.Stop()
	}
	r.graceTimer = time.AfterFunc(duration, onExpire)
}

func (r *Room) CancelGraceTimer() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.graceTimer != nil {
		r.graceTimer.Stop()
		r.graceTimer = nil
	}
}

func (r *Room) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.graceTimer != nil {
		r.graceTimer.Stop()
	}
	for _, c := range r.Clients {
		c.Close()
	}
}

// WritePump drains the Send channel and writes messages to the WebSocket.
// Run as a goroutine per client.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.Send:
	default:
		close(c.Send)
	}
	c.Conn.Close()
}
