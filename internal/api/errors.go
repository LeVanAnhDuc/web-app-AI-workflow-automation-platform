package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// errBody is the single error envelope every failing endpoint returns, so the
// frontend has one shape to branch on.
type errBody struct {
	Error errPayload `json:"error"`
}

type errPayload struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("api: encode response", "error", err)
	}
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errBody{Error: errPayload{Code: code, Message: message}})
}

func writeErrDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, errBody{Error: errPayload{Code: code, Message: message, Details: details}})
}

// writeStoreErr maps the repository's sentinel errors onto status codes so
// handlers do not each repeat the same three checks.
func writeStoreErr(w http.ResponseWriter, err error, notFoundMessage string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", notFoundMessage)
	case errors.Is(err, domain.ErrConflict):
		writeErr(w, http.StatusConflict, "conflict", err.Error())
	default:
		slog.Default().Error("api: unexpected store error", "error", err)
		writeErr(w, http.StatusInternalServerError, domain.ErrCodeInternal, "Something went wrong on our side.")
	}
}

// decodeJSON reads a JSON body with a size limit, reporting a 400 itself so
// handlers can simply return when it fails.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	const maxBody = 4 << 20 // 4 MiB: a large graph, not an upload

	body := http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		switch {
		case errors.Is(err, io.EOF):
			writeErr(w, http.StatusBadRequest, "invalid_body", "A JSON body is required.")
		default:
			writeErr(w, http.StatusBadRequest, "invalid_body", "The request body could not be read: "+err.Error())
		}
		return false
	}
	return true
}
