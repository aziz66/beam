package protocol

import (
	"encoding/json"
	"time"
)

const (
	TypeJoin         = "join"
	TypeJoined       = "joined"
	TypeDeviceList   = "device_list"
	TypeItem         = "item"
	TypeFileMeta     = "file_meta"
	TypeFileChunk    = "file_chunk"
	TypeFileComplete = "file_complete"
	TypeFileCancel   = "file_cancel"
	TypeSignalOffer  = "signal_offer"
	TypeSignalAnswer = "signal_answer"
	TypeSignalICE    = "signal_ice"
	TypePing         = "ping"
	TypePong         = "pong"
	TypeError        = "error"
	TypeRoomInfo     = "room_info"
	TypeItemExpired  = "item_expired"
)

type Envelope struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	ID       string          `json:"id,omitempty"`
	Ts       int64           `json:"ts"`
	DeviceID string          `json:"device_id,omitempty"`
	TargetID string          `json:"target_id,omitempty"`
}

func NewEnvelope(msgType string, payload interface{}) (*Envelope, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Envelope{
		Type:    msgType,
		Payload: data,
		Ts:      time.Now().UnixMilli(),
	}, nil
}

func (e *Envelope) ParsePayload(v interface{}) error {
	return json.Unmarshal(e.Payload, v)
}

type JoinPayload struct {
	RoomCode    string `json:"room_code"`
	DeviceLabel string `json:"device_label"`
	Passphrase  string `json:"passphrase,omitempty"`
}

type JoinedPayload struct {
	RoomCode string `json:"room_code"`
	DeviceID string `json:"device_id"`
}

type DeviceInfo struct {
	DeviceID    string `json:"device_id"`
	DeviceLabel string `json:"device_label"`
	JoinedAt    int64  `json:"joined_at"`
}

type DeviceListPayload struct {
	Devices []DeviceInfo `json:"devices"`
}

type ItemPayload struct {
	ItemID        string `json:"item_id"`
	Kind          string `json:"kind"`
	EncryptedData string `json:"encrypted_data"`
	Nonce         string `json:"nonce"`
	TTL           int    `json:"ttl"`
	DeviceLabel   string `json:"device_label"`
}

type FileMetaPayload struct {
	FileID        string `json:"file_id"`
	EncryptedName string `json:"encrypted_name"`
	Nonce         string `json:"nonce"`
	Size          int64  `json:"size"`
	ChunkSize     int    `json:"chunk_size"`
	TotalChunks   int    `json:"total_chunks"`
	DeviceLabel   string `json:"device_label"`
}

type FileChunkPayload struct {
	FileID        string `json:"file_id"`
	Index         int    `json:"index"`
	EncryptedData string `json:"encrypted_data"`
	Nonce         string `json:"nonce"`
}

type FileCompletePayload struct {
	FileID string `json:"file_id"`
}

type FileCancelPayload struct {
	FileID string `json:"file_id"`
	Reason string `json:"reason,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RoomInfoPayload struct {
	RoomCode    string `json:"room_code"`
	DeviceCount int    `json:"device_count"`
	CreatedAt   int64  `json:"created_at"`
	Pinned      bool   `json:"pinned"`
}

type ItemExpiredPayload struct {
	ItemID string `json:"item_id"`
}

type SignalPayload struct {
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}
