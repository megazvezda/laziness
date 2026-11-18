package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	"github.com/megazvezda/laziness/internal/auth"
	appmodels "github.com/megazvezda/laziness/models"
	"github.com/megazvezda/laziness/storage"
)

func (b *Bot) authMoodle(ctx context.Context, _ *bot.Bot, update *tgmodels.Update) {
	if update.Message.From == nil {
		return
	}
	args := strings.Fields(update.Message.Text)

	neededArgs := 3
	if len(args) != neededArgs {
		b.reply(ctx, update.Message.Chat.ID, "Usage: /auth username password")
		return
	}
	username := args[1]
	password := args[2]
	creds := appmodels.Credentials{
		CredID:   username,
		Password: password,
	}
	res, err := auth.AuthenticateMoodle(ctx, creds)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Moodle authentication failed", "error_kind", errorKind(err))
			b.reply(ctx, update.Message.Chat.ID, "Failed to authenticate into Moodle")
		}
		return
	}
	userInfo, err := auth.GetUserInfo(ctx, res.Tokens)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Moodle user-info retrieval failed", "error_kind", errorKind(err))
			b.reply(ctx, update.Message.Chat.ID, "Failed to get user-info after authenticating")
		}
		return
	}
	record := appmodels.DBRecord{
		TelegramUserID: update.Message.From.ID,
		MoodleUserID:   userInfo.UserID,
		FullName:       userInfo.FullName,
		PublicToken:    res.Tokens.PublicToken,
		PrivateToken:   res.Tokens.PrivateToken,
	}
	if err := storage.SaveSession(ctx, b.db, record); err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Could not save Moodle session", "error_kind", errorKind(err))
			b.reply(ctx, update.Message.Chat.ID, "Could not save your session. Try again.")
		}
		return
	}
	b.reply(ctx, update.Message.Chat.ID, fmt.Sprintf("Logged in as %s (%d)", record.FullName, record.MoodleUserID))
}

func (b *Bot) whoami(ctx context.Context, _ *bot.Bot, update *tgmodels.Update) {
	message := update.Message
	if message.From == nil {
		return
	}
	record, err := storage.RetrieveSession(ctx, b.db, message.From.ID)
	if errors.Is(err, sql.ErrNoRows) {
		b.reply(ctx, message.Chat.ID, "No saved session. Use /auth username password.")
		return
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Could not retrieve Moodle session", "error_kind", errorKind(err))
			b.reply(ctx, message.Chat.ID, "Could not retrieve your session.")
		}
		return
	}
	b.reply(ctx, message.Chat.ID, fmt.Sprintf("Saved Moodle account: %s (%d)", record.FullName, record.MoodleUserID))
}
