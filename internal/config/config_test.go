package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load()
	if err == nil {
		t.Fatal("expected an error when DATABASE_URL is empty, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Only DATABASE_URL and a valid corridors file are provided; everything else
	// must fall back to its documented default.
	corridorsPath := writeCorridorsFile(t, `corridors:
  - name: USDC/EURC
    enabled: true
`)
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/fairway?sslmode=disable")
	t.Setenv("CORRIDORS_FILE", corridorsPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/fairway?sslmode=disable" {
		t.Errorf("DatabaseURL = %q, want the provided value", cfg.DatabaseURL)
	}
	if cfg.HorizonURL != "https://horizon.stellar.org" {
		t.Errorf("HorizonURL default = %q", cfg.HorizonURL)
	}
	if cfg.SellAmount != 100.0 {
		t.Errorf("SellAmount default = %v, want 100", cfg.SellAmount)
	}
	if cfg.MeasureInterval != 5*time.Minute {
		t.Errorf("MeasureInterval default = %v, want 5m", cfg.MeasureInterval)
	}
	if cfg.DegradedLossPct != 2.5 {
		t.Errorf("DegradedLossPct default = %v, want 2.5", cfg.DegradedLossPct)
	}
	if cfg.UnusableLossPct != 5.0 {
		t.Errorf("UnusableLossPct default = %v, want 5.0", cfg.UnusableLossPct)
	}
	if cfg.WebhookURL != "" {
		t.Errorf("WebhookURL default = %q, want empty", cfg.WebhookURL)
	}
	if cfg.ReferenceRateURL != "https://api.frankfurter.dev/v2/rates" {
		t.Errorf("ReferenceRateURL default = %q", cfg.ReferenceRateURL)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr default = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.CORSAllowedOrigins != "" {
		t.Errorf("CORSAllowedOrigins default = %q, want empty", cfg.CORSAllowedOrigins)
	}
	if !cfg.RateLimitEnabled {
		t.Errorf("RateLimitEnabled default = %v, want true", cfg.RateLimitEnabled)
	}
	if cfg.RateLimitRPS != 10.0 {
		t.Errorf("RateLimitRPS default = %v, want 10.0", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 30 {
		t.Errorf("RateLimitBurst default = %v, want 30", cfg.RateLimitBurst)
	}
	if cfg.TrustProxy {
		t.Errorf("TrustProxy default = %v, want false", cfg.TrustProxy)
	}

	if len(cfg.Corridors) != 1 {
		t.Fatalf("expected 1 enabled corridor, got %d", len(cfg.Corridors))
	}
	if cfg.Corridors[0].Name != "USDC/EURC" {
		t.Errorf("corridor name = %q, want USDC/EURC", cfg.Corridors[0].Name)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	corridorsPath := writeCorridorsFile(t, `corridors:
  - name: NGNC/USDC
    enabled: true
`)
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("CORRIDORS_FILE", corridorsPath)
	t.Setenv("HORIZON_URL", "https://horizon.example.test")
	t.Setenv("SELL_AMOUNT", "250.5")
	t.Setenv("MEASURE_INTERVAL", "30s")
	t.Setenv("DEGRADED_LOSS_PCT", "1.5")
	t.Setenv("UNUSABLE_LOSS_PCT", "7")
	t.Setenv("WEBHOOK_URL", "https://hooks.example.test/x")
	t.Setenv("REFERENCE_RATE_URL", "https://rates.example.test")
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://gofairway.github.io")
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("RATE_LIMIT_RPS", "42.0")
	t.Setenv("RATE_LIMIT_BURST", "99")
	t.Setenv("TRUST_PROXY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HorizonURL != "https://horizon.example.test" {
		t.Errorf("HorizonURL = %q", cfg.HorizonURL)
	}
	if cfg.SellAmount != 250.5 {
		t.Errorf("SellAmount = %v, want 250.5", cfg.SellAmount)
	}
	if cfg.MeasureInterval != 30*time.Second {
		t.Errorf("MeasureInterval = %v, want 30s", cfg.MeasureInterval)
	}
	if cfg.DegradedLossPct != 1.5 {
		t.Errorf("DegradedLossPct = %v, want 1.5", cfg.DegradedLossPct)
	}
	if cfg.UnusableLossPct != 7 {
		t.Errorf("UnusableLossPct = %v, want 7", cfg.UnusableLossPct)
	}
	if cfg.WebhookURL != "https://hooks.example.test/x" {
		t.Errorf("WebhookURL = %q", cfg.WebhookURL)
	}
	if cfg.ReferenceRateURL != "https://rates.example.test" {
		t.Errorf("ReferenceRateURL = %q", cfg.ReferenceRateURL)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want :9090", cfg.ListenAddr)
	}
	if cfg.CORSAllowedOrigins != "https://gofairway.github.io" {
		t.Errorf("CORSAllowedOrigins = %q", cfg.CORSAllowedOrigins)
	}
	if cfg.RateLimitEnabled {
		t.Errorf("RateLimitEnabled = %v, want false", cfg.RateLimitEnabled)
	}
	if cfg.RateLimitRPS != 42.0 {
		t.Errorf("RateLimitRPS = %v, want 42.0", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 99 {
		t.Errorf("RateLimitBurst = %v, want 99", cfg.RateLimitBurst)
	}
	if !cfg.TrustProxy {
		t.Errorf("TrustProxy = %v, want true", cfg.TrustProxy)
	}
}

func TestLoad_MissingCorridorsFile(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("CORRIDORS_FILE", filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when the corridors file is missing, got nil")
	}
}

func TestLoadCorridors(t *testing.T) {
	path := writeCorridorsFile(t, `corridors:
  - name: NGNC/USDC
    sell_asset_code: NGNC
    buy_asset_code: USDC
    target_usd_value: 100.0
    enabled: true
  - name: USDC/EURC
    sell_asset_code: USDC
    buy_asset_code: EURC
    enabled: true
  - name: DISABLED/CORRIDOR
    sell_asset_code: FOO
    buy_asset_code: BAR
    enabled: false
`)

	corridors, err := LoadCorridors(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(corridors) != 2 {
		t.Fatalf("expected 2 enabled corridors (disabled filtered out), got %d", len(corridors))
	}
	for _, c := range corridors {
		if !c.Enabled {
			t.Errorf("corridor %q should not be present: enabled=false must be filtered", c.Name)
		}
		if c.Name == "DISABLED/CORRIDOR" {
			t.Errorf("disabled corridor was not filtered out")
		}
	}

	first := corridors[0]
	if first.Name != "NGNC/USDC" || first.SellAssetCode != "NGNC" || first.BuyAssetCode != "USDC" {
		t.Errorf("unexpected first corridor: %+v", first)
	}
	if first.TargetUSDValue != 100.0 {
		t.Errorf("TargetUSDValue = %v, want 100.0", first.TargetUSDValue)
	}
}

func TestLoadCorridors_MissingFile(t *testing.T) {
	_, err := LoadCorridors(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing corridors file, got nil")
	}
}

func TestLoadCorridors_MalformedYAML(t *testing.T) {
	path := writeCorridorsFile(t, "corridors: [this is : not valid yaml : : :\n")

	_, err := LoadCorridors(path)
	if err == nil {
		t.Fatal("expected an error for malformed YAML, got nil")
	}
}

func TestLoadCorridors_EmptyFile(t *testing.T) {
	path := writeCorridorsFile(t, "")

	corridors, err := LoadCorridors(path)
	if err != nil {
		t.Fatalf("unexpected error for empty file: %v", err)
	}
	if len(corridors) != 0 {
		t.Errorf("expected no corridors from an empty file, got %d", len(corridors))
	}
}

func TestEnv(t *testing.T) {
	t.Run("returns default when unset", func(t *testing.T) {
		t.Setenv("FAIRWAY_TEST_ENV", "")
		if got := env("FAIRWAY_TEST_ENV", "fallback"); got != "fallback" {
			t.Errorf("env() = %q, want fallback", got)
		}
	})
	t.Run("returns value when set", func(t *testing.T) {
		t.Setenv("FAIRWAY_TEST_ENV", "custom")
		if got := env("FAIRWAY_TEST_ENV", "fallback"); got != "custom" {
			t.Errorf("env() = %q, want custom", got)
		}
	})
}

func TestEnvFloat(t *testing.T) {
	tests := []struct {
		name string
		val  string
		set  bool
		def  float64
		want float64
	}{
		{"unset returns default", "", false, 1.5, 1.5},
		{"empty returns default", "", true, 1.5, 1.5},
		{"valid float", "3.14", true, 1.5, 3.14},
		{"valid integer-like", "7", true, 1.5, 7},
		{"invalid returns default", "notafloat", true, 2.0, 2.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("FAIRWAY_TEST_FLOAT", tt.val)
			} else {
				os.Unsetenv("FAIRWAY_TEST_FLOAT")
			}
			if got := envFloat("FAIRWAY_TEST_FLOAT", tt.def); got != tt.want {
				t.Errorf("envFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnvDuration(t *testing.T) {
	tests := []struct {
		name string
		val  string
		set  bool
		def  time.Duration
		want time.Duration
	}{
		{"unset returns default", "", false, 5 * time.Minute, 5 * time.Minute},
		{"empty returns default", "", true, 5 * time.Minute, 5 * time.Minute},
		{"valid minutes", "10m", true, 5 * time.Minute, 10 * time.Minute},
		{"valid seconds", "30s", true, 5 * time.Minute, 30 * time.Second},
		{"invalid returns default", "notaduration", true, 5 * time.Minute, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("FAIRWAY_TEST_DURATION", tt.val)
			} else {
				os.Unsetenv("FAIRWAY_TEST_DURATION")
			}
			if got := envDuration("FAIRWAY_TEST_DURATION", tt.def); got != tt.want {
				t.Errorf("envDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		name string
		val  string
		set  bool
		def  bool
		want bool
	}{
		{"unset returns default true", "", false, true, true},
		{"unset returns default false", "", false, false, false},
		{"true", "true", true, false, true},
		{"false", "false", true, true, false},
		{"1", "1", true, false, true},
		{"0", "0", true, true, false},
		{"invalid returns default", "maybe", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("FAIRWAY_TEST_BOOL", tt.val)
			} else {
				os.Unsetenv("FAIRWAY_TEST_BOOL")
			}
			if got := envBool("FAIRWAY_TEST_BOOL", tt.def); got != tt.want {
				t.Errorf("envBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnvInt(t *testing.T) {
	tests := []struct {
		name string
		val  string
		set  bool
		def  int
		want int
	}{
		{"unset returns default", "", false, 30, 30},
		{"empty returns default", "", true, 30, 30},
		{"valid integer", "42", true, 30, 42},
		{"negative integer", "-5", true, 30, -5},
		{"invalid returns default", "notanint", true, 30, 30},
		{"float string returns default", "3.14", true, 30, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("FAIRWAY_TEST_INT", tt.val)
			} else {
				os.Unsetenv("FAIRWAY_TEST_INT")
			}
			if got := envInt("FAIRWAY_TEST_INT", tt.def); got != tt.want {
				t.Errorf("envInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

// writeCorridorsFile writes content to a temp YAML file and returns its path.
func writeCorridorsFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "corridors-*.yaml")
	if err != nil {
		t.Fatalf("creating temp corridors file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp corridors file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing temp corridors file: %v", err)
	}
	return f.Name()
}
