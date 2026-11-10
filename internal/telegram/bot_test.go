package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type fakeSender struct {
	messages []*bot.SendMessageParams
	err      error
	deadline time.Time
}

func (s *fakeSender) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	s.messages = append(s.messages, params)
	s.deadline, _ = ctx.Deadline()
	return &models.Message{}, s.err
}

func testBot(t *testing.T, logs io.Writer) *Bot {
	t.Helper()
	b, err := New(Config{Token: "123:test-secret", AllowedUserIDs: []int64{123, 5000000000}}, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return b
}

// Match the command entity Telegram includes alongside a command's text.
func textMessage(text string) *models.Message {
	m := &models.Message{
		From: &models.User{ID: 123},
		Chat: models.Chat{ID: 99, Type: models.ChatTypePrivate},
		Text: text,
	}
	if strings.HasPrefix(text, "/") {
		m.Entities = []models.MessageEntity{{
			Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len(strings.Fields(text)[0]),
		}}
	}
	return m
}

func TestCommandRouting(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"/start", "Welcome to Laziness!"},
		{"/help", "Available commands:"},
		{"/ping", "pong"},
		{"/start@laziness_bot", "Welcome to Laziness!"},
		{"/help@laziness_bot", "Available commands:"},
		{"/ping@laziness_bot", "pong"},
		{"/start invitation", "Welcome to Laziness!"},
		{"/pingextra", "Use /help"},
		{"/unknown", "Use /help"},
		{"hello", "Use /help"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			b := testBot(t, io.Discard)
			sender := &fakeSender{}
			b.sender = sender
			b.registerCommands("laziness_bot")
			b.client.ProcessUpdate(context.Background(), &models.Update{Message: textMessage(tt.text)})
			if len(sender.messages) != 1 {
				t.Fatalf("sent %d replies, want 1", len(sender.messages))
			}
			if got := sender.messages[0]; got.ChatID != int64(99) || !strings.Contains(got.Text, tt.want) {
				t.Fatalf("reply = %+v, want chat 99 and text containing %q", got, tt.want)
			}
			if sender.deadline.IsZero() || time.Until(sender.deadline) > requestTimeout {
				t.Fatal("reply must have a bounded request timeout")
			}
		})
	}
}

func TestAccessChecks(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*models.Update)
		want   int
	}{
		{name: "allowed", want: 1},
		{name: "second allowed user", modify: func(u *models.Update) { u.Message.From.ID = 5000000000 }, want: 1},
		{name: "denied command", modify: func(u *models.Update) { u.Message.From.ID = 456 }},
		{name: "denied fallback", modify: func(u *models.Update) {
			u.Message = textMessage("hello")
			u.Message.From.ID = 456
		}},
		{name: "missing sender", modify: func(u *models.Update) { u.Message.From = nil }},
		{name: "group", modify: func(u *models.Update) { u.Message.Chat.Type = models.ChatTypeGroup }},
		{name: "supergroup", modify: func(u *models.Update) { u.Message.Chat.Type = models.ChatTypeSupergroup }},
		{name: "channel", modify: func(u *models.Update) { u.Message.Chat.Type = models.ChatTypeChannel }},
		{name: "non-text", modify: func(u *models.Update) { u.Message.Text = "" }},
		{name: "edited message", modify: func(u *models.Update) { u.EditedMessage = u.Message; u.Message = nil }},
		{name: "empty update", modify: func(u *models.Update) { u.Message = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testBot(t, io.Discard)
			sender := &fakeSender{}
			b.sender = sender
			b.registerCommands("laziness_bot")
			update := &models.Update{Message: textMessage("/ping")}
			if tt.modify != nil {
				tt.modify(update)
			}
			b.client.ProcessUpdate(context.Background(), update)
			if len(sender.messages) != tt.want {
				t.Fatalf("sent %d replies, want %d", len(sender.messages), tt.want)
			}
		})
	}
}

func TestReplyFailureDoesNotLogSecrets(t *testing.T) {
	var logs strings.Builder
	b := testBot(t, &logs)
	b.sender = &fakeSender{err: fmt.Errorf("request to https://api.telegram.org/bot123:test-secret failed: private-message")}
	b.registerCommands("laziness_bot")
	b.client.ProcessUpdate(context.Background(), &models.Update{Message: textMessage("/ping")})
	if !strings.Contains(logs.String(), "Telegram reply failed") {
		t.Fatal("missing failure log")
	}
	for _, secret := range []string{"test-secret", "api.telegram.org", "private-message"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("logs contain sensitive data: %s", secret)
		}
	}
}

// fakeHTTPClient exercises the library's real request/response path offline.
type fakeHTTPClient func(*http.Request) (*http.Response, error)

func (f fakeHTTPClient) Do(req *http.Request) (*http.Response, error) { return f(req) }

func replaceHTTPClient(t *testing.T, b *Bot, client fakeHTTPClient) {
	t.Helper()
	c, err := bot.New("123:test-secret", bot.WithSkipGetMe(), bot.WithHTTPClient(time.Minute, client))
	if err != nil {
		t.Fatal(err)
	}
	b.client = c
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestRunStartupFailures(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"invalid token", `{"ok":false,"error_code":401,"description":"test-secret"}`, "unauthorized"},
		{"invalid response", `private-message test-secret`, "request_failed"},
		{"missing username", `{"ok":true,"result":{"id":123,"is_bot":true}}`, "no bot username"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testBot(t, io.Discard)
			replaceHTTPClient(t, b, func(req *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(req.URL.Path, "/getMe") {
					t.Errorf("unexpected request: %s", req.URL.Path)
				}
				return jsonResponse(tt.body), nil
			})
			err := b.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want %s", err, tt.want)
			}
			if strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "private-message") {
				t.Fatal("startup error contains sensitive data")
			}
		})
	}
}

func TestRunStopsPollingOnCancellation(t *testing.T) {
	var logs strings.Builder
	b := testBot(t, &logs)
	polling := make(chan struct{})
	replaceHTTPClient(t, b, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/getMe") {
			return jsonResponse(`{"ok":true,"result":{"id":123,"is_bot":true,"username":"laziness_bot"}}`), nil
		}
		close(polling)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	select {
	case <-polling:
	case <-time.After(5 * time.Second):
		t.Fatal("bot did not start polling")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run(): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bot did not stop polling")
	}
	if !strings.Contains(logs.String(), "Telegram bot started") || !strings.Contains(logs.String(), "Telegram bot stopped") {
		t.Fatal("missing lifecycle logs")
	}
}

func TestRunCanceledBeforeStartup(t *testing.T) {
	b := testBot(t, io.Discard)
	replaceHTTPClient(t, b, func(*http.Request) (*http.Response, error) {
		t.Error("canceled bot must not make requests")
		return nil, context.Canceled
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Run(ctx); err != nil {
		t.Fatalf("Run(): %v", err)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	for _, cfg := range []Config{
		{AllowedUserIDs: []int64{123}},
		{Token: "test-token"},
		{Token: "test-token", AllowedUserIDs: []int64{0}},
		{Token: "test-token", AllowedUserIDs: []int64{-123}},
	} {
		if _, err := New(cfg, nil); err == nil {
			t.Fatal("New() accepted invalid configuration")
		}
	}
}
