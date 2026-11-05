package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// CorridorConfig is a single corridor entry from corridors.yaml.
type CorridorConfig struct {
	Name            string `yaml:"name"`
	SellAssetCode   string `yaml:"sell_asset_code"`
	SellAssetIssuer string `yaml:"sell_asset_issuer"`
	BuyAssetCode    string `yaml:"buy_asset_code"`
	BuyAssetIssuer  string `yaml:"buy_asset_issuer"`

	// Sell-leg anchor verification
	SellDomain         string `yaml:"sell_domain"`
	SellAnchorMetadata string `yaml:"sell_anchor_metadata"` // "full" | "none"
	SellVerifiedStatus string `yaml:"sell_verified_status"` // "live" | "pending" | "unverifiable" | "unknown"

	// Buy-leg anchor verification
	BuyDomain         string `yaml:"buy_domain"`
	BuyAnchorMetadata string `yaml:"buy_anchor_metadata"` // "full" | "none"
	BuyVerifiedStatus string `yaml:"buy_verified_status"` // "live" | "pending" | "unverifiable" | "unknown"

	// Shared
	VerificationDate string  `yaml:"verification_date"` // YYYY-MM-DD
	TargetUSDValue   float64 `yaml:"target_usd_value"`  // Economic trade size benchmark in USD (default: 100.0)
	Enabled          bool    `yaml:"enabled"`
}

// CorridorsFile is the top-level structure of corridors.yaml.
type CorridorsFile struct {
	Corridors []CorridorConfig `yaml:"corridors"`
}

// AppConfig holds all runtime configuration, assembled from env vars and YAML.
type AppConfig struct {
	// Database
	DatabaseURL string

	// Horizon
	HorizonURL string

	// Measurement
	SellAmount      float64       // amount of sell asset to price per measurement
	MeasureInterval time.Duration // how often to measure each corridor

	// Integrity thresholds
	DegradedLossPct float64 // loss % above which a corridor is "degraded"
	UnusableLossPct float64 // loss % above which a corridor is "unusable"

	// Webhook
	WebhookURL string // plain HTTP POST target; empty disables webhooks

	// Reference rate
	ReferenceRateURL string // URL template for FX reference rate (optional)

	// Corridors file path
	CorridorsFile string

	// HTTP server
	ListenAddr string

	// Security, CORS, and Rate Limiting
	CORSAllowedOrigins string  // Comma-separated list of allowed origins or "*" (default: "" - disabled)
	RateLimitEnabled   bool    // Enable in-memory rate limiting (default: true)
	RateLimitRPS       float64 // Token refill rate per second per client (default: 10.0)
	RateLimitBurst     int     // Max bucket token burst capacity per client (default: 30)
	TrustProxy         bool    // Trust X-Forwarded-For header for client IP (default: false)

	// Corridors loaded from YAML
	Corridors []CorridorConfig
}

// Load builds an AppConfig from environment variables and parses the corridors file.
func Load() (*AppConfig, error) {
	cfg := &AppConfig{
		DatabaseURL:        env("DATABASE_URL", ""),
		HorizonURL:         env("HORIZON_URL", "https://horizon.stellar.org"),
		SellAmount:         envFloat("SELL_AMOUNT", 100.0),
		MeasureInterval:    envDuration("MEASURE_INTERVAL", 5*time.Minute),
		DegradedLossPct:    envFloat("DEGRADED_LOSS_PCT", 2.5),
		UnusableLossPct:    envFloat("UNUSABLE_LOSS_PCT", 5.0),
		WebhookURL:         env("WEBHOOK_URL", ""),
		ReferenceRateURL:   env("REFERENCE_RATE_URL", "https://api.frankfurter.dev/v2/rates"),
		CorridorsFile:      env("CORRIDORS_FILE", "corridors.yaml"),
		ListenAddr:         env("LISTEN_ADDR", ":8080"),
		CORSAllowedOrigins: env("CORS_ALLOWED_ORIGINS", ""),
		RateLimitEnabled:   envBool("RATE_LIMIT_ENABLED", true),
		RateLimitRPS:       envFloat("RATE_LIMIT_RPS", 10.0),
		RateLimitBurst:     envInt("RATE_LIMIT_BURST", 30),
		TrustProxy:         envBool("TRUST_PROXY", false),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	corridors, err := LoadCorridors(cfg.CorridorsFile)
	if err != nil {
		return nil, fmt.Errorf("loading corridors file: %w", err)
	}
	cfg.Corridors = corridors

	return cfg, nil
}

// LoadCorridors parses a corridors.yaml file and returns the list of enabled corridor configs.
func LoadCorridors(path string) ([]CorridorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var f CorridorsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	var out []CorridorConfig
	for _, c := range f.Corridors {
		if c.Enabled {
			out = append(out, c)
		}
	}
	return out, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var f float64
	if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
		return def
	}
	return f
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
