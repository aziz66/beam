package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/aziz66/beam/internal/protocol"
)

const chunkSize = 64 * 1024 // 64KB

// maxTotalChunks caps file transfers at ~1 GB (16384 × 64 KB).
// This prevents a malicious peer from allocating unbounded memory in the
// receiver by claiming a huge totalChunks value.
const maxTotalChunks = 16384

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	switch cmd {
	case "send":
		cmdSend()
	case "receive":
		cmdReceive()
	case "new":
		cmdNew()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: beam <command> [options]

Commands:
  send      Send a file or text to a room
  receive   Watch a room and download files
  new       Create a new room

Examples:
  beam send ./report.pdf --room coral-tiger-88 --key <base64key> --server http://localhost:8080
  echo "hello" | beam send --room coral-tiger-88 --key <base64key>
  beam receive --room coral-tiger-88 --key <base64key> --server http://localhost:8080
  beam new --server http://localhost:8080
`)
}

func cmdSend() {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	room := fs.String("room", "", "Room code")
	key := fs.String("key", "", "Encryption key (base64)")
	server := fs.String("server", "http://localhost:8080", "Beam server URL")
	clipboard := fs.Bool("clipboard", false, "Send clipboard content (stdin)")
	fs.Parse(os.Args[1:])

	if *room == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "error: --room and --key are required")
		os.Exit(1)
	}

	conn := connectWS(*server, *room)
	defer conn.Close()

	waitForJoined(conn)

	args := fs.Args()
	if len(args) > 0 && !*clipboard {
		sendFileCLI(conn, args[0], *key)
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("reading stdin: %v", err)
		}
		sendTextCLI(conn, string(data), *key)
	}

	// Send a clean WebSocket close and drain until the server echoes it back.
	// This is more reliable than a fixed sleep for ensuring the message is
	// flushed through the server before the process exits.
	conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(2*time.Second),
	)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	fmt.Println("sent!")
}

func cmdReceive() {
	fs := flag.NewFlagSet("receive", flag.ExitOnError)
	room := fs.String("room", "", "Room code")
	key := fs.String("key", "", "Encryption key (base64)")
	server := fs.String("server", "http://localhost:8080", "Beam server URL")
	outDir := fs.String("out", ".", "Output directory for files")
	fs.Parse(os.Args[1:])

	if *room == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "error: --room and --key are required")
		os.Exit(1)
	}

	conn := connectWS(*server, *room)
	defer conn.Close()

	waitForJoined(conn)
	fmt.Printf("watching room %s... (Ctrl+C to stop)\n", *room)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	transfers := make(map[string]*fileTransfer)

	go func() {
		<-interrupt
		fmt.Println("\nbye!")
		conn.Close()
		os.Exit(0)
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Println("\nroom closed")
				return
			}
			log.Fatalf("read error: %v", err)
		}

		var env protocol.Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			continue
		}

		switch env.Type {
		case protocol.TypeItem:
			var payload protocol.ItemPayload
			if err := env.ParsePayload(&payload); err != nil {
				continue
			}
			text, err := decryptString(payload.EncryptedData, payload.Nonce, *key)
			if err != nil {
				fmt.Printf("[%s] (decryption failed)\n", payload.Kind)
				continue
			}
			fmt.Printf("[%s] %s\n", payload.Kind, text)

		case protocol.TypeFileMeta:
			// Prune transfers older than 5 minutes
			for id, ft := range transfers {
				if time.Since(ft.startedAt) > 5*time.Minute {
					fmt.Printf("\n  transfer %s timed out, discarding\n", id)
					delete(transfers, id)
				}
			}
			// Enforce max concurrent transfers
			if len(transfers) >= 50 {
				fmt.Printf("  too many concurrent transfers, skipping\n")
				continue
			}
			var payload protocol.FileMetaPayload
			if err := env.ParsePayload(&payload); err != nil {
				continue
			}
			if payload.TotalChunks == 0 {
				fmt.Printf("  skipping empty file transfer\n")
				continue
			}
			if payload.TotalChunks > maxTotalChunks {
				fmt.Printf("  transfer claims %d chunks (max %d), skipping\n", payload.TotalChunks, maxTotalChunks)
				continue
			}
			name, err := decryptString(payload.EncryptedName, payload.Nonce, *key)
			if err != nil {
				name = "unknown-file"
			}
			transfers[payload.FileID] = &fileTransfer{
				name:        name,
				size:        payload.Size,
				totalChunks: payload.TotalChunks,
				chunks:      make(map[int][]byte),
				startedAt:   time.Now(),
			}
			fmt.Printf("receiving file: %s (%d bytes)\n", name, payload.Size)

		case protocol.TypeFileChunk:
			var payload protocol.FileChunkPayload
			if err := env.ParsePayload(&payload); err != nil {
				continue
			}
			ft, ok := transfers[payload.FileID]
			if !ok {
				continue
			}
			if payload.Index < 0 || payload.Index >= ft.totalChunks {
				fmt.Printf("  chunk index %d out of range (expected 0–%d), skipping\n", payload.Index, ft.totalChunks-1)
				continue
			}
			data, err := decryptRawBytes(payload.EncryptedData, payload.Nonce, *key)
			if err != nil {
				fmt.Printf("  chunk %d decrypt failed\n", payload.Index)
				continue
			}
			ft.chunks[payload.Index] = data
			pct := len(ft.chunks) * 100 / ft.totalChunks
			fmt.Printf("\r  %s %s %d%%", ft.name, progressBar(pct), pct)

		case protocol.TypeFileComplete:
			var payload protocol.FileCompletePayload
			if err := env.ParsePayload(&payload); err != nil {
				continue
			}
			ft, ok := transfers[payload.FileID]
			if !ok {
				continue
			}
			fmt.Println()

			// Strip any path components from the received filename
			safeName := filepath.Base(ft.name)
			if safeName == "" || safeName == "." || safeName == ".." {
				fmt.Printf("  skipping file with invalid name\n")
				delete(transfers, payload.FileID)
				continue
			}
			outPath := filepath.Join(*outDir, safeName)
			// Ensure the resolved path is within the output directory
			cleanOut := filepath.Clean(*outDir)
			if !strings.HasPrefix(filepath.Clean(outPath), cleanOut+string(os.PathSeparator)) &&
				filepath.Clean(outPath) != cleanOut {
				fmt.Printf("  skipping file with unsafe path: %s\n", ft.name)
				delete(transfers, payload.FileID)
				continue
			}
			// Verify all chunks arrived before writing
			allChunks := true
			for i := 0; i < ft.totalChunks; i++ {
				if _, ok := ft.chunks[i]; !ok {
					fmt.Printf("  missing chunk %d — file incomplete, skipping\n", i)
					allChunks = false
					break
				}
			}
			if !allChunks {
				delete(transfers, payload.FileID)
				continue
			}
			tmpPath := outPath + ".beam-tmp"
			f, err := os.Create(tmpPath)
			if err != nil {
				fmt.Printf("  error creating file: %v\n", err)
				delete(transfers, payload.FileID)
				continue
			}
			writeErr := false
			for i := 0; i < ft.totalChunks; i++ {
				if _, err := f.Write(ft.chunks[i]); err != nil {
					fmt.Printf("  error writing chunk %d: %v\n", i, err)
					writeErr = true
					break
				}
			}
			f.Close()
			if writeErr {
				os.Remove(tmpPath)
				delete(transfers, payload.FileID)
				continue
			}
			if err := os.Rename(tmpPath, outPath); err != nil {
				fmt.Printf("  error saving file: %v\n", err)
				os.Remove(tmpPath)
				delete(transfers, payload.FileID)
				continue
			}
			delete(transfers, payload.FileID)
			fmt.Printf("  saved: %s\n", outPath)

		case protocol.TypeDeviceList:
			var payload protocol.DeviceListPayload
			if err := env.ParsePayload(&payload); err != nil {
				continue
			}
			fmt.Printf("devices: %d connected\n", len(payload.Devices))
		}
	}
}

func cmdNew() {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	server := fs.String("server", "http://localhost:8080", "Beam server URL")
	pinned := fs.Bool("pinned", false, "Create a pinned room")
	passphrase := fs.String("passphrase", "", "Passphrase for pinned room")
	fs.Parse(os.Args[1:])

	// Generate encryption key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		log.Fatalf("generating key: %v", err)
	}
	keyB64 := base64.StdEncoding.EncodeToString(keyBytes)

	// Create room via API
	type createReq struct {
		Pinned     bool   `json:"pinned"`
		Passphrase string `json:"passphrase,omitempty"`
	}
	bodyBytes, err := json.Marshal(createReq{Pinned: *pinned, Passphrase: *passphrase})
	if err != nil {
		log.Fatalf("marshaling request: %v", err)
	}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Post(strings.TrimRight(*server, "/")+"/api/rooms", "application/json", strings.NewReader(string(bodyBytes)))
	if err != nil {
		log.Fatalf("creating room: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("reading response: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		log.Fatalf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		RoomCode string `json:"room_code"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Fatalf("parsing response: %v", err)
	}

	link := fmt.Sprintf("%s/r/%s#%s", strings.TrimRight(*server, "/"), result.RoomCode, keyB64)
	fmt.Printf("Room: %s\n", result.RoomCode)
	fmt.Printf("Key:  %s\n", keyB64)
	fmt.Printf("Link: %s\n", link)
	fmt.Println()
	fmt.Println("Share the link above. The encryption key (after #) never reaches the server.")
}

// WebSocket helpers

func connectWS(server, roomCode string) *websocket.Conn {
	u, err := url.Parse(server)
	if err != nil {
		log.Fatalf("invalid server URL: %v", err)
	}

	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/ws/%s", scheme, u.Host, roomCode)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Fatalf("websocket connect failed: %v", err)
	}
	return conn
}

func waitForJoined(conn *websocket.Conn) {
	join := map[string]interface{}{
		"type":    "join",
		"payload": map[string]string{"device_label": "Beam CLI"},
		"ts":      time.Now().UnixMilli(),
	}
	data, err := json.Marshal(join)
	if err != nil {
		log.Fatalf("marshal join: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Fatalf("send join: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Fatalf("waiting for joined: %v", err)
		}
		var env protocol.Envelope
		if json.Unmarshal(msg, &env) == nil {
			if env.Type == protocol.TypeJoined {
				conn.SetReadDeadline(time.Time{}) // clear deadline
				return
			}
			if env.Type == protocol.TypeError {
				var ep protocol.ErrorPayload
				env.ParsePayload(&ep)
				log.Fatalf("server error: %s", ep.Message)
			}
		}
	}
}

func sendTextCLI(conn *websocket.Conn, text, keyB64 string) {
	encrypted, nonce, err := encryptData([]byte(text), keyB64)
	if err != nil {
		log.Fatalf("encryption failed: %v", err)
	}

	env := map[string]interface{}{
		"type": "item",
		"payload": map[string]interface{}{
			"item_id":        fmt.Sprintf("cli-%d", time.Now().UnixNano()),
			"kind":           "text",
			"encrypted_data": encrypted,
			"nonce":          nonce,
			"ttl":            1800,
			"device_label":   "Beam CLI",
		},
		"ts": time.Now().UnixMilli(),
	}
	data, err2 := json.Marshal(env)
	if err2 != nil {
		log.Fatalf("marshal text item: %v", err2)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Fatalf("send text: %v", err)
	}
}

func sendFileCLI(conn *websocket.Conn, filePath, keyB64 string) {
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("opening file: %v", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		log.Fatalf("stat file: %v", err)
	}

	fileName := filepath.Base(filePath)
	fileSize := stat.Size()
	if fileSize == 0 {
		log.Fatalf("cannot send empty file: %s", filePath)
	}
	totalChunks := int((fileSize + chunkSize - 1) / chunkSize)
	fileID := fmt.Sprintf("cli-%d", time.Now().UnixNano())

	encName, nameNonce, err := encryptData([]byte(fileName), keyB64)
	if err != nil {
		log.Fatalf("encrypting filename: %v", err)
	}

	meta := map[string]interface{}{
		"type": "file_meta",
		"payload": map[string]interface{}{
			"file_id":        fileID,
			"encrypted_name": encName,
			"nonce":          nameNonce,
			"size":           fileSize,
			"chunk_size":     chunkSize,
			"total_chunks":   totalChunks,
			"device_label":   "Beam CLI",
		},
		"ts": time.Now().UnixMilli(),
	}
	data, err := json.Marshal(meta)
	if err != nil {
		log.Fatalf("marshal file_meta: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Fatalf("send file_meta: %v", err)
	}

	buf := make([]byte, chunkSize)
	for i := 0; i < totalChunks; i++ {
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			log.Fatalf("reading file: %v", err)
		}

		encData, chunkNonce, err := encryptData(buf[:n], keyB64)
		if err != nil {
			log.Fatalf("encrypting chunk: %v", err)
		}

		chunk := map[string]interface{}{
			"type": "file_chunk",
			"payload": map[string]interface{}{
				"file_id":        fileID,
				"index":          i,
				"encrypted_data": encData,
				"nonce":          chunkNonce,
			},
			"ts": time.Now().UnixMilli(),
		}
		cdata, err := json.Marshal(chunk)
		if err != nil {
			log.Fatalf("marshal chunk %d: %v", i, err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, cdata); err != nil {
			log.Fatalf("send chunk %d: %v", i, err)
		}

		pct := (i + 1) * 100 / totalChunks
		fmt.Printf("\r%s %s %d%%", fileName, progressBar(pct), pct)
	}
	fmt.Println()

	complete := map[string]interface{}{
		"type":    "file_complete",
		"payload": map[string]string{"file_id": fileID},
		"ts":      time.Now().UnixMilli(),
	}
	cdata, err := json.Marshal(complete)
	if err != nil {
		log.Fatalf("marshal file_complete: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, cdata); err != nil {
		log.Fatalf("send file_complete: %v", err)
	}
}

// Crypto helpers

func encryptData(plaintext []byte, keyB64 string) (encB64, nonceB64 string, err error) {
	keyBytes, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return "", "", err
	}
	if len(keyBytes) != 32 {
		return "", "", fmt.Errorf("key must be 32 bytes, got %d", len(keyBytes))
	}
	var key [32]byte
	copy(key[:], keyBytes)

	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", "", err
	}

	encrypted := secretbox.Seal(nil, plaintext, &nonce, &key)
	return base64.StdEncoding.EncodeToString(encrypted), base64.StdEncoding.EncodeToString(nonce[:]), nil
}

func decryptString(encB64, nonceB64, keyB64 string) (string, error) {
	data, err := decryptRawBytes(encB64, nonceB64, keyB64)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decryptRawBytes(encB64, nonceB64, keyB64 string) ([]byte, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, err
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(keyBytes))
	}
	var key [32]byte
	copy(key[:], keyBytes)

	nonceBytes, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return nil, err
	}
	if len(nonceBytes) != 24 {
		return nil, fmt.Errorf("nonce must be 24 bytes, got %d", len(nonceBytes))
	}
	var nonce [24]byte
	copy(nonce[:], nonceBytes)

	encrypted, err := base64.StdEncoding.DecodeString(encB64)
	if err != nil {
		return nil, err
	}

	decrypted, ok := secretbox.Open(nil, encrypted, &nonce, &key)
	if !ok {
		return nil, fmt.Errorf("decryption failed")
	}
	return decrypted, nil
}

// Helpers

type fileTransfer struct {
	name        string
	size        int64
	totalChunks int
	chunks      map[int][]byte
	startedAt   time.Time
}

func progressBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct / 5
	empty := 20 - filled
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", empty) + "]"
}
