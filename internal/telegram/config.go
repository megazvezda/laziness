// Package telegram provides the Telegram messaging adapter.
package telegram

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/lpernett/godotenv"
)

// Config contains the bot credentials and the users allowed to interact with it.
type Config struct {
	Token            string
	AllowedUserIDs   []int64
	AllowedGroupsIDs []int64
}

// LoadConfig reads and validates the required environment variables.
func LoadConfig() (Config, error) {
	if err := godotenv.Load("internal/telegram/.env"); err != nil {
		return Config{}, fmt.Errorf("loading .env file: %w", err)
	}
	if strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")) == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	cfg := Config{Token: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))}

	for _, value := range strings.Split(os.Getenv("TELEGRAM_ALLOWED_USER_IDS"), ",") {
		if value == "" {
			slog.Info("no allowed user ids provided")
			break
		}
		id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		cfg.AllowedUserIDs = append(cfg.AllowedUserIDs, id)
	}
	for _, value := range strings.Split(os.Getenv("TELEGRAM_ALLOWED_GROUP_IDS"), ",") {
		id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if id == 0 {
			slog.Info("no allowed group ids provided")
			break
		}
		cfg.AllowedGroupsIDs = append(cfg.AllowedGroupsIDs, id)
	}
	return cfg, nil
}
