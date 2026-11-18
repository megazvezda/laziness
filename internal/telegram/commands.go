package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	"github.com/megazvezda/laziness/internal/auth"
	appmodels "github.com/megazvezda/laziness/models"
)

func (b *Bot) authMoodle(ctx context.Context, _ *bot.Bot, update *tgmodels.Update) {
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
	b.reply(ctx, update.Message.Chat.ID, fmt.Sprintf("Logged in as %s (%d)", userInfo.FullName, userInfo.UserID))
}
