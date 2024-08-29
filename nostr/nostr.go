package nostr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

// NostrEvent represents a Nostr event
type NostrEvent struct {
	ID        string     `json:"id"`
	Pubkey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// PrepareMessageContent prepares the message content by removing all mentions and appending attachment URLs
func PrepareMessageContent(m *discordgo.MessageCreate) string {
	content := m.Content

	// Remove channel mentions (e.g., <#1067205302946111602>)
	content = removeMentions(content, `<#[0-9]+>`)

	// Remove user mentions (e.g., <@UserID> or <@!UserID>)
	content = removeMentions(content, `<@!?[0-9]+>`)

	// Remove role mentions (e.g., <@&RoleID>)
	content = removeMentions(content, `<@&[0-9]+>`)

	for _, attachment := range m.Attachments {
		decodedURL := strings.ReplaceAll(attachment.URL, "\\u0026", "&")
		content += "\n" + decodedURL
	}

	log.Printf("Message content prepared after removing mentions: %s", content)
	return content
}

// removeMentions removes all matches of the given regex pattern from the content
func removeMentions(content string, pattern string) string {
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllString(content, "")
}

// CreateNostrEvent creates a Nostr event with the given content and public key
func CreateNostrEvent(content, pubkey string) (*NostrEvent, error) {
	event := &NostrEvent{
		Pubkey:    pubkey,
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Content:   content,
		Tags:      [][]string{},
	}

	eventStr, err := SerializeEventForID(*event)
	if err != nil {
		log.Printf("Error serializing event for ID: %v", err)
		return nil, fmt.Errorf("failed to serialize event for ID: %w", err)
	}

	event.ID = ComputeEventID(eventStr)
	log.Printf("Nostr event ID computed: %s", event.ID)

	return event, nil
}

// SerializeEventForID serializes the event into the format required by NIP-01 for ID computation
func SerializeEventForID(event NostrEvent) (string, error) {
	serializedEvent := []interface{}{
		0,
		event.Pubkey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}

	eventBytes, err := json.Marshal(serializedEvent)
	if err != nil {
		log.Printf("Error marshaling event: %v", err)
		return "", err
	}

	eventStr := string(eventBytes)
	eventStr = strings.ReplaceAll(eventStr, "\\u0026", "&")
	log.Printf("Serialized event string: %s", eventStr)

	return eventStr, nil
}

// ComputeEventID computes the ID for a given event
func ComputeEventID(serializedEvent string) string {
	hash := sha256.Sum256([]byte(serializedEvent))
	eventID := hex.EncodeToString(hash[:])
	log.Printf("Computed event ID: %s", eventID)
	return eventID
}

// SignAndSendEvent signs the event and sends it to the Nostr relay
func SignAndSendEvent(event *NostrEvent, privKeyHex, relayURL string) error {
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		log.Printf("Error decoding private key: %v", err)
		return fmt.Errorf("failed to decode private key: %w", err)
	}

	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	log.Println("Private key decoded successfully")

	sig, err := SignEventSchnorr(event.ID, privKey)
	if err != nil {
		log.Printf("Error signing event: %v", err)
		return fmt.Errorf("failed to sign event: %v", err)
	}
	event.Sig = sig
	log.Printf("Event signed with Schnorr signature: %s", event.Sig)

	return SendEvent(relayURL, *event)
}

// SignEventSchnorr signs the event ID using Schnorr signatures
func SignEventSchnorr(eventID string, privKey *btcec.PrivateKey) (string, error) {
	idBytes, err := hex.DecodeString(eventID)
	if err != nil {
		log.Printf("Error decoding event ID: %v", err)
		return "", fmt.Errorf("failed to decode event ID: %w", err)
	}

	sig, err := schnorr.Sign(privKey, idBytes)
	if err != nil {
		log.Printf("Error signing event with Schnorr: %v", err)
		return "", fmt.Errorf("failed to sign event with Schnorr: %w", err)
	}

	sigStr := hex.EncodeToString(sig.Serialize())
	log.Printf("Schnorr signature created: %s", sigStr)

	return sigStr, nil
}

// SendEvent sends the event to the Nostr relay via WebSocket and reads the server's response
func SendEvent(relayURL string, event NostrEvent) error {
	ws, _, err := websocket.DefaultDialer.Dial(relayURL, nil)
	if err != nil {
		log.Printf("Error connecting to Nostr relay: %v", err)
		return fmt.Errorf("error connecting to Nostr relay: %v", err)
	}
	defer ws.Close()
	log.Println("Connected to Nostr relay successfully")

	msg := []interface{}{"EVENT", event}
	eventJSON, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Error serializing event: %v", err)
		return fmt.Errorf("failed to serialize event: %v", err)
	}

	log.Printf("Sending event to relay: %s", eventJSON)
	err = ws.WriteMessage(websocket.TextMessage, eventJSON)
	if err != nil {
		log.Printf("Error sending event: %v", err)
		return fmt.Errorf("failed to send event: %v", err)
	}

	_, message, err := ws.ReadMessage()
	if err != nil {
		log.Printf("Error reading response from relay: %v", err)
		return fmt.Errorf("failed to read response from relay: %v", err)
	}

	log.Printf("Received response from relay: %s", string(message))

	return nil
}