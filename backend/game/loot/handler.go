package loot

import (
	"encoding/json"
	"net/http"

	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"
)

// Response/error envelope types live once in pkg/httpx.
type apiResponse = httpx.Response
type apiError = httpx.Error

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

		limit, offset := httpx.ParsePage(r, 50, 200)
		list, err := service.ListLootTables(r.Context(), limit, offset)
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
	httpx.WriteError(w, statusCode, code, message)
}
