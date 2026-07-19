// Package httpx holds the shared HTTP response envelope used by every handler
// package, replacing the apiResponse/apiError/writeError trio that was
// previously duplicated in auth, character, inventory, items, quests and loot.
package httpx

import (
	"encoding/json"
	"net/http"
)

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
