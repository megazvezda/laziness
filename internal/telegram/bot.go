package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const requestTimeout = 10 * time.Second

const helpText = "Available commands:\n/start - Welcome and available commands\n/help - Show this help\n/ping - Check that the bot is responding"

// messageSender is the small part of the Telegram client that handlers need.
type messageSender interface {
	SendMessage(context.Context, *bot.SendMessageParams) (*models.Message, error)
}

// Bot handles private messages from an explicit set of Telegram users.
type Bot struct {
	client       *bot.Bot
	sender       messageSender
	allowedUsers map[int64]struct{}
	logger       *slog.Logger
}

// New constructs a bot without making network requests. A nil logger uses slog.Default.
func New(cfg Config, logger *slog.Logger) (*Bot, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bot{
		allowedUsers: make(map[int64]struct{}, len(cfg.AllowedUserIDs)),
		logger:       logger,
	}
	for _, id := range cfg.AllowedUserIDs {
		b.allowedUsers[id] = struct{}{}
	}
	client, err := bot.New(strings.TrimSpace(cfg.Token),
		// Authenticate in Run so startup requests share the shutdown context.
		bot.WithSkipGetMe(),
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message"}),
		bot.WithNotAsyncHandlers(),
		bot.WithMiddlewares(b.authorize),
		bot.WithDefaultHandler(b.unknownMessage),
		bot.WithErrorsHandler(func(err error) {
			if !errors.Is(err, context.Canceled) {
				logger.Error("Telegram update processing failed", "error_kind", errorKind(err))
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create Telegram client: %s", errorKind(err))
	}
	b.client = client
	b.sender = client
	return b, nil
}

// Run validates the bot token and polls until ctx is canceled. Call it once per Bot.
func (b *Bot) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}
	initCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	me, err := b.client.GetMe(initCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("initialize Telegram bot (%s): check bot token and connectivity", errorKind(err))
	}
	if me.Username == "" {
		return fmt.Errorf("initialize Telegram bot: Telegram returned no bot username")
	}
	b.registerCommands(me.Username)
	b.logger.InfoContext(ctx, "Telegram bot started", "transport", "long_polling")
	b.client.Start(ctx)
	b.logger.InfoContext(ctx, "Telegram bot stopped")
	return nil
}

func (b *Bot) registerCommands(username string) {
	commands := []struct {
		name    string
		handler bot.HandlerFunc
	}{
		{"start", b.start},
		{"help", b.help},
		{"ping", b.ping},
	}
	for _, command := range commands {
		b.client.RegisterHandler(bot.HandlerTypeMessageText, command.name, bot.MatchTypeCommandStartOnly, command.handler)
		b.client.RegisterHandler(bot.HandlerTypeMessageText, command.name+"@"+username, bot.MatchTypeCommandStartOnly, command.handler)
	}
}

func (b *Bot) authorize(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, client *bot.Bot, update *models.Update) {
		if ctx.Err() != nil || update == nil || update.Message == nil {
			return
		}
		message := update.Message
		if message.From == nil || message.Chat.Type != models.ChatTypePrivate || message.Text == "" {
			return
		}
		if _, ok := b.allowedUsers[message.From.ID]; !ok {
			return
		}
		next(ctx, client, update)
	}
}

func (b *Bot) start(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.reply(ctx, update.Message.Chat.ID, "Welcome to Laziness! This bot is an early skeleton.\n\n"+helpText)
}

func (b *Bot) help(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.reply(ctx, update.Message.Chat.ID, helpText)
}

func (b *Bot) ping(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.reply(ctx, update.Message.Chat.ID, "pong")
}

func (b *Bot) unknownMessage(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.reply(ctx, update.Message.Chat.ID, "Use /help to see the available commands.")
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	_, err := b.sender.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
	if err != nil && !errors.Is(err, context.Canceled) {
		b.logger.ErrorContext(ctx, "Telegram reply failed", "error_kind", errorKind(err))
	}
}

// Library errors can contain request URLs or response bodies. Only expose known
// categories, never the underlying error text or update payload.
func errorKind(err error) string {
	var rateLimit *bot.TooManyRequestsError
	var networkError net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, bot.ErrorUnauthorized):
		return "unauthorized"
	case errors.Is(err, bot.ErrorForbidden):
		return "forbidden"
	case errors.Is(err, bot.ErrorConflict):
		return "conflict"
	case errors.As(err, &rateLimit):
		return "rate_limited"
	case errors.Is(err, bot.ErrorBadRequest):
		return "bad_request"
	case errors.As(err, &networkError):
		return "network"
	default:
		return "request_failed"
	}
}
