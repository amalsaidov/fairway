package config

import (
	"strings"
	"testing"
)

func TestWebhookFormat(t *testing.T) {
	valid := map[string]string{
		"":          "raw",
		"raw":       "raw",
		"RAW":       "raw",
		"discord":   "discord",
		"Discord":   "discord",
		" discord ": "discord",
	}
	for in, want := range valid {
		got, err := webhookFormat(in)
		if err != nil {
			t.Errorf("webhookFormat(%q): unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("webhookFormat(%q) = %q, want %q", in, got, want)
		}
	}

	if _, err := webhookFormat("discrod"); err == nil {
		t.Error(`webhookFormat("discrod"): expected error for unsupported value, got nil`)
	}
}

func TestLoad_UnsupportedWebhookFormat(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/fairway")
	t.Setenv("WEBHOOK_FORMAT", "discrod")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unsupported WEBHOOK_FORMAT, got nil")
	}
	if !strings.Contains(err.Error(), "WEBHOOK_FORMAT") {
		t.Errorf("expected error to mention WEBHOOK_FORMAT, got: %v", err)
	}
}
