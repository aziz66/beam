package relay

import (
	"encoding/json"
	"log"

	"github.com/beam-sh/beam/internal/protocol"
	"github.com/beam-sh/beam/internal/room"
)

type Relay struct{}

func New() *Relay {
	return &Relay{}
}

// Broadcast sends the envelope to all clients in the room except the sender.
func (rl *Relay) Broadcast(env *protocol.Envelope, sender *room.Client, rm *room.Room) {
	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("relay: marshal error: %v", err)
		return
	}

	if env.Type == protocol.TypeItem {
		rm.StoreItem(data, rm.DefaultTTL)
	}

	for _, c := range rm.GetClients() {
		if c.DeviceID == sender.DeviceID {
			continue
		}
		select {
		case c.Send <- data:
		default:
			log.Printf("relay: send buffer full for device=%s", c.DeviceID)
		}
	}
}

// Forward sends the envelope to a specific target device.
func (rl *Relay) Forward(env *protocol.Envelope, rm *room.Room) {
	if env.TargetID == "" {
		return
	}

	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("relay: marshal error: %v", err)
		return
	}

	c := rm.GetClient(env.TargetID)
	if c == nil {
		return
	}
	select {
	case c.Send <- data:
	default:
		log.Printf("relay: send buffer full for device=%s", env.TargetID)
	}
}
