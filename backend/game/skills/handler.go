package skills

import (
	"encoding/json"
	"net/http"

	"github.com/singoesdeep/zzrpg/backend/pkg/httpx"
)

// ListHandler serves the skill catalogue: GET /api/v1/skills.
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(httpx.Response{
			Success: true,
			Data:    map[string]any{"skills": svc.List()},
		})
	}
}
