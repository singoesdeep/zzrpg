package config

import (
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
		JWTSecret:     getEnv("JWT_SECRET", "super_secret_jwt_key_zzrpg"),
		Env:           getEnv("ENV", "development"),
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
