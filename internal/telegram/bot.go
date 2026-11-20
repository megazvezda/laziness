package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/megazvezda/laziness/internal"
)

const requestTimeout = 10 * time.Second

const helpText = "Available commands:\n" +
	"/start\n" +
	"/help\n" +
	"/ping\n" +
	"/auth (start Moodle login in an allowed group)\n" +
	"/whoami"

// messageSender is the small part of the Telegram client that handlers need.
type messageSender interface {
	SendMessage(context.Context, *bot.SendMessageParams) (*models.Message, error)
}

// Bot handles private messages from an explicit set of Telegram users.
type Bot struct {
	client        *bot.Bot
	sender        messageSender
	allowedUsers  map[int64]struct{}
	allowedGroups map[int64]struct{}
	logger        *slog.Logger
	db            *sql.DB
	photoDir      string
	attendance    internal.AttendanceService
	username      string
	authRequests  moodleAuthRequests
}

// New constructs a bot. Can pass nil as a logger to use slog.Default.
func New(cfg Config, logger *slog.Logger, db *sql.DB) (*Bot, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bot{
		allowedUsers:  make(map[int64]struct{}, len(cfg.AllowedUserIDs)),
		allowedGroups: make(map[int64]struct{}, len(cfg.AllowedGroupsIDs)),
		logger:        logger,
		db:            db,
	}
	for _, id := range cfg.AllowedUserIDs {
		b.allowedUsers[id] = struct{}{}
	}
	for _, id := range cfg.AllowedGroupsIDs {
		b.allowedGroups[id] = struct{}{}
	}
	client, err := bot.New(strings.TrimSpace(cfg.Token),
		// Authenticate in Run so startup requests share the shutdown context.
		bot.WithSkipGetMe(),
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message"}),
		bot.WithNotAsyncHandlers(),
		bot.WithMiddlewares(b.authorize),
		bot.WithDefaultHandler(func(context.Context, *bot.Bot, *models.Update) {}),
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
	cancel() // cancel child context, free resources
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
	if len(b.allowedGroups) > 0 {
		if err := b.preparePhotoCache(); err != nil {
			return fmt.Errorf("prepare Telegram photo cache: %w", err)
		}
		b.client.RegisterHandlerMatchFunc(isGroupPhoto, b.handleGroupPhoto)
	}
	b.logger.InfoContext(ctx, "Telegram bot started", "transport", "long_polling")
	b.client.Start(ctx)
	b.logger.InfoContext(ctx, "Telegram bot stopped")
	return nil
}

func (b *Bot) registerCommands(username string) {
	b.username = username
	commands := []struct {
		name    string
		handler bot.HandlerFunc
	}{
		{"start", b.start},
		{"help", b.help},
		{"ping", b.ping},
		{"auth", b.authMoodle},
		{"whoami", b.whoami},
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
		if message.Text == "" && len(message.Photo) == 0 {
			return
		}
		switch message.Chat.Type {
		case models.ChatTypePrivate:
			if message.From == nil || message.Text == "" {
				return
			}
			if b.handlePrivateMoodleAuth(ctx, client, message) {
				return
			}
			if _, ok := b.allowedUsers[message.From.ID]; !ok {
				return
			}

		case models.ChatTypeGroup, models.ChatTypeSupergroup:
			if _, ok := b.allowedGroups[message.Chat.ID]; !ok {
				return
			}
		}
		next(ctx, client, update)
	}
}

func (b *Bot) start(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.reply(ctx, update.Message.Chat.ID, "this is a sample text. todo\n\n"+helpText)
}

func (b *Bot) help(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.reply(ctx, update.Message.Chat.ID, helpText)
}

func (b *Bot) ping(ctx context.Context, _ *bot.Bot, update *models.Update) {
	b.reply(ctx, update.Message.Chat.ID, "pong")
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
