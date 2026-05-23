package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/sourceasia/backend/internal/models"
)

// writeJSON serialises v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg, details string) {
	writeJSON(w, status, models.ErrorResponse{
		Error:   msg,
		Details: details,
	})
}
