package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	UserIDKey   contextKey = "userID"
	UsernameKey contextKey = "username"
	RoleKey     contextKey = "role"
)

// ParseAccessToken validates a signed access token against jwtSecret and
// returns its claims. It pins HS256 to defend against algorithm-substitution
// attacks. It is the single place token validation lives, shared by the HTTP
// middleware and any other transport (e.g. WebSocket auth).
func ParseAccessToken(jwtSecret, tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

func AuthMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing authorization header")
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid authorization header format")
				return
			}

			claims, err := ParseAccessToken(jwtSecret, parts[1])
			if err != nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired token")
				return
			}

			// Add claims to request context
			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UsernameKey, claims.Username)
			ctx = context.WithValue(ctx, RoleKey, claims.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin wraps a handler and rejects requests whose authenticated user
// does not have the admin role. Compose it inside AuthMiddleware so the role is
// present in the request context.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RoleFromContext(r.Context()) != RoleAdmin {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "administrator role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RoleFromContext retrieves the role from context. Returns empty string if absent.
func RoleFromContext(ctx context.Context) string {
	if role, ok := ctx.Value(RoleKey).(string); ok {
		return role
	}
	return ""
}

// UserIDFromContext retrieves the user ID from context. Returns 0 if not present.
func UserIDFromContext(ctx context.Context) int64 {
	if id, ok := ctx.Value(UserIDKey).(int64); ok {
		return id
	}
	return 0
}

// UsernameFromContext retrieves the username from context. Returns empty string if not present.
func UsernameFromContext(ctx context.Context) string {
	if name, ok := ctx.Value(UsernameKey).(string); ok {
		return name
	}
	return ""
}
