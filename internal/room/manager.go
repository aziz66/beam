package room

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aziz66/beam/internal/config"
	"github.com/aziz66/beam/internal/namegen"
	"golang.org/x/crypto/bcrypt"
)

// maxPassphraseBytes is the bcrypt hard limit; bytes beyond this are silently
// truncated by bcrypt, so we reject passphrases that would be silently shortened.
const maxPassphraseBytes = 72

// bcryptSem limits concurrent bcrypt operations to 1 so a flood of requests
// with passphrases cannot saturate all CPU cores.
var bcryptSem = make(chan struct{}, 1)

// ComparePassphrase performs a bcrypt hash comparison gated by the global
// semaphore so concurrent API requests cannot saturate all CPU cores.
// It returns an error if the semaphore is not acquired within 30 seconds.
func ComparePassphrase(hash, passphrase string) error {
	select {
	case bcryptSem <- struct{}{}:
	case <-time.After(30 * time.Second):
		return fmt.Errorf("passphrase check timed out")
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(passphrase))
	<-bcryptSem
	return err
}

type Manager struct {
	rooms  map[string]*Room
	mu     sync.RWMutex
	config *config.Config
	store  *Store // nil if pinned rooms disabled
	done   chan struct{}
}

func NewManager(cfg *config.Config, store *Store) (*Manager, error) {
	m := &Manager{
		rooms:  make(map[string]*Room),
		config: cfg,
		store:  store,
		done:   make(chan struct{}),
	}

	// Restore pinned rooms from store
	if store != nil {
		metas, err := store.LoadAllRooms()
		if err != nil {
			return nil, fmt.Errorf("loading pinned rooms: %w", err)
		}
		for _, meta := range metas {
			r := NewRoom(meta.Code, meta.MaxSize, meta.DefaultTTL)
			r.Pinned = true
			r.Passphrase = meta.Passphrase
			r.CreatedAt = meta.CreatedAt
			m.rooms[meta.Code] = r
			log.Printf("restored pinned room: %s", meta.Code)
		}
	}

	go m.cleanupLoop()
	return m, nil
}

func (m *Manager) CreateRoom(pinned bool, passphrase string) (*Room, error) {
	if len(passphrase) > maxPassphraseBytes {
		return nil, fmt.Errorf("passphrase too long (max %d bytes)", maxPassphraseBytes)
	}
	// Phase 1: check limits and reserve a unique code (under lock, fast).
	m.mu.Lock()
	if len(m.rooms) >= m.config.MaxRooms {
		m.mu.Unlock()
		return nil, fmt.Errorf("maximum room limit reached")
	}
	var code string
	for i := 0; i < 10; i++ {
		candidate, err := namegen.Generate()
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("generating room code: %w", err)
		}
		if _, exists := m.rooms[candidate]; !exists {
			code = candidate
			break
		}
	}
	if code == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to generate unique room code")
	}
	enablePinned := m.config.EnablePinnedRooms
	maxRoomSize := m.config.MaxRoomSize
	defaultTTL := m.config.DefaultTTL
	m.mu.Unlock()

	// Phase 2: create room and hash passphrase outside the lock.
	// bcrypt at cost 12 takes ~300ms — holding the global write-lock during
	// that would stall every GetRoom/GetOrCreateRoom call server-wide.
	r := NewRoom(code, maxRoomSize, defaultTTL)
	if pinned && enablePinned {
		r.Pinned = true
		if passphrase != "" {
			bcryptSem <- struct{}{}
			hash, err := bcrypt.GenerateFromPassword([]byte(passphrase), 12)
			<-bcryptSem
			if err != nil {
				return nil, fmt.Errorf("hashing passphrase: %w", err)
			}
			r.Passphrase = string(hash)
		}
	}

	// Phase 3: re-acquire lock to insert (re-check limits in case another
	// CreateRoom filled the pool while we were hashing).
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.rooms) >= m.config.MaxRooms {
		return nil, fmt.Errorf("maximum room limit reached")
	}
	if _, exists := m.rooms[code]; exists {
		return nil, fmt.Errorf("room code conflict, please retry")
	}
	if r.Pinned && m.store != nil {
		meta := &RoomMetadata{
			Code:       r.Code,
			Passphrase: r.Passphrase,
			CreatedAt:  r.CreatedAt,
			DefaultTTL: r.DefaultTTL,
			MaxSize:    r.MaxSize,
		}
		if err := m.store.SaveRoom(meta); err != nil {
			return nil, fmt.Errorf("persisting room: %w", err)
		}
	}
	m.rooms[code] = r
	log.Printf("created room: %s (pinned=%v)", code, r.Pinned)
	return r, nil
}

func (m *Manager) GetRoom(code string) *Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[code]
}

func (m *Manager) GetOrCreateRoom(code string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r, exists := m.rooms[code]; exists {
		return r, nil
	}

	if len(m.rooms) >= m.config.MaxRooms {
		return nil, fmt.Errorf("maximum room limit reached")
	}

	r := NewRoom(code, m.config.MaxRoomSize, m.config.DefaultTTL)
	m.rooms[code] = r
	log.Printf("created room on join: %s", code)
	return r, nil
}

func (m *Manager) DeleteRoom(code string) {
	m.mu.Lock()
	r, exists := m.rooms[code]
	if !exists {
		m.mu.Unlock()
		return
	}
	delete(m.rooms, code)
	m.mu.Unlock()

	r.Close()

	if r.Pinned && m.store != nil {
		if err := m.store.DeleteRoom(code); err != nil {
			log.Printf("failed to delete room from store: %s: %v", code, err)
		}
	}
	log.Printf("deleted room: %s", code)
}

// DeleteRoomIfMatch deletes the room only if the current room in the map is
// the same pointer as expected. This prevents stale grace-timer closures from
// deleting a newly re-created room that was assigned the same code.
func (m *Manager) DeleteRoomIfMatch(code string, expected *Room) {
	m.mu.Lock()
	r, exists := m.rooms[code]
	if !exists || r != expected {
		m.mu.Unlock()
		return
	}
	delete(m.rooms, code)
	m.mu.Unlock()

	r.Close()

	if r.Pinned && m.store != nil {
		if err := m.store.DeleteRoom(code); err != nil {
			log.Printf("failed to delete room from store: %s: %v", code, err)
		}
	}
	log.Printf("deleted room: %s", code)
}

func (m *Manager) GetRoomCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rooms)
}

func (m *Manager) Close() {
	close(m.done)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rooms {
		r.Close()
	}
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *Manager) cleanup() {
	m.mu.RLock()
	codes := make([]string, 0, len(m.rooms))
	for code := range m.rooms {
		codes = append(codes, code)
	}
	m.mu.RUnlock()

	for _, code := range codes {
		m.mu.RLock()
		r, exists := m.rooms[code]
		m.mu.RUnlock()
		if !exists {
			continue
		}

		r.CleanExpiredItems()

		// Atomically start a grace timer only if none is running
		if r.IsEmpty() && !r.Pinned {
			r.StartGraceTimerIfNone(m.config.GracePeriod, func() {
				if r.IsEmpty() {
					m.DeleteRoomIfMatch(code, r)
				}
			})
		}
	}
}
