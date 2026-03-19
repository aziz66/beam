package signaling

import (
	"encoding/json"
	"log"

	"github.com/beam-sh/beam/internal/protocol"
	"github.com/beam-sh/beam/internal/room"
)

// Signaling relays WebRTC SDP offers/answers and ICE candidates
// between clients. No state is maintained — it's pure message forwarding.
type Signaling struct{}

func New() *Signaling {
	return &Signaling{}
}

// Relay forwards a signaling message to the target device in the room.
// Returns false if the target was not found.
func (s *Signaling) Relay(env *protocol.Envelope, rm *room.Room) bool {
	if env.TargetID == "" {
		log.Printf("signaling: no target_id in %s message", env.Type)
		return false
	}

	target := rm.GetClient(env.TargetID)
	if target == nil {
		log.Printf("signaling: target %s not found in room %s", env.TargetID, rm.Code)
		return false
	}

	data, err := json.Marshal(env)
	if err != nil {
		log.Printf("signaling: marshal error: %v", err)
		return false
	}

	select {
	case target.Send <- data:
		return true
	default:
		log.Printf("signaling: send buffer full for target %s", env.TargetID)
		return false
	}
}

// IsSignalingType returns true if the message type is a WebRTC signaling message.
func IsSignalingType(msgType string) bool {
	switch msgType {
	case protocol.TypeSignalOffer, protocol.TypeSignalAnswer, protocol.TypeSignalICE:
		return true
	}
	return false
}
