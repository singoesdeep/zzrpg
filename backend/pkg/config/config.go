package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	DatabaseURL   string
	RedisURL      string
	ZzstatGRPCURL string
	JWTSecret     string
	Env           string
}

func LoadConfig() (*Config, error) {
	// Optional: Load .env if present
	_ = godotenv.Load()

	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://postgres:password123@localhost:5432/zzrpg?sslmode=disable"),
		RedisURL:      getEnv("REDIS_URL", "redis://localhost:6379/0"),
		ZzstatGRPCURL: getEnv("ZZSTAT_GRPC_URL", "localhost:50051"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		Env:           getEnv("ENV", "development"),
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

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
