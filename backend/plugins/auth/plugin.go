package auth

import (
	"encoding/json"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
	"github.com/singoesdeep/zzrpg/backend/platform/socket"
	"github.com/singoesdeep/zzrpg/sdk/engine/admin"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
)

// AdminOnly composes JWT auth with an admin-role check for mutating
// administrative endpoints.
func AdminOnly(jwtSecret string, h http.Handler) http.Handler {
	return auth.AuthMiddleware(jwtSecret)(auth.RequireAdmin(h))
}

type Plugin struct{ plugin.Base }

func (Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Authentication & Users",
		Description: "User registration, JWT tokens, refresh token rotation, and brute-force protection",
		Icon:        "fa-key",
		Category:    "Security",
		Endpoints:   []string{"POST /api/v1/auth/register", "POST /api/v1/auth/login", "POST /api/v1/auth/refresh", "GET /api/v1/auth/me"},
	}
}

func (Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "auth", Requires: []string{"core"}} }

func (Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	cfg := ic.Config()

	// Provide the WebSocket authenticator so core can gate /ws without importing
	// the auth domain (resolved per-connection by core).
	jwtSecret := cfg.JWTSecret
	if err := registry.Provide(reg, "wsAuthenticator", socket.Authenticator(func(token string) (int64, string, bool) {
		claims, err := auth.ParseAccessToken(jwtSecret, token)
		if err != nil {
			return 0, "", false
		}
		return claims.UserID, claims.Username, true
	})); err != nil {
		return err
	}

	db := registry.MustResolve[*database.DB](reg, "db")
	userRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(userRepo, cfg.JWTSecret,
		auth.WithRefreshStore(auth.NewPgRefreshStore(db.Store)),
		auth.WithTokenTTLs(cfg.AccessTokenTTL, cfg.RefreshTokenTTL),
	)

	mux.HandleFunc("/api/v1/auth/register", auth.RegisterHandler(authService))
	mux.HandleFunc("/api/v1/auth/login", auth.LoginHandler(authService))
	mux.HandleFunc("/api/v1/auth/refresh", auth.RefreshHandler(authService))
	mux.HandleFunc("/api/v1/auth/logout", auth.LogoutHandler(authService))
	mux.Handle("/api/v1/auth/me", auth.AuthMiddleware(cfg.JWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID := auth.UserIDFromContext(r.Context())
		username := auth.UsernameFromContext(r.Context())

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"user_id":  userID,
				"username": username,
			},
		})
	})))

	return nil
}
