package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/megazvezda/laziness/internal/telegram"
	"github.com/megazvezda/laziness/storage"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	db, err := storage.Open(context.Background(), "db/laziness.sqlite")
	if err != nil {
		logger.Error("Could not initialize database", "error", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("Could not close database", "error", err)
		}
	}()

	cfg, err := telegram.LoadConfig()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		return 1
	}
	b, err := telegram.New(cfg, logger)
	if err != nil {
		logger.Error("Could not create Telegram bot", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := b.Run(ctx); err != nil {
		logger.Error("Telegram bot failed", "error", err)
		return 1
	}
	return 0
}
