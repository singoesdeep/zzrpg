// Package httpx holds the shared HTTP response envelope used by every handler
// package, replacing the apiResponse/apiError/writeError trio that was
// previously duplicated in auth, character, inventory, items, quests and loot.
package httpx

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// ParsePage reads ?limit= and ?offset= query parameters, applying defaultLimit
// when absent/invalid and clamping to maxLimit. offset defaults to 0. It gives
// list endpoints a bounded result set instead of returning an entire table.
func ParsePage(r *http.Request, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// Response is the standard JSON envelope. Exactly one of Data/Error is set.
type Response struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Error is the error detail carried by a failed Response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes a success envelope with the given status and payload.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Success: true, Data: data})
}

// WriteError writes a failure envelope with a machine code and human message.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{Success: false, Error: &Error{Code: code, Message: message}})
}
