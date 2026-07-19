package items

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/pkg/httpx"
)

// Response/error envelope types live once in pkg/httpx.
type apiResponse = httpx.Response

type apiError = httpx.Error

func CreateHandler(service ItemService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var item ItemDefinition
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid request body")
			return
		}

		if err := service.Create(r.Context(), &item); err != nil {
			if errors.Is(err, ErrItemAlreadyExists) {
				writeError(w, http.StatusConflict, "ITEM_ALREADY_EXISTS", err.Error())
				return
			}
			if errors.Is(err, ErrInvalidSlotType) || errors.Is(err, ErrInvalidModifierType) {
				writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to create item definition")
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    item,
		})
	}
}

func UpdateHandler(service ItemService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_ID", "Item ID path parameter is required")
			return
		}

		var item ItemDefinition
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid request body")
			return
		}
		item.ID = id // enforce ID from path

		if err := service.Update(r.Context(), &item); err != nil {
			if errors.Is(err, ErrItemNotFound) {
				writeError(w, http.StatusNotFound, "ITEM_NOT_FOUND", err.Error())
				return
			}
			if errors.Is(err, ErrInvalidSlotType) || errors.Is(err, ErrInvalidModifierType) {
				writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to update item definition")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    item,
		})
	}
}

func GetHandler(service ItemService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_ID", "Item ID path parameter is required")
			return
		}

		item, err := service.GetByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrItemNotFound) {
				writeError(w, http.StatusNotFound, "ITEM_NOT_FOUND", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to fetch item definition")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    item,
		})
	}
}

func ListHandler(service ItemService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		items, err := service.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to list item definitions")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    items,
		})
	}
}

func DeleteHandler(service ItemService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_ID", "Item ID path parameter is required")
			return
		}

		if err := service.Delete(r.Context(), id); err != nil {
			if errors.Is(err, ErrItemNotFound) {
				writeError(w, http.StatusNotFound, "ITEM_NOT_FOUND", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Failed to delete item definition")
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
		})
	}
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	httpx.WriteError(w, statusCode, code, message)
}
