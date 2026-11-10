// Package telegram provides the Telegram messaging adapter.
package telegram

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config contains the bot credentials and the users allowed to interact with it.
type Config struct {
	Token          string
	AllowedUserIDs []int64
}

// LoadConfig reads and validates the required environment variables.
func LoadConfig() (Config, error) {
	cfg := Config{Token: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))}
	for _, value := range strings.Split(os.Getenv("TELEGRAM_ALLOWED_USER_IDS"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || id <= 0 {
			return Config{}, fmt.Errorf("TELEGRAM_ALLOWED_USER_IDS must contain comma-separated positive user IDs")
		}
		cfg.AllowedUserIDs = append(cfg.AllowedUserIDs, id)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if len(c.AllowedUserIDs) == 0 {
		return fmt.Errorf("TELEGRAM_ALLOWED_USER_IDS must contain at least one positive user ID")
	}
	for _, id := range c.AllowedUserIDs {
		if id <= 0 {
			return fmt.Errorf("TELEGRAM_ALLOWED_USER_IDS must contain only positive user IDs")
		}
	}
	return nil
}
