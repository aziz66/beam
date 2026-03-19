package room

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/beam-sh/beam/internal/config"
	"github.com/beam-sh/beam/internal/namegen"
	"golang.org/x/crypto/bcrypt"
)

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
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.rooms) >= m.config.MaxRooms {
		return nil, fmt.Errorf("maximum room limit reached")
	}

	// Generate unique code with retries
	var code string
	for i := 0; i < 10; i++ {
		candidate := namegen.Generate()
		if _, exists := m.rooms[candidate]; !exists {
			code = candidate
			break
		}
	}
	if code == "" {
		return nil, fmt.Errorf("failed to generate unique room code")
	}

	r := NewRoom(code, m.config.MaxRoomSize, m.config.DefaultTTL)

	if pinned && m.config.EnablePinnedRooms {
		r.Pinned = true
		if passphrase != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(passphrase), 12)
			if err != nil {
				return nil, fmt.Errorf("hashing passphrase: %w", err)
			}
			r.Passphrase = string(hash)
		}
		if m.store != nil {
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
		code := code // shadow loop variable for closure capture
		m.mu.RLock()
		r, exists := m.rooms[code]
		m.mu.RUnlock()
		if !exists {
			continue
		}

		r.CleanExpiredItems()

		// Only start a grace timer if one isn't already running (hub may have started one)
		if r.IsEmpty() && !r.Pinned && !r.HasGraceTimer() {
			r.StartGraceTimer(m.config.GracePeriod, func() {
				if r.IsEmpty() {
					m.DeleteRoom(code)
				}
			})
		}
	}
}
