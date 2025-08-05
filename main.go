package main

import (
	"fmt"
	"log"
	"ndmBridge/nostr"
	"ndmBridge/utils"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	maxRetries = 5
	baseDelay  = 5 * time.Second
	maxDelay   = 5 * time.Minute
)

func main() {
	// Load configuration from config.yml
	config, err := utils.LoadConfig("config.yml")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	log.Println("Config loaded successfully")

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start the bot with retry logic
	go runBotWithRetry(config, stop)

	fmt.Println("Bot is starting with retry logic. Press CTRL+C to exit.")
	log.Println("Bot is starting with retry logic")

	// Wait for termination signal
	<-stop
	fmt.Println("Shutting down bot.")
	log.Println("Shutting down bot")
}

func runBotWithRetry(config *utils.Config, stop chan os.Signal) {
	retryCount := 0

	for {
		select {
		case <-stop:
			return
		default:
			// Try to start the bot
			err := startBot(config, stop)

			if err != nil {
				retryCount++
				delay := calculateBackoffDelay(retryCount)

				log.Printf("Bot connection failed (attempt %d/%d): %v", retryCount, maxRetries, err)

				if retryCount >= maxRetries {
					log.Printf("Max retries (%d) reached. Waiting %v before resetting counter...", maxRetries, maxDelay)
					time.Sleep(maxDelay)
					retryCount = 0 // Reset counter after max delay
				} else {
					log.Printf("Retrying in %v...", delay)
					time.Sleep(delay)
				}
			} else {
				// Connection was successful but later disconnected
				retryCount = 0 // Reset counter on successful connection
				log.Println("Bot disconnected. Attempting to reconnect...")
				time.Sleep(baseDelay)
			}
		}
	}
}

func startBot(config *utils.Config, stop chan os.Signal) error {
	// Create a new Discord session using the provided bot token
	dg, err := discordgo.New("Bot " + config.Discord.Token)
	if err != nil {
		return fmt.Errorf("error creating Discord session: %w", err)
	}

	log.Println("Discord session created successfully")

	// Add the message handler
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		log.Printf("New message received: %s", m.Content)
		messageCreateHandler(s, m, config)
	})

	// Add connection status handlers
	dg.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Bot connected as %s", r.User.Username)
	})

	dg.AddHandler(func(s *discordgo.Session, d *discordgo.Disconnect) {
		log.Println("Bot disconnected from Discord")
	})

	// Open a WebSocket connection to Discord
	err = dg.Open()
	if err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}
	defer dg.Close()

	log.Println("Bot is now running")

	// Wait for either a termination signal or connection issue
	select {
	case <-stop:
		return nil
	case <-time.After(1 * time.Hour): // Periodic reconnection check
		log.Println("Periodic reconnection cycle")
		return nil
	}
}

// calculateBackoffDelay implements exponential backoff with jitter
func calculateBackoffDelay(retryCount int) time.Duration {
	delay := baseDelay
	for i := 1; i < retryCount && delay < maxDelay; i++ {
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
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
