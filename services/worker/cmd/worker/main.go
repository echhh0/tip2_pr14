package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

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

type ProcessTaskJob struct {
	Job       string `json:"job"`
	TaskID    string `json:"task_id"`
	Attempt   int    `json:"attempt"`
	MessageID string `json:"message_id"`
}

type processedStore struct {
	ids map[string]struct{}
}

func newProcessedStore() *processedStore {
	return &processedStore{ids: make(map[string]struct{})}
}

func (s *processedStore) Exists(id string) bool {
	_, ok := s.ids[id]
	return ok
}

func (s *processedStore) Mark(id string) {
	s.ids[id] = struct{}{}
}

type processor struct {
	logger        *zap.Logger
	channel       *amqp.Channel
	queueName     string
	maxAttempts   int
	processingMin time.Duration
	processingMax time.Duration
	processedIDs  *processedStore
}

func main() {
	logger, err := sharedlogger.New("worker")
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	rabbitURL := getEnv("RABBIT_URL", "amqp://guest:guest@localhost:5672/")
	queueName := getEnv("QUEUE_NAME", "task_jobs")
	dlxName := getEnv("DLX_NAME", "task_jobs_dlx")
	dlqName := getEnv("DLQ_NAME", "task_jobs_dlq")
	prefetch := mustInt("WORKER_PREFETCH", 1)
	maxAttempts := mustInt("MAX_ATTEMPTS", 3)
	processingMin := time.Duration(mustInt("PROCESSING_MIN_MS", 2000)) * time.Millisecond
	processingMax := time.Duration(mustInt("PROCESSING_MAX_MS", 5000)) * time.Millisecond

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

	if dlxName != "" {
		if err := ch.ExchangeDeclare(dlxName, "direct", true, false, false, false, nil); err != nil {
			logger.Fatal("declare dlx failed", zap.String("component", "rabbitmq"), zap.String("dlx", dlxName), zap.Error(err))
		}
	}
	if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
		logger.Fatal("declare dlq failed", zap.String("component", "rabbitmq"), zap.String("dlq", dlqName), zap.Error(err))
	}
	if dlxName != "" {
		if err := ch.QueueBind(dlqName, dlqName, dlxName, false, nil); err != nil {
			logger.Fatal("bind dlq failed", zap.String("component", "rabbitmq"), zap.String("dlx", dlxName), zap.String("dlq", dlqName), zap.Error(err))
		}
	}

	queueArgs := amqp.Table{"x-dead-letter-routing-key": dlqName}
	if dlxName != "" {
		queueArgs["x-dead-letter-exchange"] = dlxName
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, queueArgs); err != nil {
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
		zap.String("dlq", dlqName),
		zap.Int("prefetch", prefetch),
		zap.Int("max_attempts", maxAttempts),
	)

	worker := &processor{
		logger:        logger,
		channel:       ch,
		queueName:     queueName,
		maxAttempts:   maxAttempts,
		processingMin: processingMin,
		processingMax: processingMax,
		processedIDs:  newProcessedStore(),
	}

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
			worker.handleDelivery(delivery)
		}
	}
}

func (p *processor) handleDelivery(delivery amqp.Delivery) {
	var job ProcessTaskJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		p.logger.Warn("invalid job json",
			zap.String("component", "rabbitmq"),
			zap.ByteString("body", delivery.Body),
			zap.Error(err),
		)
		_ = delivery.Nack(false, false)
		return
	}

	if job.Job == "" {
		p.handleTaskEvent(delivery)
		return
	}

	if job.Job != "process_task" || strings.TrimSpace(job.TaskID) == "" || strings.TrimSpace(job.MessageID) == "" {
		p.logger.Warn("invalid process task job",
			zap.String("component", "rabbitmq"),
			zap.Any("job", job),
		)
		_ = delivery.Nack(false, false)
		return
	}

	if p.processedIDs.Exists(job.MessageID) {
		p.logger.Info("duplicate job ignored",
			zap.String("component", "worker"),
			zap.String("task_id", job.TaskID),
			zap.String("message_id", job.MessageID),
		)
		_ = delivery.Ack(false)
		return
	}

	p.logger.Info("process task job started",
		zap.String("component", "worker"),
		zap.String("task_id", job.TaskID),
		zap.Int("attempt", job.Attempt),
		zap.String("message_id", job.MessageID),
	)

	if err := p.process(job); err != nil {
		p.logger.Warn("process task job failed",
			zap.String("component", "worker"),
			zap.String("task_id", job.TaskID),
			zap.Int("attempt", job.Attempt),
			zap.String("message_id", job.MessageID),
			zap.Error(err),
		)

		if job.Attempt >= p.maxAttempts {
			p.logger.Warn("max attempts reached, move job to dlq",
				zap.String("component", "worker"),
				zap.String("task_id", job.TaskID),
				zap.Int("attempt", job.Attempt),
				zap.String("message_id", job.MessageID),
			)
			_ = delivery.Nack(false, false)
			return
		}

		job.Attempt++
		if err := p.publishRetry(job); err != nil {
			p.logger.Error("publish retry failed",
				zap.String("component", "worker"),
				zap.String("task_id", job.TaskID),
				zap.Int("attempt", job.Attempt),
				zap.String("message_id", job.MessageID),
				zap.Error(err),
			)
			_ = delivery.Nack(false, true)
			return
		}

		p.logger.Info("retry published",
			zap.String("component", "worker"),
			zap.String("task_id", job.TaskID),
			zap.Int("next_attempt", job.Attempt),
			zap.String("message_id", job.MessageID),
		)
		_ = delivery.Ack(false)
		return
	}

	p.processedIDs.Mark(job.MessageID)
	p.logger.Info("process task job completed",
		zap.String("component", "worker"),
		zap.String("task_id", job.TaskID),
		zap.Int("attempt", job.Attempt),
		zap.String("message_id", job.MessageID),
	)
	_ = delivery.Ack(false)
}

func (p *processor) handleTaskEvent(delivery amqp.Delivery) {
	var event TaskEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		p.logger.Warn("invalid task event json",
			zap.String("component", "rabbitmq"),
			zap.ByteString("body", delivery.Body),
			zap.Error(err),
		)
		_ = delivery.Nack(false, false)
		return
	}

	p.logger.Info("received task event",
		zap.String("component", "rabbitmq"),
		zap.String("event_type", event.Type),
		zap.String("task_id", event.TaskID),
		zap.String("title", event.Title),
		zap.String("created_at", event.CreatedAt),
	)

	if err := delivery.Ack(false); err != nil {
		p.logger.Warn("ack task event failed",
			zap.String("component", "rabbitmq"),
			zap.String("task_id", event.TaskID),
			zap.Error(err),
		)
	}
}

func (p *processor) process(job ProcessTaskJob) error {
	sleepFor := p.processingMin
	if p.processingMax > p.processingMin {
		delta := int(p.processingMax - p.processingMin)
		sleepFor += time.Duration(rand.Intn(delta + 1))
	}
	time.Sleep(sleepFor)

	switch {
	case job.TaskID == "t_fail":
		return errors.New("simulated permanent failure")
	case job.TaskID == "t_flaky" && job.Attempt < 2:
		return errors.New("simulated transient failure")
	case strings.HasSuffix(job.TaskID, "3"):
		return errors.New("simulated deterministic failure for ids ending with 3")
	default:
		return nil
	}
}

func (p *processor) publishRetry(job ProcessTaskJob) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return p.channel.PublishWithContext(ctx, "", p.queueName, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Type:         job.Job,
		MessageId:    job.MessageID,
		Body:         body,
	})
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
