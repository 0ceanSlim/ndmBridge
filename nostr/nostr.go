package nostr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gorilla/websocket"
)

type NostrEvent struct {
	ID        string     `json:"id"`
	Pubkey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// SerializeEventForID serializes the event into the format required by NIP-01 for ID computation
func SerializeEventForID(event NostrEvent) (string, error) {
	// The serialization format for ID calculation:
	// [0, <pubkey>, <created_at>, <kind>, <tags>, <content>]
	serializedEvent := []interface{}{
		0,
		event.Pubkey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}

	// Convert to JSON without any unnecessary formatting (minified)
	eventBytes, err := json.Marshal(serializedEvent)
	if err != nil {
		return "", err
	}

	return string(eventBytes), nil
}

// ComputeEventID computes the ID for a given event
func ComputeEventID(serializedEvent string) string {
	hash := sha256.Sum256([]byte(serializedEvent))
	return hex.EncodeToString(hash[:])
}

// SendEvent sends the event to the Nostr relay via WebSocket and reads the server's response
func SendEvent(ws *websocket.Conn, event NostrEvent) error {
	// Prepare the event in the required format: ["EVENT", <event object>]
	msg := []interface{}{"EVENT", event}

	// Serialize the message into JSON
	eventJSON, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize event: %v", err)
	}

	// Send the event to the relay
	err = ws.WriteMessage(websocket.TextMessage, eventJSON)
	if err != nil {
		return fmt.Errorf("failed to send event: %v", err)
	}

	// Read the response from the relay
	_, message, err := ws.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read response from relay: %v", err)
	}

	// Debugging: Print the response received from the relay
	fmt.Printf("Received response from relay: %s\n", string(message))

	return nil
}
