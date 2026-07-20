package idle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/pkg/httpx"
)

// CharacterReader is the slice of the character service the idle handlers need:
// ownership + power/level for a character.
type CharacterReader interface {
	GetByID(ctx context.Context, id int64) (*character.CharacterWithStats, error)
}

// owned resolves the {id} path character and verifies it belongs to the
// authenticated user. A missing or non-owned character both return 404 so ids
// cannot be enumerated across accounts.
func owned(w http.ResponseWriter, r *http.Request, chars CharacterReader) (*character.CharacterWithStats, bool) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == 0 {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user context not found")
		return nil, false
	}
	charID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || charID <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_ID", "invalid character id")
		return nil, false
	}
	char, err := chars.GetByID(r.Context(), charID)
	if err != nil || char.UserID != userID {
		httpx.WriteError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", "character not found")
		return nil, false
	}
	return char, true
}

// StateHandler serves GET /characters/{id}/idle/state.
func StateHandler(svc *Service, chars CharacterReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		char, ok := owned(w, r, chars)
		if !ok {
			return
		}
		view, err := svc.State(r.Context(), char.ID)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to load idle state")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, view)
	}
}

// ActivitiesHandler serves GET /characters/{id}/idle/activities.
func ActivitiesHandler(svc *Service, chars CharacterReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		char, ok := owned(w, r, chars)
		if !ok {
			return
		}
		power := svc.Power(char.Stats.DerivedStats)
		httpx.WriteJSON(w, http.StatusOK, svc.Activities(power, char.Level))
	}
}

// AssignHandler serves POST /characters/{id}/idle/assign with {type, id}.
func AssignHandler(svc *Service, chars CharacterReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		char, ok := owned(w, r, chars)
		if !ok {
			return
		}
		var body struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
			return
		}
		power := svc.Power(char.Stats.DerivedStats)
		err := svc.Assign(r.Context(), char.ID, power, char.Level, Assignment{Type: ActivityType(body.Type), ID: body.ID})
		switch {
		case errors.Is(err, ErrActivityNotFound):
			httpx.WriteError(w, http.StatusNotFound, "ACTIVITY_NOT_FOUND", "unknown activity")
		case errors.Is(err, ErrActivityLocked):
			httpx.WriteError(w, http.StatusForbidden, "ACTIVITY_LOCKED", "activity locked for this character")
		case err != nil:
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to assign activity")
		default:
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"type": body.Type, "id": body.ID})
		}
	}
}

// UpgradeBuildingHandler serves POST /characters/{id}/idle/buildings/{gen}/upgrade.
func UpgradeBuildingHandler(svc *Service, chars CharacterReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		char, ok := owned(w, r, chars)
		if !ok {
			return
		}
		gen := r.PathValue("gen")
		newLevel, err := svc.UpgradeBuilding(r.Context(), char.ID, gen)
		switch {
		case errors.Is(err, ErrNotAGenerator):
			httpx.WriteError(w, http.StatusNotFound, "GENERATOR_NOT_FOUND", "unknown generator")
		case errors.Is(err, ErrMaxLevel):
			httpx.WriteError(w, http.StatusConflict, "MAX_LEVEL", "generator already at max level")
		case errors.Is(err, ErrInsufficientResources):
			httpx.WriteError(w, http.StatusPaymentRequired, "INSUFFICIENT_RESOURCES", "not enough resources")
		case errors.Is(err, ErrInsufficientGold):
			httpx.WriteError(w, http.StatusPaymentRequired, "INSUFFICIENT_GOLD", "not enough gold")
		case err != nil:
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to upgrade")
		default:
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"generator_id": gen, "level": newLevel})
		}
	}
}
