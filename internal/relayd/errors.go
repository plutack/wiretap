package relayd

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/plutack/wiretap/internal/api"
)

// writeJSON serializes v as JSON with status. Used by every handler so the
// response shape is uniform and content-type is always correct.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr writes an api.ErrorResponse body with status. code is a stable
// machine-readable string the typed client can switch on.
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, api.ErrorResponse{Code: code, Message: msg})
}

// apiError returns an *api.Error-shaped sentinel for handlers that branch on
// store results. Sentinels carry only the wire fields; the wire-level status
// is added when the error is converted by the caller via writeErr.
//
// We use *api.Error for the storage layer translations because the typed
// HTTP client already understands errors.As(*api.Error). Handler code can
// propagate these up and writeErr with the embedded status below.
func apiError(status int, code, msg string) error {
	return &api.Error{Status: status, ErrorResponse: api.ErrorResponse{Code: code, Message: msg}}
}

// errStatus unwraps an *api.Error preserving the embedded Status, falling
// back to 500 for unknown error types. Used to convert layered sentinel
// errors into a wire status code.
func errStatus(err error) (status int, code, msg string) {
	var e *api.Error
	if errors.As(err, &e) {
		return e.Status, e.Code, e.Message
	}
	return http.StatusInternalServerError, "", err.Error()
}
