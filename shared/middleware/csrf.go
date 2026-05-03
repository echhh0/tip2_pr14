package middleware

import (
	"net/http"
)

const (
	HeaderCSRFToken = "X-CSRF-Token"
	CookieCSRFToken = "csrf_token"
)

func RequireDoubleSubmitCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requiresCSRFFCheck(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// CSRF актуален только для cookie-based авторизации.
		if _, err := r.Cookie("session"); err != nil {
			next.ServeHTTP(w, r)
			return
		}

		csrfCookie, err := r.Cookie(CookieCSRFToken)
		if err != nil || csrfCookie.Value == "" {
			writeCSRFError(w)
			return
		}

		csrfHeader := r.Header.Get(HeaderCSRFToken)
		if csrfHeader == "" || csrfHeader != csrfCookie.Value {
			writeCSRFError(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func requiresCSRFFCheck(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func writeCSRFError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"csrf token mismatch"}`))
}
