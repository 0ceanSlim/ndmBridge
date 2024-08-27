package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
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

	// Establish a WebSocket connection to the Nostr relay
	ws, _, err := websocket.DefaultDialer.Dial(config.Nostr.RelayURL, nil)
	if err != nil {
		log.Fatalf("Error connecting to Nostr relay: %v", err)
	}
	defer ws.Close()

	// Add the message handler, passing the WebSocket connection to it
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		messageCreateHandler(s, m, config.Discord.ChannelID, config, ws)
	})

	// Open a websocket connection to Discord
	err = dg.Open()
	if err != nil {
		log.Fatalf("Error opening connection: %v", err)
	}
	defer dg.Close()

	fmt.Println("Bot is now running. Press CTRL+C to exit.")

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
func messageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate, channelID string, config *Config, ws *websocket.Conn) {
	// Ignore messages from the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.ChannelID == channelID {
		// Create a new Nostr event
		event := NostrEvent{
			Pubkey:    config.Nostr.Pubkey,
			CreatedAt: time.Now().Unix(),
			Kind:      1, // Kind 1 for text note
			Content:   m.Content,
			Tags:      [][]string{}, // Empty tags array as required by NIP-01
		}

		// Serialize and compute ID
		eventStr, err := SerializeEventForID(event)
		if err != nil {
			fmt.Printf("Failed to serialize event for ID: %v\n", err)
			return
		}

		event.ID = ComputeEventID(eventStr)

		// Sign the event using Schnorr signature
		privKeyBytes, _ := hex.DecodeString(config.Nostr.PrivKey)
		privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
		event.Sig, err = SignEventSchnorr(event.ID, privKey)
		if err != nil {
			fmt.Printf("Failed to sign event: %v\n", err)
			return
		}

		// Send the event to Nostr relay
		err = SendEvent(ws, event)
		if err != nil {
			fmt.Printf("Failed to send event: %v\n", err)
		} else {
			fmt.Println("Event sent successfully.")
		}
	}
}

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

// SignEventSchnorr signs the event ID using Schnorr signatures
func SignEventSchnorr(eventID string, privKey *btcec.PrivateKey) (string, error) {
	idBytes, err := hex.DecodeString(eventID)
	if err != nil {
		return "", fmt.Errorf("failed to decode event ID: %w", err)
	}

	sig, err := schnorr.Sign(privKey, idBytes)
	if err != nil {
		return "", fmt.Errorf("failed to sign event with Schnorr: %w", err)
	}

	return hex.EncodeToString(sig.Serialize()), nil
}

// SendEvent sends the event to the Nostr relay via WebSocket
func SendEvent(ws *websocket.Conn, event NostrEvent) error {
	eventJSON, err := json.Marshal([]interface{}{"EVENT", event})
	if err != nil {
		return err
	}
	return ws.WriteMessage(websocket.TextMessage, eventJSON)
}
