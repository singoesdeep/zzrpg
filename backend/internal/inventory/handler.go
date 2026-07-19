package inventory

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/items"
	"github.com/singoesdeep/zzrpg/backend/pkg/httpx"
)

// requireOwnership resolves the authenticated user and verifies charID belongs
// to them. It writes the appropriate error response and returns false on
// failure. A missing character and a non-owned character both map to 404 so
// character IDs cannot be enumerated across accounts.
func requireOwnership(w http.ResponseWriter, r *http.Request, service InventoryService, charID int32) bool {
	userID := auth.UserIDFromContext(r.Context())
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "User context not found")
		return false
	}
	if err := service.VerifyOwnership(r.Context(), userID, charID); err != nil {
		writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", "character not found")
		return false
	}
	return true
}

// Response/error envelope types live once in pkg/httpx.
type apiResponse = httpx.Response
type apiError = httpx.Error

type addTestItemRequest struct {
	CharacterID      int32  `json:"character_id"`
	ItemDefinitionID string `json:"item_definition_id"`
	Quantity         int32  `json:"quantity"`
}

func GetInventoryHandler(service InventoryService) http.HandlerFunc {
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

		charID, err := strconv.ParseInt(idStr, 10, 32)
		if err != nil || charID <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ID", "Invalid character ID")
			return
		}

		if !requireOwnership(w, r, service, int32(charID)) {
			return
		}

		items, err := service.GetInventory(r.Context(), int32(charID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to load inventory")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    items,
		})
	}
}

func MoveItemHandler(service InventoryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var req MoveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid request body")
			return
		}

		if !requireOwnership(w, r, service, req.CharacterID) {
			return
		}

		err := service.MoveItem(r.Context(), req.CharacterID, req.FromSlot, req.ToSlot)
		if err != nil {
			if errors.Is(err, ErrSlotOutOfBounds) || errors.Is(err, ErrInvalidEquipmentSlot) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
				return
			}
			if errors.Is(err, ErrLevelRestricted) || errors.Is(err, ErrClassRestricted) {
				writeError(w, http.StatusForbidden, "EQUIPMENT_RESTRICTED", err.Error())
				return
			}
			if errors.Is(err, ErrItemNotFound) {
				writeError(w, http.StatusNotFound, "ITEM_NOT_FOUND", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    map[string]interface{}{"refresh_stats": true},
		})
	}
}

func AddAdminItemHandler(service InventoryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var req addTestItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid request body")
			return
		}

		if req.Quantity <= 0 {
			req.Quantity = 1
		}

		item := &InventoryItem{
			CharacterID:      req.CharacterID,
			ItemDefinitionID: req.ItemDefinitionID,
			Quantity:         req.Quantity,
			Durability:       100,
			CustomModifiers:  []items.StatModifier{},
		}

		if err := service.AddItem(r.Context(), item); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    item,
		})
	}
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	httpx.WriteError(w, statusCode, code, message)
}
