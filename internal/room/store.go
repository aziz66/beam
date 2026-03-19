package room

import (
	"encoding/json"
	"log"
	"path/filepath"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

const roomKeyPrefix = "room:"

type RoomMetadata struct {
	Code       string        `json:"code"`
	Passphrase string        `json:"passphrase"` // bcrypt hash
	CreatedAt  time.Time     `json:"created_at"`
	DefaultTTL time.Duration `json:"default_ttl"`
	MaxSize    int           `json:"max_size"`
}

type Store struct {
	db   *badger.DB
	done chan struct{}
}

func NewStore(dataDir string) (*Store, error) {
	dbPath := filepath.Join(dataDir, "rooms.db")
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // suppress verbose badger logs

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	s := &Store{
		db:   db,
		done: make(chan struct{}),
	}
	go s.gcLoop()
	return s, nil
}

func (s *Store) SaveRoom(meta *RoomMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(roomKeyPrefix+meta.Code), data)
	})
}

func (s *Store) LoadRoom(code string) (*RoomMetadata, error) {
	var meta RoomMetadata
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(roomKeyPrefix + code))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &meta)
		})
	})
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Store) LoadAllRooms() ([]*RoomMetadata, error) {
	var rooms []*RoomMetadata
	err := s.db.View(func(txn *badger.Txn) error {
		prefix := []byte(roomKeyPrefix)
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var meta RoomMetadata
				if err := json.Unmarshal(val, &meta); err != nil {
					return err
				}
				rooms = append(rooms, &meta)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return rooms, err
}

func (s *Store) DeleteRoom(code string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(roomKeyPrefix + code))
	})
}

func (s *Store) Close() error {
	close(s.done)
	return s.db.Close()
}

func (s *Store) gcLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			err := s.db.RunValueLogGC(0.5)
			if err != nil && err != badger.ErrNoRewrite {
				log.Printf("badger gc: %v", err)
			}
		}
	}
}
