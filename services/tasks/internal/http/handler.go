package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"tip2/services/tasks/internal/client/authclient"
	"tip2/services/tasks/internal/service"
	"tip2/shared/middleware"

	"go.uber.org/zap"
)

const (
	sessionCookieName = "session"
	instanceHeader    = "X-Instance-ID"
)

type Handler struct {
	taskService *service.TaskService
	authClient  *authclient.Client
	logger      *zap.Logger
	instanceID  string
}

func New(taskService *service.TaskService, authClient *authclient.Client, logger *zap.Logger, instanceID string) *Handler {
	return &Handler{
		taskService: taskService,
		authClient:  authClient,
		logger:      logger,
		instanceID:  instanceID,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("POST /v1/tasks", h.withInstanceHeader(h.withAuth(h.CreateTask)))
	mux.HandleFunc("POST /v1/jobs/process-task", h.withInstanceHeader(h.withAuth(h.ProcessTaskJob)))
	mux.HandleFunc("GET /v1/tasks", h.withInstanceHeader(h.withAuth(h.ListTasks)))
	mux.HandleFunc("GET /v1/tasks/search", h.withInstanceHeader(h.withAuth(h.SearchTasks)))
	mux.HandleFunc("GET /v1/tasks/{id}", h.withInstanceHeader(h.withAuth(h.GetTask)))
	mux.HandleFunc("PATCH /v1/tasks/{id}", h.withInstanceHeader(h.withAuth(h.UpdateTask)))
	mux.HandleFunc("DELETE /v1/tasks/{id}", h.withInstanceHeader(h.withAuth(h.DeleteTask)))
}

func (h *Handler) withInstanceHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(instanceHeader, h.instanceID)
		next(w, r)
	}
}

func (h *Handler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(instanceHeader, h.instanceID)

		token, ok := extractToken(r)
		if !ok {
			h.logger.Warn(
				"missing auth token",
				zap.String("request_id", middleware.GetRequestID(r.Context())),
				zap.String("component", "handler"),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("instance_id", h.instanceID),
			)
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
			return
		}

		ok, status, err := h.authClient.Verify(r.Context(), token)
		if err != nil {
			h.logger.Error(
				"auth verification failed",
				zap.String("request_id", middleware.GetRequestID(r.Context())),
				zap.String("component", "auth_client"),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("instance_id", h.instanceID),
				zap.Error(err),
			)
			writeJSON(w, status, map[string]string{
				"error": "auth service unavailable",
			})
			return
		}

		if !ok {
			h.logger.Warn(
				"auth verification rejected",
				zap.String("request_id", middleware.GetRequestID(r.Context())),
				zap.String("component", "auth_client"),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("instance_id", h.instanceID),
			)
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
			return
		}

		next(w, r)
	}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(instanceHeader, h.instanceID)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":      "ok",
		"service":     "tasks",
		"instance_id": h.instanceID,
	})
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logWarn(r, "handler", "invalid create task json", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid json",
		})
		return
	}

	task, err := h.taskService.Create(r.Context(), req)
	if err != nil {
		h.handleServiceError(w, r, "create task failed", err)
		return
	}

	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) ProcessTaskJob(w http.ResponseWriter, r *http.Request) {
	var req service.ProcessTaskInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logWarn(r, "handler", "invalid process task job json", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid json",
		})
		return
	}

	job, err := h.taskService.ProcessTask(r.Context(), req)
	if err != nil {
		h.handleServiceError(w, r, "publish process task job failed", err)
		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.taskService.List(r.Context())
	if err != nil {
		h.handleInternalError(w, r, "list tasks failed", err)
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) SearchTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.taskService.SearchByTitle(r.Context(), r.URL.Query().Get("title"))
	if err != nil {
		h.handleServiceError(w, r, "search tasks failed", err)
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	task, err := h.taskService.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			h.logWarn(r, "handler", "task not found", err)
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "task not found",
			})
			return
		}

		h.handleInternalError(w, r, "get task failed", err)
		return
	}

	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req service.UpdateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logWarn(r, "handler", "invalid update task json", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid json",
		})
		return
	}

	task, err := h.taskService.Update(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			h.logWarn(r, "handler", "task not found", err)
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "task not found",
			})
			return
		}

		h.handleServiceError(w, r, "update task failed", err)
		return
	}

	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	err := h.taskService.Delete(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			h.logWarn(r, "handler", "task not found", err)
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "task not found",
			})
			return
		}

		h.handleInternalError(w, r, "delete task failed", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleServiceError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if isValidationError(err) {
		h.logWarn(r, "handler", msg, err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}

	h.handleInternalError(w, r, msg, err)
}

func (h *Handler) handleInternalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	h.logError(r, "handler", msg, err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{
		"error": "internal error",
	})
}

func (h *Handler) logWarn(r *http.Request, component, msg string, err error) {
	h.logger.Warn(
		msg,
		zap.String("request_id", middleware.GetRequestID(r.Context())),
		zap.String("component", component),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("instance_id", h.instanceID),
		zap.Error(err),
	)
}

func (h *Handler) logError(r *http.Request, component, msg string, err error) {
	h.logger.Error(
		msg,
		zap.String("request_id", middleware.GetRequestID(r.Context())),
		zap.String("component", component),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("instance_id", h.instanceID),
		zap.Error(err),
	)
}

func isValidationError(err error) bool {
	return strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "format")
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

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
