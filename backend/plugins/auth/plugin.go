package auth

import (
	"encoding/json"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
)

// AdminOnly composes JWT auth with an admin-role check for mutating
// administrative endpoints.
func AdminOnly(jwtSecret string, h http.Handler) http.Handler {
	return auth.AuthMiddleware(jwtSecret)(auth.RequireAdmin(h))
}

type Plugin struct{ plugin.Base }

func (Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "auth", Requires: []string{"core"}} }

func (Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	cfg := ic.Config()

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
