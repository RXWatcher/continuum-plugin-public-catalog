package server

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr returns a stable error envelope that callers can pattern on.
// Use when the client legitimately needs to know what went wrong
// (validation, missing resource, auth) — never pass through internal
// driver-level error messages.
func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// writeInternal logs the underlying error server-side (with request
// method + path for triage) and returns an opaque 500 with a stable
// code. Use this for any unexpected DB / SDK failure instead of leaking
// err.Error() to the wire.
func writeInternal(w http.ResponseWriter, r *http.Request, d Deps, code string, err error) {
	if d.Logger != nil {
		d.Logger.Error("public-catalog: internal error",
			"method", r.Method, "path", r.URL.Path, "code", code, "err", err)
	}
	writeErr(w, http.StatusInternalServerError, code, "internal error")
}
