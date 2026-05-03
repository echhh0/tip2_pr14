package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"tip2/services/auth/internal/service"
	"tip2/shared/middleware"

	"go.uber.org/zap"
)

const (
	sessionCookieName = "session"
	sessionMaxAge     = 60 * 60
	csrfCookieName    = "csrf_token"
	csrfMaxAge        = 60 * 60
)

type Handler struct {
	authService *service.AuthService
	logger      *zap.Logger
}

func New(authService *service.AuthService, logger *zap.Logger) *Handler {
	return &Handler{authService: authService, logger: logger}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	CSRFToken   string `json:"csrf_token"`
	Message     string `json:"message"`
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/login", h.Login)
	mux.HandleFunc("GET /v1/auth/verify", h.Verify)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logWarn(r, "handler", "invalid login json", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid json",
		})
		return
	}

	token, ok := h.authService.Login(req.Username, req.Password)
	if !ok {
		h.logger.Warn(
			"login failed",
			zap.String("request_id", middleware.GetRequestID(r.Context())),
			zap.String("component", "handler"),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "invalid credentials",
		})
		return
	}

	csrfToken, err := h.authService.NewCSRFToken()
	if err != nil {
		h.logger.Error(
			"generate csrf token failed",
			zap.String("request_id", middleware.GetRequestID(r.Context())),
			zap.String("component", "handler"),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Error(err),
		)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal error",
		})
		return
	}

	setSessionCookie(w, token)
	setCSRFCookie(w, csrfToken)

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		CSRFToken:   csrfToken,
		Message:     "login successful",
	})
}

func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	token, ok := extractToken(r)
	if !ok {
		h.logger.Warn(
			"missing auth token",
			zap.String("request_id", middleware.GetRequestID(r.Context())),
			zap.String("component", "handler"),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"valid": false,
			"error": "unauthorized",
		})
		return
	}

	result := h.authService.VerifyHTTP(token)
	if !result.Valid {
		h.logger.Warn(
			"token verification failed",
			zap.String("request_id", middleware.GetRequestID(r.Context())),
			zap.String("component", "handler"),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		)
		writeJSON(w, http.StatusUnauthorized, result)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) logWarn(r *http.Request, component, msg string, err error) {
	h.logger.Warn(
		msg,
		zap.String("request_id", middleware.GetRequestID(r.Context())),
		zap.String("component", component),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.Error(err),
	)
}

func extractToken(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value, true
	}

	return extractBearerToken(r.Header.Get("Authorization"))
}

func extractBearerToken(authHeader string) (string, bool) {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return "", false
	}
	if parts[0] != "Bearer" {
		return "", false
	}
	if strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAge,
		Expires:  time.Now().Add(sessionMaxAge * time.Second),
	})
}

func setCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   csrfMaxAge,
		Expires:  time.Now().Add(csrfMaxAge * time.Second),
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
