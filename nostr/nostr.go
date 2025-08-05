package nostr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
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

const (
	ox0URL      = "https://0x0.st"
	maxFileSize = 512 * 1024 * 1024 // 512 MiB as per 0x0.st limits
	httpTimeout = 30 * time.Second
)

// PrepareMessageContent prepares the message content by removing mentions and uploading attachments to 0x0.st
func PrepareMessageContent(m *discordgo.MessageCreate) string {
	content := m.Content

	// Remove channel mentions (e.g., <#1067205302946111602>)
	content = removeMentions(content, `<#[0-9]+>`)

	// Remove user mentions (e.g., <@UserID> or <@!UserID>)
	content = removeMentions(content, `<@!?[0-9]+>`)

	// Remove role mentions (e.g., <@&RoleID>)
	content = removeMentions(content, `<@&[0-9]+>`)

	// Process attachments and upload to 0x0.st
	for _, attachment := range m.Attachments {
		decodedURL := strings.ReplaceAll(attachment.URL, "\\u0026", "&")

		// Try to upload to 0x0.st with retry logic
		ox0URL, err := uploadToOx0WithRetry(decodedURL, attachment.Filename, 3)
		if err != nil {
			log.Printf("Failed to upload %s to 0x0.st: %v, using original URL", attachment.Filename, err)
			content += "\n" + decodedURL
		} else {
			log.Printf("Successfully uploaded %s to 0x0.st: %s", attachment.Filename, ox0URL)
			content += "\n" + ox0URL
		}
	}

	log.Printf("Message content prepared: %s", content)
	return content
}

// uploadToOx0 uploads a file from a URL to 0x0.st and returns the new URL
func uploadToOx0(fileURL, filename string) (string, error) {
	log.Printf("Attempting to upload %s to 0x0.st", filename)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: httpTimeout,
	}

	// Prepare the multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the URL field (0x0.st can fetch from remote URLs)
	err := writer.WriteField("url", fileURL)
	if err != nil {
		return "", fmt.Errorf("failed to write url field: %w", err)
	}

	// Add secret field for hard-to-guess URLs
	err = writer.WriteField("secret", "")
	if err != nil {
		return "", fmt.Errorf("failed to write secret field: %w", err)
	}

	// Set expiration to maximum (8760 hours = 1 year)
	err = writer.WriteField("expires", "8760")
	if err != nil {
		return "", fmt.Errorf("failed to write expires field: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create the request
	req, err := http.NewRequest("POST", ox0URL, &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "ndmBridge/1.0")

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("0x0.st returned status %d: %s", resp.StatusCode, string(body))
	}

	// The response should be the URL
	ox0ResponseURL := strings.TrimSpace(string(body))

	// Validate the response looks like a URL
	if !strings.HasPrefix(ox0ResponseURL, "https://0x0.st/") {
		return "", fmt.Errorf("unexpected response from 0x0.st: %s", ox0ResponseURL)
	}

	// Optionally append the original filename for better presentation
	if filename != "" {
		ox0ResponseURL = fmt.Sprintf("%s/%s", ox0ResponseURL, filename)
	}

	return ox0ResponseURL, nil
}

// uploadToOx0WithRetry uploads a file to 0x0.st with retry logic
func uploadToOx0WithRetry(fileURL, filename string, maxRetries int) (string, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		url, err := uploadToOx0(fileURL, filename)
		if err == nil {
			return url, nil
		}

		lastErr = err
		log.Printf("Attempt %d failed to upload %s to 0x0.st: %v", attempt, filename, err)

		if attempt < maxRetries {
			delay := time.Duration(attempt) * 2 * time.Second
			log.Printf("Retrying upload in %v...", delay)
			time.Sleep(delay)
		}
	}

	return "", fmt.Errorf("failed to upload %s after %d attempts: %w", filename, maxRetries, lastErr)
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

	return SendEventWithRetry(relayURL, *event, 3)
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

// SendEventWithRetry sends the event to the Nostr relay with retry logic
func SendEventWithRetry(relayURL string, event NostrEvent, maxRetries int) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := SendEvent(relayURL, event)
		if err == nil {
			log.Printf("Event sent successfully on attempt %d", attempt)
			return nil
		}

		lastErr = err
		log.Printf("Attempt %d failed to send event: %v", attempt, err)

		if attempt < maxRetries {
			delay := time.Duration(attempt) * 2 * time.Second
			log.Printf("Retrying in %v...", delay)
			time.Sleep(delay)
		}
	}

	return fmt.Errorf("failed to send event after %d attempts: %w", maxRetries, lastErr)
}

// SendEvent sends the event to the Nostr relay via WebSocket and reads the server's response
func SendEvent(relayURL string, event NostrEvent) error {
	// Set a connection timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	ws, _, err := dialer.Dial(relayURL, nil)
	if err != nil {
		log.Printf("Error connecting to Nostr relay: %v", err)
		return fmt.Errorf("error connecting to Nostr relay: %v", err)
	}
	defer ws.Close()

	// Set write deadline
	ws.SetWriteDeadline(time.Now().Add(10 * time.Second))

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

	// Set read deadline
	ws.SetReadDeadline(time.Now().Add(10 * time.Second))

	_, message, err := ws.ReadMessage()
	if err != nil {
		log.Printf("Error reading response from relay: %v", err)
		return fmt.Errorf("failed to read response from relay: %v", err)
	}

	log.Printf("Received response from relay: %s", string(message))
	return nil
}
