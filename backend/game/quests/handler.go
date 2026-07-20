package quests

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/singoesdeep/zzrpg/sdk/pkg/httpx"
)

// Response/error envelope types live once in pkg/httpx.
type apiResponse = httpx.Response

type apiError = httpx.Error

type acceptRequest struct {
	QuestID string `json:"quest_id"`
}

type progressUpdateRequest struct {
	CharacterID int32  `json:"character_id"`
	ActionType  string `json:"action_type"`
	Target      string `json:"target"`
	Amount      int32  `json:"amount"`
}

func CreateDefinitionHandler(service QuestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var qd QuestDefinition
		if err := json.NewDecoder(r.Body).Decode(&qd); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid JSON body")
			return
		}

		if qd.ID == "" || qd.Title == "" {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "Quest ID and Title are required")
			return
		}

		if err := service.CreateDefinition(r.Context(), &qd); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    qd,
		})
	}
}

func ListDefinitionsHandler(service QuestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		limit, offset := httpx.ParsePage(r, 50, 200)
		defs, err := service.ListDefinitions(r.Context(), limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    defs,
		})
	}
}

func AcceptQuestHandler(service QuestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
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

		var req acceptRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid JSON body")
			return
		}

		err = service.AcceptQuest(r.Context(), int32(charID), req.QuestID)
		if err != nil {
			if errors.Is(err, ErrLevelRequirement) {
				writeError(w, http.StatusForbidden, "LEVEL_RESTRICTION", err.Error())
				return
			}
			if errors.Is(err, ErrQuestAlreadyActive) || errors.Is(err, ErrQuestAlreadyCompleted) {
				writeError(w, http.StatusConflict, "QUEST_CONFLICT", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    map[string]interface{}{"quest_id": req.QuestID, "status": StatusActive},
		})
	}
}

func GetQuestLogHandler(service QuestService) http.HandlerFunc {
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

		log, err := service.GetQuestLog(r.Context(), int32(charID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    log,
		})
	}
}

func UpdateQuestProgressHandler(service QuestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}

		var req progressUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST_BODY", "Invalid JSON body")
			return
		}

		if req.Amount <= 0 {
			req.Amount = 1
		}

		err := service.UpdateQuestProgress(r.Context(), req.CharacterID, req.ActionType, req.Target, req.Amount)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{
			Success: true,
			Data:    map[string]interface{}{"updated": true},
		})
	}
}

func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	httpx.WriteError(w, statusCode, code, message)
}
