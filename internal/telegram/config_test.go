package telegram

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		ids     string
		wantIDs []int64
		wantErr string
	}{
		{name: "single user", token: "test-token", ids: "123", wantIDs: []int64{123}},
		{name: "whitespace and large IDs", token: " test-token ", ids: " 123, 5000000000 ", wantIDs: []int64{123, 5000000000}},
		{name: "duplicate IDs", token: "test-token", ids: "123,123", wantIDs: []int64{123, 123}},
		{name: "missing token", ids: "123", wantErr: "TELEGRAM_BOT_TOKEN"},
		{name: "blank token", token: " \t", ids: "123", wantErr: "TELEGRAM_BOT_TOKEN"},
		{name: "missing allowlist", token: "test-token", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
		{name: "blank allowlist", token: "test-token", ids: " ", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
		{name: "nonnumeric ID", token: "test-token", ids: "alice", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
		{name: "zero ID", token: "test-token", ids: "0", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
		{name: "negative ID", token: "test-token", ids: "-123", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
		{name: "empty item", token: "test-token", ids: "123,,456", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
		{name: "trailing comma", token: "test-token", ids: "123,", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
		{name: "overflow", token: "test-token", ids: "9223372036854775808", wantErr: "TELEGRAM_ALLOWED_USER_IDS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEGRAM_BOT_TOKEN", tt.token)
			t.Setenv("TELEGRAM_ALLOWED_USER_IDS", tt.ids)
			cfg, err := LoadConfig()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadConfig() error = %v, want %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig(): %v", err)
			}
			if cfg.Token != "test-token" || !reflect.DeepEqual(cfg.AllowedUserIDs, tt.wantIDs) {
				t.Fatal("LoadConfig() returned unexpected configuration")
			}
		})
	}
}
