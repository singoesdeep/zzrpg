package config

import "testing"

func TestAllowOrigin(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		allowed []string
		origin  string
		want    bool
	}{
		{"empty origin always allowed (non-browser)", "production", nil, "", true},
		{"dev with no allowlist permits any", "development", nil, "https://evil.example", true},
		{"prod with no allowlist denies cross-origin", "production", nil, "https://evil.example", false},
		{"explicit allowlist match", "production", []string{"https://game.example"}, "https://game.example", true},
		{"explicit allowlist case-insensitive", "production", []string{"https://Game.Example"}, "https://game.example", true},
		{"explicit allowlist miss", "production", []string{"https://game.example"}, "https://evil.example", false},
		{"wildcard entry allows any", "production", []string{"*"}, "https://anything.example", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Env: tt.env, AllowedOrigins: tt.allowed}
			if got := c.AllowOrigin(tt.origin); got != tt.want {
				t.Fatalf("AllowOrigin(%q) env=%s allowed=%v = %v, want %v", tt.origin, tt.env, tt.allowed, got, tt.want)
			}
		})
	}
}
