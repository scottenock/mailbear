package http

import (
	"encoding/json"
	"net/http"
)

type mailbearResponse struct {
	Message string `json:"message"`
}

// writeJSON writes a JSON message response with the given status code.
func writeJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(mailbearResponse{Message: message})
}
