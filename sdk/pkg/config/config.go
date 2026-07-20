package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	DatabaseURL   string
	RedisURL      string
	ZzstatGRPCURL string
	JWTSecret     string
	Env           string

	// AllowedOrigins is the browser Origin allowlist for CORS and WebSocket
	// upgrades, parsed from ALLOWED_ORIGINS (comma-separated). A single "*"
	// entry allows any origin. Empty means: allow all in development, deny
	// cross-origin in production. See AllowOrigin.
	AllowedOrigins []string

	// HTTP hardening.
	RateLimitRPS   float64 // sustained requests/sec allowed per client IP
	RateLimitBurst int     // burst size above the sustained rate
	MaxBodyBytes   int64   // max accepted request body size in bytes

	// OutboxRetention is how long dispatched (published) outbox rows are kept
	// before the relay prunes them. <=0 disables pruning.
	OutboxRetention time.Duration

	// IdleTickInterval is how often the idle plugin accrues real-time progress
	// for online characters and pushes IDLE_TICK. Kept comfortably above the idle
	// minimum-elapsed gate so each tick reliably grants.
	IdleTickInterval time.Duration

	// Token lifetimes.
	AccessTokenTTL  time.Duration // short-lived JWT access token
	RefreshTokenTTL time.Duration // long-lived, rotating refresh token
}

func LoadConfig() (*Config, error) {
	// Optional: Load .env if present
	_ = godotenv.Load()

	cfg := &Config{
		Port:           getEnv("PORT", "8080"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"),
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379/0"),
		ZzstatGRPCURL:  getEnv("ZZSTAT_GRPC_URL", "localhost:50051"),
		JWTSecret:      getEnv("JWT_SECRET", ""),
		Env:            getEnv("ENV", "development"),
		AllowedOrigins: parseCSV(getEnv("ALLOWED_ORIGINS", "")),

		RateLimitRPS:     getEnvFloat("RATE_LIMIT_RPS", 20),
		RateLimitBurst:   getEnvInt("RATE_LIMIT_BURST", 40),
		MaxBodyBytes:     int64(getEnvInt("MAX_BODY_BYTES", 1<<20)), // 1 MiB
		OutboxRetention:  getEnvDuration("OUTBOX_RETENTION", 24*time.Hour),
		IdleTickInterval: getEnvDuration("IDLE_TICK_INTERVAL", 15*time.Second),

		AccessTokenTTL:  getEnvDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getEnvDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
	}

	// Fail fast on insecure configuration in production instead of silently
	// falling back to a well-known default secret (which would let anyone forge
	// JWTs). Development keeps a convenience default that is never used in prod.
	if cfg.Env == "production" {
		if cfg.JWTSecret == "" {
			return nil, errors.New("JWT_SECRET must be set when ENV=production")
		}
		if len(cfg.JWTSecret) < 32 {
			return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters in production (got %d)", len(cfg.JWTSecret))
		}
		if v, ok := os.LookupEnv("DATABASE_URL"); !ok || v == "" {
			return nil, errors.New("DATABASE_URL must be explicitly set when ENV=production")
		}
	} else if cfg.JWTSecret == "" {
		cfg.JWTSecret = "dev_only_insecure_secret_do_not_use_in_prod"
	}

	return cfg, nil
}

// AllowOrigin reports whether a browser Origin header value is permitted for
// CORS responses and WebSocket upgrades. An empty origin (non-browser client,
// e.g. a native game client or server-to-server call) is always allowed since
// it is not subject to the browser same-origin policy. With no configured
// allowlist, development permits any origin for convenience and production
// denies cross-origin requests (same-origin only).
func (c *Config) AllowOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	if len(c.AllowedOrigins) == 0 {
		return c.Env != "production"
	}
	for _, o := range c.AllowedOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

// parseCSV splits a comma-separated list, trimming whitespace and dropping
// empty entries.
func parseCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value, exists := os.LookupEnv(key); exists {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
