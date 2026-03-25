package api

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aziz66/beam/internal/namegen"
	"github.com/aziz66/beam/internal/preview"
	"github.com/aziz66/beam/internal/protocol"
	"github.com/aziz66/beam/internal/room"
)

type ipEntry struct {
	count    int
	windowAt time.Time
}

type API struct {
	manager        *room.Manager
	previewer      *preview.Previewer
	trustedProxy   bool
	ipMu           sync.Mutex
	ipLimiter      map[string]*ipEntry // room creation
	deleteMu       sync.Mutex
	deleteLimiter  map[string]*ipEntry // room deletion (separate so DELETE can't exhaust creation quota)
	previewMu      sync.Mutex
	previewLimiter map[string]*ipEntry
	itemsMu        sync.Mutex
	itemsLimiter   map[string]*ipEntry // POST /api/rooms/{code}/items
}

func New(manager *room.Manager, previewer *preview.Previewer, trustedProxy bool) *API {
	return &API{
		manager:        manager,
		previewer:      previewer,
		trustedProxy:   trustedProxy,
		ipLimiter:      make(map[string]*ipEntry),
		deleteLimiter:  make(map[string]*ipEntry),
		previewLimiter: make(map[string]*ipEntry),
		itemsLimiter:   make(map[string]*ipEntry),
	}
}

// roomCreateAllowed returns true if the IP is within the rate limit:
// max 10 room creations per minute per IP.
func (a *API) roomCreateAllowed(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	// Only trust X-Forwarded-For when explicitly configured (prevents IP spoofing).
	// Use the rightmost entry — appended by our trusted proxy, not the client.
	if a.trustedProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			ip = strings.TrimSpace(parts[len(parts)-1])
		}
	}

	const maxPerMinute = 10
	const maxTrackedIPs = 5000
	now := time.Now()

	a.ipMu.Lock()
	defer a.ipMu.Unlock()

	// Evict stale entries (older than 1 minute)
	for k, e := range a.ipLimiter {
		if now.Sub(e.windowAt) > time.Minute {
			delete(a.ipLimiter, k)
		}
	}

	// Hard cap: if still too large (botnet with rotating IPs), prune to 75% to
	// preserve rate-limit state rather than wiping and letting attackers reset it.
	if len(a.ipLimiter) >= maxTrackedIPs {
		target := maxTrackedIPs * 3 / 4
		for k := range a.ipLimiter {
			if len(a.ipLimiter) <= target {
				break
			}
			delete(a.ipLimiter, k)
		}
	}

	entry, ok := a.ipLimiter[ip]
	if !ok || now.Sub(entry.windowAt) > time.Minute {
		a.ipLimiter[ip] = &ipEntry{count: 1, windowAt: now}
		return true
	}
	if entry.count >= maxPerMinute {
		return false
	}
	entry.count++
	return true
}

// deleteRoomAllowed rate-limits DELETE /api/rooms/{code} independently from
// room creation so that a flood of DELETE requests cannot exhaust the creation
// quota for legitimate users. Max 30 deletions per minute per IP.
func (a *API) deleteRoomAllowed(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if a.trustedProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			ip = strings.TrimSpace(parts[len(parts)-1])
		}
	}

	const maxPerMinute = 30
	const maxTrackedIPs = 5000
	now := time.Now()

	a.deleteMu.Lock()
	defer a.deleteMu.Unlock()

	for k, e := range a.deleteLimiter {
		if now.Sub(e.windowAt) > time.Minute {
			delete(a.deleteLimiter, k)
		}
	}
	if len(a.deleteLimiter) >= maxTrackedIPs {
		target := maxTrackedIPs * 3 / 4
		for k := range a.deleteLimiter {
			if len(a.deleteLimiter) <= target {
				break
			}
			delete(a.deleteLimiter, k)
		}
	}

	entry, ok := a.deleteLimiter[ip]
	if !ok || now.Sub(entry.windowAt) > time.Minute {
		a.deleteLimiter[ip] = &ipEntry{count: 1, windowAt: now}
		return true
	}
	if entry.count >= maxPerMinute {
		return false
	}
	entry.count++
	return true
}

// previewAllowed limits /api/preview to 30 fetches per minute per IP.
func (a *API) previewAllowed(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if a.trustedProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			ip = strings.TrimSpace(parts[len(parts)-1])
		}
	}

	const maxPerMinute = 30
	const maxTrackedIPs = 5000
	now := time.Now()

	a.previewMu.Lock()
	defer a.previewMu.Unlock()

	for k, e := range a.previewLimiter {
		if now.Sub(e.windowAt) > time.Minute {
			delete(a.previewLimiter, k)
		}
	}
	if len(a.previewLimiter) >= maxTrackedIPs {
		target := maxTrackedIPs * 3 / 4
		for k := range a.previewLimiter {
			if len(a.previewLimiter) <= target {
				break
			}
			delete(a.previewLimiter, k)
		}
	}

	entry, ok := a.previewLimiter[ip]
	if !ok || now.Sub(entry.windowAt) > time.Minute {
		a.previewLimiter[ip] = &ipEntry{count: 1, windowAt: now}
		return true
	}
	if entry.count >= maxPerMinute {
		return false
	}
	entry.count++
	return true
}

// itemsPostAllowed limits POST /api/rooms/{code}/items to 60 per minute per IP.
func (a *API) itemsPostAllowed(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if a.trustedProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			ip = strings.TrimSpace(parts[len(parts)-1])
		}
	}

	const maxPerMinute = 60
	const maxTrackedIPs = 5000
	now := time.Now()

	a.itemsMu.Lock()
	defer a.itemsMu.Unlock()

	for k, e := range a.itemsLimiter {
		if now.Sub(e.windowAt) > time.Minute {
			delete(a.itemsLimiter, k)
		}
	}
	if len(a.itemsLimiter) >= maxTrackedIPs {
		target := maxTrackedIPs * 3 / 4
		for k := range a.itemsLimiter {
			if len(a.itemsLimiter) <= target {
				break
			}
			delete(a.itemsLimiter, k)
		}
	}

	entry, ok := a.itemsLimiter[ip]
	if !ok || now.Sub(entry.windowAt) > time.Minute {
		a.itemsLimiter[ip] = &ipEntry{count: 1, windowAt: now}
		return true
	}
	if entry.count >= maxPerMinute {
		return false
	}
	entry.count++
	return true
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", a.handleHealth)
	mux.HandleFunc("/api/rooms", a.handleRooms)
	mux.HandleFunc("/api/rooms/", a.handleRoomByCode)
	mux.HandleFunc("/api/preview", a.handlePreview)
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"ts":     time.Now().UnixMilli(),
	})
}

func (a *API) handleRooms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.createRoom(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type createRoomRequest struct {
	Pinned     bool   `json:"pinned"`
	Passphrase string `json:"passphrase"`
}

func (a *API) createRoom(w http.ResponseWriter, r *http.Request) {
	if !a.roomCreateAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req createRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body for simple creation
		req = createRoomRequest{}
	}

	rm, err := a.manager.CreateRoom(req.Pinned, req.Passphrase)
	if err != nil {
		log.Printf("create room failed: %v", err)
		switch {
		case strings.Contains(err.Error(), "maximum room limit"):
			writeError(w, http.StatusServiceUnavailable, "maximum room limit reached")
		case strings.Contains(err.Error(), "passphrase too long"):
			writeError(w, http.StatusBadRequest, err.Error())
		case strings.Contains(err.Error(), "room code conflict"):
			writeError(w, http.StatusConflict, "room code conflict, please retry")
		default:
			writeError(w, http.StatusServiceUnavailable, "service unavailable")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"room_code":  rm.Code,
		"created_at": rm.CreatedAt.UnixMilli(),
		"pinned":     rm.Pinned,
	})
}

func (a *API) handleRoomByCode(w http.ResponseWriter, r *http.Request) {
	// Parse: /api/rooms/{code} or /api/rooms/{code}/items
	path := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	parts := strings.SplitN(path, "/", 2)
	code := parts[0]

	if code == "" {
		writeError(w, http.StatusBadRequest, "room code required")
		return
	}

	if !namegen.Validate(code) {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}

	if len(parts) == 2 && parts[1] == "items" {
		a.handleRoomItems(w, r, code)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.getRoomInfo(w, r, code)
	case http.MethodDelete:
		a.deleteRoom(w, r, code)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *API) getRoomInfo(w http.ResponseWriter, r *http.Request, code string) {
	rm := a.manager.GetRoom(code)
	if rm == nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	// Passphrase-protected rooms require authentication to prevent metadata leakage.
	if rm.Pinned && rm.Passphrase != "" {
		passphrase := r.Header.Get("X-Passphrase")
		if passphrase == "" {
			writeError(w, http.StatusUnauthorized, "passphrase required for pinned rooms")
			return
		}
		if err := room.ComparePassphrase(rm.Passphrase, passphrase); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid passphrase")
			return
		}
	}

	writeJSON(w, http.StatusOK, protocol.RoomInfoPayload{
		RoomCode:    rm.Code,
		DeviceCount: rm.ClientCount(),
		CreatedAt:   rm.CreatedAt.UnixMilli(),
		Pinned:      rm.Pinned,
	})
}

func (a *API) deleteRoom(w http.ResponseWriter, r *http.Request, code string) {
	if !a.deleteRoomAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	rm := a.manager.GetRoom(code)
	if rm == nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	// Pinned rooms require passphrase — accept via X-Passphrase header only
	if rm.Pinned && rm.Passphrase != "" {
		passphrase := r.Header.Get("X-Passphrase")
		if passphrase == "" {
			writeError(w, http.StatusUnauthorized, "passphrase required for pinned rooms")
			return
		}
		if err := room.ComparePassphrase(rm.Passphrase, passphrase); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid passphrase")
			return
		}
	}

	a.manager.DeleteRoom(code)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *API) handleRoomItems(w http.ResponseWriter, r *http.Request, code string) {
	rm := a.manager.GetRoom(code)
	if rm == nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Gate item listing behind the same passphrase check as POST.
		if rm.Pinned && rm.Passphrase != "" {
			passphrase := r.Header.Get("X-Passphrase")
			if passphrase == "" {
				writeError(w, http.StatusUnauthorized, "passphrase required for pinned rooms")
				return
			}
			if err := room.ComparePassphrase(rm.Passphrase, passphrase); err != nil {
				writeError(w, http.StatusUnauthorized, "invalid passphrase")
				return
			}
		}
		items := rm.GetRecentItems()
		// [][]byte marshals as base64 in Go's json encoder; wrap each element as
		// json.RawMessage so clients receive JSON objects, not base64 strings.
		rawItems := make([]json.RawMessage, len(items))
		for i, item := range items {
			rawItems[i] = json.RawMessage(item)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": rawItems,
			"count": len(rawItems),
		})
	case http.MethodPost:
		if !a.itemsPostAllowed(r) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		// Pinned rooms with passphrases require authentication
		if rm.Pinned && rm.Passphrase != "" {
			passphrase := r.Header.Get("X-Passphrase")
			if passphrase == "" {
				writeError(w, http.StatusUnauthorized, "passphrase required for pinned rooms")
				return
			}
			if err := room.ComparePassphrase(rm.Passphrase, passphrase); err != nil {
				writeError(w, http.StatusUnauthorized, "invalid passphrase")
				return
			}
		}
		// 512 KB limit — encrypted items include base64-encoded ciphertext which
		// expands ~4/3; this covers ~370 KB of plaintext, sufficient for typical
		// clipboard content. The WebSocket path enforces a separate 10 MB limit.
		r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
		var payload protocol.ItemPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "invalid payload")
			return
		}

		env, err := protocol.NewEnvelope(protocol.TypeItem, payload)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "envelope creation failed")
			return
		}
		env.DeviceID = "api"

		data, err := json.Marshal(env)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "marshal failed")
			return
		}

		// Respect client-requested TTL (clamped to room default), consistent with the WS path.
		// Cap at 24 h to prevent integer overflow in time.Duration arithmetic.
		ttl := rm.DefaultTTL
		if payload.TTL > 0 && payload.TTL <= 86400 {
			if clientTTL := time.Duration(payload.TTL) * time.Second; clientTTL < ttl {
				ttl = clientTTL
			}
		}
		rm.StoreItem(data, ttl)

		// Broadcast to connected clients
		for _, c := range rm.GetClients() {
			c.TrySend(data)
		}

		writeJSON(w, http.StatusCreated, map[string]string{"status": "sent"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *API) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !a.previewAllowed(r) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeError(w, http.StatusBadRequest, "url parameter required")
		return
	}

	result, err := a.previewer.Fetch(rawURL)
	if err != nil {
		log.Printf("preview fetch failed for %q: %v", rawURL, err)
		writeError(w, http.StatusBadGateway, "preview unavailable")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
