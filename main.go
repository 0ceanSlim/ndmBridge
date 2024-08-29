package main

import (
	"fmt"
	"log"
	"ndmBridge/nostr"
	"ndmBridge/utils"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

func main() {
	// Load configuration from config.yml
	config, err := utils.LoadConfig("config.yml")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	log.Println("Config loaded successfully")

	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + config.Discord.Token)
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
	}
	log.Println("Discord session created successfully")

	// Add the message handler
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		log.Printf("New message received: %s", m.Content)
		messageCreateHandler(s, m, config)
	})

	// Open a WebSocket connection to Discord
	err = dg.Open()
	if err != nil {
		log.Fatalf("Error opening connection: %v", err)
	}
	defer dg.Close()

	fmt.Println("Bot is now running. Press CTRL+C to exit.")
	log.Println("Bot is now running")

	// Wait for a termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("Shutting down bot.")
	log.Println("Shutting down bot")
}

// messageCreateHandler handles incoming Discord messages
func messageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate, config *utils.Config) {
	if m.Author.ID == s.State.User.ID {
		log.Println("Ignoring message from bot itself")
		return
	}

	if m.ChannelID == config.Discord.ChannelID {
		content := nostr.PrepareMessageContent(m)
		log.Printf("Prepared content for Nostr event: %s", content)

		event, err := nostr.CreateNostrEvent(content, config.Nostr.Pubkey)
		if err != nil {
			log.Printf("Error creating Nostr event: %v", err)
			return
		}
		log.Printf("Nostr event created: %+v", event)

		err = nostr.SignAndSendEvent(event, config.Nostr.PrivKey, config.Nostr.RelayURL)
		if err != nil {
			log.Printf("Error sending Nostr event: %v", err)
		} else {
			log.Println("Nostr event sent successfully")
		}
	}
}
