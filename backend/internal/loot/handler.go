package loot

import (
	"encoding/json"
	"net/http"
)

type apiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *apiError   `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func CreateLootTableHandler(service LootService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var lt LootTable
		if err := json.NewDecoder(r.Body).Decode(&lt); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid JSON body")
			return
		}

		if lt.ID == "" {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "Loot table ID is required")
			return
		}

		if err := service.CreateLootTable(r.Context(), &lt); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    lt,
		})
	}
}

func ListLootTablesHandler(service LootService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		list, err := service.ListLootTables(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    list,
		})
	}
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(apiResponse{
		Success: false,
		Error: &apiError{
			Code:    code,
			Message: message,
		},
	})
}
