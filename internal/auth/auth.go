package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/megazvezda/laziness/models"
)

const (
	moodleService = "moodle_mobile_app"
	moodleURL     = "https://moodle.vilniustech.lt"
)

// Fields used from Moodle's token and site-info responses.
type moodleResponse struct {
	Token        string `json:"token"`
	PrivateToken string `json:"privatetoken"`
	UserID       int64  `json:"userid"`
	Error        string `json:"error"`
	ErrorCode    string `json:"errorcode"`
	Exception    string `json:"exception"`
}

var client = &http.Client{
	Timeout: 15 * time.Second,
}

func AuthenticateMoodle(ctx context.Context, creds models.Credentials) (models.Student, error) {
	if strings.TrimSpace(creds.CredID) == "" || creds.Password == "" {
		return models.Student{}, fmt.Errorf("username and password are required")
	}

	values := url.Values{}
	values.Set("username", creds.CredID)
	values.Set("password", creds.Password)
	values.Set("service", moodleService)

	var tokens moodleResponse
	if err := postMoodle(ctx, "/login/token.php", values, &tokens); err != nil {
		return models.Student{}, fmt.Errorf("authenticate Moodle: %w", err)
	}
	if strings.TrimSpace(tokens.Token) == "" {
		return models.Student{}, fmt.Errorf("token response is missing a token")
	}

	values = url.Values{
		"wstoken":            {tokens.Token},
		"wsfunction":         {"core_webservice_get_site_info"},
		"moodlewsrestformat": {"json"},
	}
	var siteInfo moodleResponse
	if err := postMoodle(ctx, "/webservice/rest/server.php", values, &siteInfo); err != nil {
		return models.Student{}, fmt.Errorf("get Moodle site info: %w", err)
	}
	if siteInfo.UserID <= 0 {
		return models.Student{}, fmt.Errorf("site info response is missing a valid user ID")
	}

	return models.Student{
		InternalID:  strconv.FormatInt(siteInfo.UserID, 10),
		Credentials: creds,
		Tokens: models.Tokens{
			PublicToken:  tokens.Token,
			PrivateToken: tokens.PrivateToken,
		},
	}, nil
}

func postMoodle(ctx context.Context, path string, values url.Values, result *moodleResponse) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		moodleURL+path,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.WarnContext(ctx, "Failed to close Moodle response body",
				"error", err)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Moodle returned HTTP %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if result.ErrorCode != "" {
		return fmt.Errorf("Moodle error: %s", result.ErrorCode)
	}
	if result.Error != "" || result.Exception != "" {
		return fmt.Errorf("Moodle returned an error")
	}
	return nil
}
