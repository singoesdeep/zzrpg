package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"
)

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Response/error envelope types live once in pkg/httpx.
type apiResponse = httpx.Response
type apiError = httpx.Error

func RegisterHandler(service AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid request body")
			return
		}

		if req.Username == "" || req.Email == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "Username, email, and password are required")
			return
		}

		user, err := service.Register(r.Context(), req.Username, req.Email, req.Password)
		if err != nil {
			if errors.Is(err, ErrUserAlreadyExists) {
				writeError(w, http.StatusConflict, "USER_ALREADY_EXISTS", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Registration failed")
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data: map[string]interface{}{
				"user_id":  user.ID,
				"username": user.Username,
				"email":    user.Email,
			},
		})
	}
}

func LoginHandler(service AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid request body")
			return
		}

		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "Username and password are required")
			return
		}

		pair, err := service.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, ErrInvalidCredentials) {
				writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", err.Error())
				return
			}
			if errors.Is(err, ErrTooManyAttempts) {
				w.Header().Set("Retry-After", "900")
				writeError(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Login failed")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    tokenData(pair),
		})
	}
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshHandler exchanges a valid refresh token for a new token pair (rotating
// the refresh token). POST body: {"refresh_token": "..."}.
func RefreshHandler(service AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "refresh_token is required")
			return
		}
		pair, err := service.Refresh(r.Context(), req.RefreshToken)
		if err != nil {
			if errors.Is(err, ErrInvalidRefreshToken) {
				writeError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Refresh failed")
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{Success: true, Data: tokenData(pair)})
	}
}

// LogoutHandler revokes a refresh token. POST body: {"refresh_token": "..."}.
func LogoutHandler(service AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "refresh_token is required")
			return
		}
		if err := service.Logout(r.Context(), req.RefreshToken); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Logout failed")
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{Success: true, Data: map[string]interface{}{"revoked": true}})
	}
}

func tokenData(pair *TokenPair) map[string]interface{} {
	return map[string]interface{}{
		"token":         pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
	}
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	httpx.WriteError(w, statusCode, code, message)
}
