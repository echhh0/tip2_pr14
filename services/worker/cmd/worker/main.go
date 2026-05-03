package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	sharedlogger "tip2/shared/logger"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

type TaskEvent struct {
	Type      string `json:"type"`
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

func main() {
	logger, err := sharedlogger.New("worker")
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	rabbitURL := getEnv("RABBIT_URL", "amqp://guest:guest@localhost:5672/")
	queueName := getEnv("QUEUE_NAME", "task_events")
	prefetch := mustInt("WORKER_PREFETCH", 1)

	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		logger.Fatal("connect rabbitmq failed", zap.String("component", "rabbitmq"), zap.Error(err))
	}
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		logger.Fatal("open rabbitmq channel failed", zap.String("component", "rabbitmq"), zap.Error(err))
	}
	defer func() { _ = ch.Close() }()

	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		logger.Fatal("declare queue failed", zap.String("component", "rabbitmq"), zap.String("queue", queueName), zap.Error(err))
	}

	if err := ch.Qos(prefetch, 0, false); err != nil {
		logger.Fatal("set prefetch failed", zap.String("component", "rabbitmq"), zap.Int("prefetch", prefetch), zap.Error(err))
	}

	deliveries, err := ch.Consume(queueName, "tasks-worker", false, false, false, false, nil)
	if err != nil {
		logger.Fatal("consume queue failed", zap.String("component", "rabbitmq"), zap.String("queue", queueName), zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("worker started",
		zap.String("component", "rabbitmq"),
		zap.String("queue", queueName),
		zap.Int("prefetch", prefetch),
	)

	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopped", zap.String("component", "rabbitmq"))
			return
		case delivery, ok := <-deliveries:
			if !ok {
				logger.Info("delivery channel closed", zap.String("component", "rabbitmq"))
				return
			}
			handleDelivery(logger, delivery)
		}
	}
}

func handleDelivery(logger *zap.Logger, delivery amqp.Delivery) {
	var event TaskEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		logger.Warn("invalid task event json",
			zap.String("component", "rabbitmq"),
			zap.ByteString("body", delivery.Body),
			zap.Error(err),
		)
		_ = delivery.Nack(false, false)
		return
	}

	logger.Info("received task event",
		zap.String("component", "rabbitmq"),
		zap.String("event_type", event.Type),
		zap.String("task_id", event.TaskID),
		zap.String("title", event.Title),
		zap.String("created_at", event.CreatedAt),
	)

	if err := delivery.Ack(false); err != nil {
		logger.Warn("ack task event failed",
			zap.String("component", "rabbitmq"),
			zap.String("task_id", event.TaskID),
			zap.Error(err),
		)
	}
}

func getEnv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
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
