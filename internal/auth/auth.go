package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/megazvezda/laziness/models"
)

const (
	moodleService = "moodle_mobile_app"
)

type tokenResponse struct {
	Token        string `json:"token"`
	PrivateToken string `json:"privatetoken"`
}

var client = &http.Client{
	Timeout: 15 * time.Second,
}

func AuthenticateMoodle(ctx context.Context, creds models.Credentials) (models.Student, error) {
	endpoint := "https://moodle.vilniustech.lt/login/token.php"

	values := url.Values{}
	values.Set("username", creds.CredID)
	values.Set("password", creds.Password)
	values.Set("service", moodleService)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return models.Student{}, fmt.Errorf("create authentication request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return models.Student{}, fmt.Errorf("send authentication request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.WarnContext(ctx, "Failed to close Moodle response body",
				"error", err)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return models.Student{}, fmt.Errorf(
			"authentication returned HTTP %d", resp.StatusCode)
	}

	tokens := &tokenResponse{}
	err = json.NewDecoder(resp.Body).Decode(tokens)
	if err != nil {
		return models.Student{}, fmt.Errorf("send authentication request: %w", err)
	}

	if tokens.Token == "" {
		return models.Student{}, fmt.Errorf("token response is missing a token")
	}
	student := models.Student{
		Credentials: creds,
		Tokens: struct {
			PublicToken  string
			PrivateToken string
		}{PublicToken: tokens.Token, PrivateToken: tokens.PrivateToken},
	}
	// we populate only tokens and creds in the student struct,
	// InternalID needs to be populated

	// todo check successful mapping, maybe tests
	return student, nil // todo validate better and populate student struct properly
}
