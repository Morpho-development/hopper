package share

import (
	"encoding/json"
	"net/http"

	"hopper/backend/internal/response"
)

func decodeCreateRequest(w http.ResponseWriter, r *http.Request) (createRequest, bool) {
	var req createRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "invalid JSON body")
		return createRequest{}, false
	}

	if req.ExpiresInSeconds <= 0 {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "expiresInSeconds must be greater than zero")
		return createRequest{}, false
	}
	if req.MaxDownloads != nil && *req.MaxDownloads <= 0 {
		response.WriteError(w, http.StatusBadRequest, "validation_error", "maxDownloads must be greater than zero")
		return createRequest{}, false
	}

	return req, true
}
