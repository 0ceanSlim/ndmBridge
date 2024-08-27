package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"ndmBridge/nostr"
	"ndmBridge/utils"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

func main() {
	// Load configuration from config.yml
	config, err := utils.LoadConfig("config.yml")
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

	// Open a WebSocket connection to Discord
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

// messageCreateHandler handles incoming Discord messages
func messageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate, channelID string, config *utils.Config) {
	// Ignore messages from the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.ChannelID == channelID {
		// Start with the original message content
		content := m.Content

		// Append any attachment URLs to the content, ensuring they are correctly decoded
		for _, attachment := range m.Attachments {
			decodedURL := strings.ReplaceAll(attachment.URL, "\\u0026", "&")
			content += "\n" + decodedURL
		}

		// Create a new Nostr event
		event := nostr.NostrEvent{
			Pubkey:    config.Nostr.Pubkey,
			CreatedAt: time.Now().Unix(),
			Kind:      1,
			Content:   content, // Message content including URLs
			Tags:      [][]string{}, // Empty tags array as required by NIP-01
		}

		// Serialize and compute ID
		eventStr, err := nostr.SerializeEventForID(event)
		if err != nil {
			fmt.Printf("Failed to serialize event for ID: %v\n", err)
			return
		}

		event.ID = nostr.ComputeEventID(eventStr)

		// Sign the event using Schnorr signature
		privKeyBytes, _ := hex.DecodeString(config.Nostr.PrivKey)
		privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
		event.Sig, err = SignEventSchnorr(event.ID, privKey)
		if err != nil {
			fmt.Printf("Failed to sign event: %v\n", err)
			return
		}

		// Establish a WebSocket connection to the Nostr relay, send the event, and then close the connection
		ws, _, err := websocket.DefaultDialer.Dial(config.Nostr.RelayURL, nil)
		if err != nil {
			fmt.Printf("Error connecting to Nostr relay: %v\n", err)
			return
		}
		defer ws.Close()

		// Send the event to Nostr relay and read the response
		err = nostr.SendEvent(ws, event)
		if err != nil {
			fmt.Printf("Failed to send event: %v\n", err)
		} else {
			fmt.Println("Event sent successfully.")
		}
	}
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
