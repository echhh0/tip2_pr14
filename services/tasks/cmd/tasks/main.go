package main

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"tip2/services/tasks/internal/cache"
	"tip2/services/tasks/internal/client/authclient"
	"tip2/services/tasks/internal/events"
	httpapi "tip2/services/tasks/internal/http"
	"tip2/services/tasks/internal/service"
	"tip2/services/tasks/internal/storage/postgres"
	sharedlogger "tip2/shared/logger"
	"tip2/shared/metrics"
	"tip2/shared/middleware"

	"go.uber.org/zap"
)

func main() {
	port := getEnv("TASKS_PORT", "8082")
	authGRPCAddr := getEnv("AUTH_GRPC_ADDR", "localhost:50051")
	dbDSN := getEnv("TASKS_DB_DSN", "postgres://tasks:tasks@localhost:5432/tasksdb?sslmode=disable")
	instanceID := getEnv("INSTANCE_ID", "tasks-single")

	logger, err := sharedlogger.New("tasks")
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	db, err := postgres.Open(dbDSN)
	if err != nil {
		logger.Fatal("open postgres failed", zap.Error(err), zap.String("component", "postgres"))
	}
	defer func() { _ = db.Close() }()

	taskRepo := postgres.New(db)
	taskCache := newTaskCache(logger)
	defer func() { _ = taskCache.Close() }()

	eventPublisher := newEventPublisher(logger)
	defer func() { _ = eventPublisher.Close() }()

	taskService := service.New(taskRepo, taskCache, logger, eventPublisher)

	authClient, err := authclient.New(authGRPCAddr, logger)
	if err != nil {
		logger.Fatal("create auth client failed", zap.Error(err), zap.String("component", "auth_client"))
	}
	defer func(authClient *authclient.Client) {
		err := authClient.Close()
		if err != nil {
			panic(err)
		}
	}(authClient)

	handler := httpapi.New(taskService, authClient, logger, instanceID)

	mux := http.NewServeMux()
	handler.Register(mux)
	mux.Handle("GET /metrics", metrics.Handler())

	app := middleware.RequestID(
		middleware.SecurityHeaders(
			metrics.InstrumentHTTP(
				middleware.RequireDoubleSubmitCSRF(
					middleware.AccessLog(logger)(mux),
				),
			),
		),
	)

	addr := ":" + port
	logger.Info(
		"tasks service starting",
		zap.String("address", addr),
		zap.String("auth_grpc_addr", authGRPCAddr),
		zap.String("instance_id", instanceID),
	)

	if err := http.ListenAndServe(addr, app); err != nil {
		logger.Fatal("tasks service failed", zap.Error(err), zap.String("component", "http_server"))
	}
}

type closeablePublisher interface {
	service.EventPublisher
	Close() error
}

func newEventPublisher(logger *zap.Logger) closeablePublisher {
	rabbitURL := strings.TrimSpace(getEnv("RABBIT_URL", ""))
	if rabbitURL == "" {
		logger.Warn("rabbitmq publisher disabled: RABBIT_URL is empty", zap.String("component", "events"))
		return noopEventPublisher{}
	}

	queueName := getEnv("QUEUE_NAME", "task_events")
	dlxName := getEnv("DLX_NAME", "task_jobs_dlx")
	dlqName := getEnv("DLQ_NAME", "task_jobs_dlq")
	publisher, err := events.NewRabbitPublisher(rabbitURL, queueName, dlxName, dlqName)
	if err != nil {
		logger.Warn("rabbitmq publisher unavailable",
			zap.String("component", "events"),
			zap.String("queue", queueName),
			zap.Error(err),
		)
		return noopEventPublisher{}
	}

	logger.Info("rabbitmq publisher enabled",
		zap.String("component", "events"),
		zap.String("queue", queueName),
		zap.String("dlx", dlxName),
		zap.String("dlq", dlqName),
	)
	return publisher
}

type noopEventPublisher struct{}

func (noopEventPublisher) Publish(context.Context, service.TaskEvent) error { return nil }
func (noopEventPublisher) Close() error                                     { return nil }

func newTaskCache(logger *zap.Logger) cache.TaskCache {
	addrs := splitCSV(getEnv("REDIS_ADDRS", "redis-cluster:7000,redis-cluster:7001,redis-cluster:7002,redis-cluster:7003,redis-cluster:7004,redis-cluster:7005"))
	if len(addrs) == 0 {
		logger.Warn("redis cache disabled: REDIS_ADDRS is empty", zap.String("component", "cache"))
		return cache.NewNoop()
	}

	ttl := mustDurationFromSeconds("CACHE_TTL_SECONDS", 120)
	jitter := mustDurationFromSeconds("CACHE_TTL_JITTER_SECONDS", 30)
	opTimeout := mustDurationFromMilliseconds("REDIS_OP_TIMEOUT_MS", 300)
	initTimeout := mustDurationFromMilliseconds("REDIS_INIT_TIMEOUT_MS", 1500)

	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	redisCache, err := cache.NewRedis(ctx, cache.RedisConfig{
		Addrs:    addrs,
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       mustInt("REDIS_DB", 0),
		TTL:      ttl,
		Jitter:   jitter,
		Timeout:  opTimeout,
	})
	if err != nil {
		logger.Warn("redis cache unavailable, fallback to database only",
			zap.String("component", "cache"),
			zap.Strings("redis_addrs", addrs),
			zap.Error(err),
		)
		return cache.NewNoop()
	}

	logger.Info("redis cache enabled",
		zap.String("component", "cache"),
		zap.Strings("redis_addrs", addrs),
		zap.Duration("ttl", ttl),
		zap.Duration("jitter", jitter),
	)

	return redisCache
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func mustInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func mustDurationFromSeconds(key string, fallback int) time.Duration {
	return time.Duration(mustInt(key, fallback)) * time.Second
}

func mustDurationFromMilliseconds(key string, fallback int) time.Duration {
	return time.Duration(mustInt(key, fallback)) * time.Millisecond
}
