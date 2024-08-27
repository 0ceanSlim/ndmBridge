package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v2"
)

// Config structure to hold the data from config.yml
type Config struct {
	Discord struct {
		Token     string `yaml:"token"`
		ChannelID string `yaml:"channel_id"`
	} `yaml:"discord"`
	Nostr struct {
		Pubkey   string `yaml:"pubkey"`
		PrivKey  string `yaml:"privkey"`
		RelayURL string `yaml:"relay_url"`
	} `yaml:"nostr"`
}

func main() {
	// Load configuration from config.yml
	config, err := loadConfig("config.yml")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + config.Discord.Token)
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
	}

	// Add the message handler
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		messageCreateHandler(s, m, config.Discord.ChannelID, config)
	})

	// Open a websocket connection to Discord
	err = dg.Open()
	if err != nil {
		log.Fatalf("Error opening connection: %v", err)
	}
	defer dg.Close()

	fmt.Println("Bot is now running. Press CTRL+C to exit.")

	// Establish a WebSocket connection to the Nostr relay
	ws, _, err := websocket.DefaultDialer.Dial(config.Nostr.RelayURL, nil)
	if err != nil {
		log.Fatalf("Error connecting to Nostr relay: %v", err)
	}
	defer ws.Close()

	// Wait for a termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("Shutting down bot.")
}

// loadConfig reads and parses the configuration file
func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal config data: %w", err)
	}

	// Validate that necessary fields are not empty
	if config.Discord.Token == "" || config.Discord.ChannelID == "" ||
		config.Nostr.Pubkey == "" || config.Nostr.PrivKey == "" || config.Nostr.RelayURL == "" {
		return nil, fmt.Errorf("all fields in config.yml must be provided")
	}

	return &config, nil
}

// messageCreateHandler handles incoming Discord messages
func messageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate, channelID string, config *Config) {
	// Ignore messages from the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Debug: Print the entire message object in JSON format
	messageJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Printf("Failed to marshal message object: %v\n", err)
		return
	}

	fmt.Println("----- New Message Received -----")
	fmt.Printf("Full Message Object:\n%s\n", string(messageJSON))

	if m.ChannelID == channelID {
		// Create a new Nostr event
		event := NostrEvent{
			Pubkey:    config.Nostr.Pubkey,
			CreatedAt: time.Now().Unix(),
			Kind:      1, // Kind 1 for text note
			Content:   m.Content,
			Tags:      []string{}, // No tags for now
		}

		// Serialize and compute ID
		eventStr, _ := SerializeEvent(event)
		event.ID = ComputeEventID(eventStr)

		// Sign the event
		privKeyBytes, _ := hex.DecodeString(config.Nostr.PrivKey)
		privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
		event.Sig, _ = SignEvent(eventStr, privKey)

		// Send the event to Nostr relay
		ws, _, _ := websocket.DefaultDialer.Dial(config.Nostr.RelayURL, nil)
		defer ws.Close()
		err := SendEvent(ws, event)
		if err != nil {
			fmt.Printf("Failed to send event: %v\n", err)
		}
	}
}

type NostrEvent struct {
	ID        string   `json:"id"`
	Pubkey    string   `json:"pubkey"`
	CreatedAt int64    `json:"created_at"`
	Kind      int      `json:"kind"`
	Tags      []string `json:"tags"`
	Content   string   `json:"content"`
	Sig       string   `json:"sig"`
}

// SerializeEvent converts an NostrEvent to JSON
func SerializeEvent(event NostrEvent) (string, error) {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	// Convert JSON to string and escape special characters
	eventStr := string(eventJSON)
	eventStr = strings.ReplaceAll(eventStr, "\n", "\\n")
	eventStr = strings.ReplaceAll(eventStr, "\"", "\\\"")
	eventStr = strings.ReplaceAll(eventStr, "\\", "\\\\")
	eventStr = strings.ReplaceAll(eventStr, "\r", "\\r")
	eventStr = strings.ReplaceAll(eventStr, "\t", "\\t")
	eventStr = strings.ReplaceAll(eventStr, "\b", "\\b")
	eventStr = strings.ReplaceAll(eventStr, "\f", "\\f")

	return eventStr, nil
}

// ComputeEventID computes the ID for a given event
func ComputeEventID(eventStr string) string {
	hash := sha256.Sum256([]byte(eventStr))
	return hex.EncodeToString(hash[:])
}

// SignEvent signs the event with the private key
func SignEvent(eventStr string, privKey *btcec.PrivateKey) (string, error) {
	hash := sha256.Sum256([]byte(eventStr))

	r, s, err := ecdsa.Sign(rand.Reader, privKey.ToECDSA(), hash[:])
	if err != nil {
		return "", err
	}

	signature := append(r.Bytes(), s.Bytes()...)
	return hex.EncodeToString(signature), nil
}

// SendEvent sends the event to the Nostr relay via WebSocket
func SendEvent(ws *websocket.Conn, event NostrEvent) error {
	eventJSON, err := json.Marshal([]interface{}{"EVENT", event})
	if err != nil {
		return err
	}
	return ws.WriteMessage(websocket.TextMessage, eventJSON)
}
