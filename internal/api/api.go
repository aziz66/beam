package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/beam-sh/beam/internal/preview"
	"github.com/beam-sh/beam/internal/protocol"
	"github.com/beam-sh/beam/internal/room"
)

type API struct {
	manager   *room.Manager
	previewer *preview.Previewer
}

func New(manager *room.Manager, previewer *preview.Previewer) *API {
	return &API{
		manager:   manager,
		previewer: previewer,
	}
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
		"status":     "ok",
		"room_count": a.manager.GetRoomCount(),
		"ts":         time.Now().UnixMilli(),
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
	var req createRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body for simple creation
		req = createRoomRequest{}
	}

	rm, err := a.manager.CreateRoom(req.Pinned, req.Passphrase)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
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

	if len(parts) == 2 && parts[1] == "items" {
		a.handleRoomItems(w, r, code)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.getRoomInfo(w, code)
	case http.MethodDelete:
		a.deleteRoom(w, r, code)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *API) getRoomInfo(w http.ResponseWriter, code string) {
	rm := a.manager.GetRoom(code)
	if rm == nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	writeJSON(w, http.StatusOK, protocol.RoomInfoPayload{
		RoomCode:    rm.Code,
		DeviceCount: rm.ClientCount(),
		CreatedAt:   rm.CreatedAt.UnixMilli(),
		Pinned:      rm.Pinned,
	})
}

func (a *API) deleteRoom(w http.ResponseWriter, r *http.Request, code string) {
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
		if err := bcrypt.CompareHashAndPassword([]byte(rm.Passphrase), []byte(passphrase)); err != nil {
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
		items := rm.GetRecentItems()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": items,
			"count": len(items),
		})
	case http.MethodPost:
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

		rm.StoreItem(data, rm.DefaultTTL)

		// Broadcast to connected clients
		for _, c := range rm.GetClients() {
			select {
			case c.Send <- data:
			default:
			}
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

	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeError(w, http.StatusBadRequest, "url parameter required")
		return
	}

	result, err := a.previewer.Fetch(rawURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
