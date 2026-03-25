package room

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxStoredItems = 50
	// maxItemBytes caps the stored size per item envelope to prevent a single
	// large message from consuming disproportionate server memory. Items larger
	// than this are relayed but not stored in the room history.
	maxItemBytes   = 256 * 1024 // 256 KB
	sendBufferSize = 256
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
)

type Room struct {
	Code       string
	CreatedAt  time.Time
	clients    map[string]*Client
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
	closeOnce   sync.Once
}

type StoredItem struct {
	Envelope  []byte
	ExpiresAt time.Time
}

func NewRoom(code string, maxSize int, defaultTTL time.Duration) *Room {
	return &Room{
		Code:       code,
		CreatedAt:  time.Now(),
		clients:    make(map[string]*Client),
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

	if len(r.clients) >= r.MaxSize {
		return false
	}
	r.clients[c.DeviceID] = c
	return true
}

func (r *Room) RemoveClient(deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, deviceID)
}

func (r *Room) GetClients() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clients := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	return clients
}

func (r *Room) GetClient(deviceID string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[deviceID]
}

func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

func (r *Room) StoreItem(data []byte, ttl time.Duration) {
	if len(data) > maxItemBytes {
		return // relay but do not store oversized items
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	item := &StoredItem{
		Envelope:  data,
		ExpiresAt: time.Now().Add(ttl),
	}
	r.Items = append(r.Items, item)

	// FIFO eviction — copy rather than reslice so the evicted pointers are
	// freed by the GC instead of staying alive in the backing array.
	if len(r.Items) > maxStoredItems {
		keep := r.Items[len(r.Items)-maxStoredItems:]
		r.Items = make([]*StoredItem, maxStoredItems)
		copy(r.Items, keep)
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
	return len(r.clients) == 0
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
	for _, c := range r.clients {
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
			if err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.Send)
	})
	// Conn is closed by WritePump's deferred c.Conn.Close().
	// Do NOT call c.Conn.Close() here — it races with WritePump's in-progress write.
}

// TrySend attempts a non-blocking send to the client's Send channel.
// Returns false if the buffer is full or the client has already disconnected.
// A deferred recover() guards against the race where Close() fires between
// the liveness check and the select in the calling goroutine.
func (c *Client) TrySend(data []byte) bool {
	defer func() { recover() }() //nolint:errcheck
	select {
	case c.Send <- data:
		return true
	default:
		return false
	}
}

func (r *Room) HasGraceTimer() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.graceTimer != nil
}

func (c *Client) GetDeviceLabel() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.DeviceLabel
}

func (c *Client) SetDeviceLabel(label string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DeviceLabel = label
}

func (r *Room) StartGraceTimerIfNone(duration time.Duration, onExpire func()) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.graceTimer != nil {
		return false
	}
	r.graceTimer = time.AfterFunc(duration, onExpire)
	return true
}
