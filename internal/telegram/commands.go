package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	"github.com/megazvezda/laziness/storage"
)

func (b *Bot) whoami(ctx context.Context, _ *bot.Bot, update *tgmodels.Update) {
	message := update.Message
	if message.From == nil {
		return
	}
	record, err := storage.RetrieveSession(ctx, b.db, message.From.ID)
	if errors.Is(err, sql.ErrNoRows) {
		b.reply(ctx, message.Chat.ID, "No saved session. Use /auth in the group to start Moodle login.")
		return
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Could not retrieve Moodle session", "error_kind", errorKind(err))
			b.reply(ctx, message.Chat.ID, "Could not retrieve your session.")
		}
		return
	}
	b.reply(ctx, message.Chat.ID, fmt.Sprintf("Sleeping as: %s (%d)", record.FullName, record.MoodleUserID))
}

func (b *Bot) logout(ctx context.Context, _ *bot.Bot, update *tgmodels.Update) {
	message := update.Message
	if message.From == nil {
		return
	}
	deleted, err := storage.DeleteSession(ctx, b.db, message.From.ID)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Could not delete Moodle session", "error_kind", errorKind(err))
			b.reply(ctx, message.Chat.ID, "Could not delete your session.")
		}
		return
	}
	if !deleted {
		b.reply(ctx, message.Chat.ID, "No saved session. Use /auth in the group to start Moodle login.")
		return
	}
	b.reply(ctx, message.Chat.ID, "Logged out. Your Moodle tokens were deleted.")
}
