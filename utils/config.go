package utils

import (
	"fmt"
	"os"

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

// loadConfig reads and parses the configuration file
func LoadConfig(filename string) (*Config, error) {
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