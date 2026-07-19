package character

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/pkg/httpx"
)

type createCharacterRequest struct {
	Name      string `json:"name"`
	ClassName string `json:"class_name"`
}

// Response/error envelope types live once in pkg/httpx.
type apiResponse = httpx.Response
type apiError = httpx.Error

func CreateHandler(service CharacterService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		userID := auth.UserIDFromContext(r.Context())
		if userID == 0 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "User context not found")
			return
		}

		var req createCharacterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid request body")
			return
		}

		char, err := service.Create(r.Context(), userID, req.Name, req.ClassName)
		if err != nil {
			if errors.Is(err, ErrCharacterNameTaken) {
				writeError(w, http.StatusConflict, "NAME_TAKEN", err.Error())
				return
			}
			if errors.Is(err, ErrCharacterLimitReached) {
				writeError(w, http.StatusForbidden, "CHARACTER_LIMIT_REACHED", err.Error())
				return
			}
			if errors.Is(err, ErrInvalidClass) || errors.Is(err, ErrNameTooShort) || errors.Is(err, ErrNameTooLong) {
				writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to create character")
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    char,
		})
	}
}

func ListHandler(service CharacterService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		userID := auth.UserIDFromContext(r.Context())
		if userID == 0 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "User context not found")
			return
		}

		chars, err := service.ListByUserID(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to list characters")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    chars,
		})
	}
}

func GetHandler(service CharacterService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		// Support both wildcards and query params for flexibility
		idStr := r.PathValue("id")
		if idStr == "" {
			idStr = r.URL.Query().Get("id")
		}

		charID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || charID <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid character ID")
			return
		}

		userID := auth.UserIDFromContext(r.Context())
		if userID == 0 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "User context not found")
			return
		}

		char, err := service.GetByID(r.Context(), charID)
		if err != nil {
			if errors.Is(err, ErrCharacterNotFound) {
				writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to retrieve character")
			return
		}

		// Ownership check: a user may only read their own characters. Return 404
		// (not 403) so IDs belonging to other users cannot be enumerated.
		if char.UserID != userID {
			writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", "character not found")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    char,
		})
	}
}

func GetStatsHandler(service CharacterService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		idStr := r.PathValue("id")
		if idStr == "" {
			idStr = r.URL.Query().Get("id")
		}

		charID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || charID <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid character ID")
			return
		}

		userID := auth.UserIDFromContext(r.Context())
		if userID == 0 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "User context not found")
			return
		}

		char, err := service.GetByID(r.Context(), charID)
		if err != nil {
			if errors.Is(err, ErrCharacterNotFound) {
				writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to retrieve stats")
			return
		}

		// Ownership check (see GetHandler). Prevents reading other users' stats.
		if char.UserID != userID {
			writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", "character not found")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data: map[string]interface{}{
				"character_id":  char.ID,
				"base_stats":    char.Stats.BaseStats,
				"derived_stats": char.Stats.DerivedStats,
			},
		})
	}
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	httpx.WriteError(w, statusCode, code, message)
}
