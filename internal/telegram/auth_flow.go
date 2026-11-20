package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	tgmodels "github.com/go-telegram/bot/models"
	"github.com/megazvezda/laziness/internal/auth"
	"github.com/megazvezda/laziness/models"
	"github.com/megazvezda/laziness/storage"
)

const moodleAuthLifetime = 5 * time.Minute

type moodleAuthRequest struct {
	expiresAt time.Time
	prompted  bool
}

type moodleAuthRequests struct {
	mu     sync.Mutex
	byUser map[int64]moodleAuthRequest
}

// authMoodle starts a login from an allowed group. Credentials are accepted in private.
func (b *Bot) authMoodle(ctx context.Context, client *bot.Bot, update *tgmodels.Update) {
	message := update.Message
	if message.Chat.Type != tgmodels.ChatTypeGroup && message.Chat.Type != tgmodels.ChatTypeSupergroup {
		return
	}
	if message.From == nil || message.SenderChat != nil {
		return
	}
	if len(strings.Fields(message.Text)) != 1 {
		b.deleteAuthMessage(ctx, client, message)
		b.reply(ctx, message.Chat.ID, "Send only /auth here. Enter credentials in the private chat.")
		return
	}

	userID := message.From.ID
	now := time.Now()
	expiresAt := now.Add(moodleAuthLifetime)
	b.authRequests.mu.Lock()
	if b.authRequests.byUser == nil {
		b.authRequests.byUser = make(map[int64]moodleAuthRequest)
	}
	for id, request := range b.authRequests.byUser {
		if !now.Before(request.expiresAt) {
			delete(b.authRequests.byUser, id)
		}
	}
	if _, pending := b.authRequests.byUser[userID]; pending {
		b.authRequests.mu.Unlock()
		return
	}
	b.authRequests.byUser[userID] = moodleAuthRequest{expiresAt: expiresAt}
	b.authRequests.mu.Unlock()

	sendCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	_, err := b.sender.SendMessage(sendCtx, &bot.SendMessageParams{
		ChatID: message.Chat.ID,
		Text:   "Continue in private to sign in to Moodle. This request expires in five minutes.",
		ReplyMarkup: &tgmodels.InlineKeyboardMarkup{
			InlineKeyboard: [][]tgmodels.InlineKeyboardButton{
				{{Text: "Continue in private", URL: "https://t.me/" + b.username + "?start=auth"}},
			},
		},
	})
	if err != nil {
		b.authRequests.mu.Lock()
		if b.authRequests.byUser[userID].expiresAt == expiresAt {
			delete(b.authRequests.byUser, userID)
		}
		b.authRequests.mu.Unlock()
		b.logger.WarnContext(ctx, "Could not send Moodle login link", "error_kind", errorKind(err))
	}
}

// handlePrivateMoodleAuth handles the two login steps before the regular DM allowlist.
func (b *Bot) handlePrivateMoodleAuth(ctx context.Context, client *bot.Bot, message *tgmodels.Message) bool {
	if !strings.HasPrefix(message.Text, "/") {
		return false
	}
	args := strings.Fields(message.Text)
	command, username, addressed := strings.Cut(args[0], "@")
	if addressed && !strings.EqualFold(username, b.username) {
		return false
	}
	starting := command == "/start" && len(args) == 2 && args[1] == "auth"
	if !starting && command != "/auth" {
		return false
	}
	if !b.authRequests.advance(message.From.ID, starting) {
		return true
	}
	if starting {
		b.reply(ctx, message.Chat.ID, "Send /auth username password here. You have one login attempt.")
		return true
	}

	b.deleteAuthMessage(ctx, client, message)
	if len(args) != 3 {
		b.reply(ctx, message.Chat.ID, "Expected /auth username password. Start again with /auth in the group.")
		return true
	}
	b.finishMoodleAuth(ctx, message, models.Credentials{CredID: args[1], Password: args[2]})
	return true
}

// advance records the first prompt and consumes the first credential attempt.
func (r *moodleAuthRequests) advance(userID int64, starting bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	request, ok := r.byUser[userID]
	if !ok || !time.Now().Before(request.expiresAt) {
		delete(r.byUser, userID)
		return false
	}
	if starting {
		if request.prompted {
			return false
		}
		request.prompted = true
		r.byUser[userID] = request
		return true
	}
	if !request.prompted {
		return false
	}
	delete(r.byUser, userID)
	return true
}

func (b *Bot) deleteAuthMessage(ctx context.Context, client *bot.Bot, message *tgmodels.Message) {
	deleteCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	deleted, err := client.DeleteMessage(deleteCtx, &bot.DeleteMessageParams{
		ChatID: message.Chat.ID, MessageID: message.ID,
	})
	if err != nil || !deleted {
		b.logger.WarnContext(ctx, "Could not delete Moodle login message", "error_kind", errorKind(err))
		if message.Chat.Type == tgmodels.ChatTypePrivate {
			b.reply(ctx, message.Chat.ID, "I couldn't delete your login message. Please delete it yourself.")
		}
	}
}

func (b *Bot) finishMoodleAuth(ctx context.Context, message *tgmodels.Message, credentials models.Credentials) {
	res, err := auth.AuthenticateMoodle(ctx, credentials)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Moodle authentication failed", "error_kind", errorKind(err))
			b.reply(ctx, message.Chat.ID, "Failed to authenticate into Moodle. Start again with /auth in the group.")
		}
		return
	}
	userInfo, err := auth.GetUserInfo(ctx, res.Tokens)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Moodle user-info retrieval failed", "error_kind", errorKind(err))
			b.reply(ctx, message.Chat.ID, "Failed to get Moodle user info. Start again with /auth in the group.")
		}
		return
	}
	record := models.DBRecord{
		TelegramUserID: message.From.ID,
		MoodleUserID:   userInfo.UserID,
		FullName:       userInfo.FullName,
		PublicToken:    res.Tokens.PublicToken,
		PrivateToken:   res.Tokens.PrivateToken,
	}
	if err := storage.SaveSession(ctx, b.db, record); err != nil {
		if !errors.Is(err, context.Canceled) {
			b.logger.ErrorContext(ctx, "Could not save Moodle session", "error_kind", errorKind(err))
			b.reply(ctx, message.Chat.ID, "Could not save your session. Start again with /auth in the group.")
		}
		return
	}
	b.reply(ctx, message.Chat.ID, fmt.Sprintf("Logged in as %s (%d)", record.FullName, record.MoodleUserID))
}
