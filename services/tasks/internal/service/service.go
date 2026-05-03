package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"tip2/services/tasks/internal/cache"
	"tip2/services/tasks/internal/repository"

	"go.uber.org/zap"
)

var ErrNotFound = repository.ErrNotFound

type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
	Done        bool   `json:"done"`
	CreatedAt   string `json:"created_at"`
}

type CreateTaskInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"`
}

type UpdateTaskInput struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
	Done        *bool   `json:"done,omitempty"`
}

type TaskEvent struct {
	Type      string `json:"type"`
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

type EventPublisher interface {
	Publish(ctx context.Context, event TaskEvent) error
}

type TaskService struct {
	repo      repository.TaskRepository
	cache     cache.TaskCache
	publisher EventPublisher
	logger    *zap.Logger
}

func New(repo repository.TaskRepository, taskCache cache.TaskCache, logger *zap.Logger, publishers ...EventPublisher) *TaskService {
	if taskCache == nil {
		taskCache = cache.NewNoop()
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	var publisher EventPublisher
	if len(publishers) > 0 {
		publisher = publishers[0]
	}

	return &TaskService{
		repo:      repo,
		cache:     taskCache,
		publisher: publisher,
		logger:    logger,
	}
}

func (s *TaskService) Create(ctx context.Context, input CreateTaskInput) (Task, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Task{}, errors.New("title is required")
	}

	dueDate, err := parseOptionalDate(input.DueDate)
	if err != nil {
		return Task{}, err
	}

	task, err := s.repo.Create(ctx, repository.CreateTaskParams{
		Title:       title,
		Description: sanitizePlainText(input.Description),
		DueDate:     dueDate,
	})
	if err != nil {
		return Task{}, err
	}

	dto := toTaskDTO(task)
	s.publishTaskCreated(ctx, dto)

	return dto, nil
}

func (s *TaskService) List(ctx context.Context) ([]Task, error) {
	tasks, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	return toTaskDTOList(tasks), nil
}

func (s *TaskService) SearchByTitle(ctx context.Context, title string) ([]Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("title query parameter is required")
	}

	tasks, err := s.repo.SearchByTitle(ctx, title)
	if err != nil {
		return nil, err
	}

	return toTaskDTOList(tasks), nil
}

func (s *TaskService) Get(ctx context.Context, id string) (Task, error) {
	cacheKey := taskCacheKey(id)

	cachedValue, found, err := s.cache.Get(ctx, cacheKey)
	if err != nil {
		s.logger.Warn("redis get failed",
			zap.String("component", "cache"),
			zap.String("key", cacheKey),
			zap.Error(err),
		)
	} else if found {
		var cached Task
		if err := json.Unmarshal(cachedValue, &cached); err != nil {
			s.logger.Warn("redis decode failed",
				zap.String("component", "cache"),
				zap.String("key", cacheKey),
				zap.Error(err),
			)
		} else {
			s.logger.Info("task cache hit",
				zap.String("component", "cache"),
				zap.String("key", cacheKey),
			)
			return cached, nil
		}
	} else {
		s.logger.Info("task cache miss",
			zap.String("component", "cache"),
			zap.String("key", cacheKey),
		)
	}

	task, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}

	dto := toTaskDTO(task)

	encoded, err := json.Marshal(dto)
	if err != nil {
		s.logger.Warn("task cache encode failed",
			zap.String("component", "cache"),
			zap.String("key", cacheKey),
			zap.Error(err),
		)
		return dto, nil
	}

	if err := s.cache.Set(ctx, cacheKey, encoded); err != nil {
		s.logger.Warn("redis set failed",
			zap.String("component", "cache"),
			zap.String("key", cacheKey),
			zap.Error(err),
		)
	}

	return dto, nil
}

func (s *TaskService) Update(ctx context.Context, id string, input UpdateTaskInput) (Task, error) {
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}

	if input.Title != nil {
		current.Title = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		current.Description = sanitizePlainText(*input.Description)
	}
	if input.DueDate != nil {
		dueDate, err := parseOptionalDate(*input.DueDate)
		if err != nil {
			return Task{}, err
		}
		current.DueDate = dueDate
	}
	if input.Done != nil {
		current.Done = *input.Done
	}

	if strings.TrimSpace(current.Title) == "" {
		return Task{}, errors.New("title is required")
	}

	updated, err := s.repo.Update(ctx, id, repository.UpdateTaskParams{
		Title:       current.Title,
		Description: current.Description,
		DueDate:     current.DueDate,
		Done:        current.Done,
	})
	if err != nil {
		return Task{}, err
	}

	if err := s.cache.Delete(ctx, taskCacheKey(id)); err != nil {
		s.logger.Warn("redis delete after update failed",
			zap.String("component", "cache"),
			zap.String("key", taskCacheKey(id)),
			zap.Error(err),
		)
	}

	return toTaskDTO(updated), nil
}

func (s *TaskService) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	if err := s.cache.Delete(ctx, taskCacheKey(id)); err != nil {
		s.logger.Warn("redis delete after delete failed",
			zap.String("component", "cache"),
			zap.String("key", taskCacheKey(id)),
			zap.Error(err),
		)
	}

	return nil
}

func (s *TaskService) publishTaskCreated(ctx context.Context, task Task) {
	if s.publisher == nil {
		return
	}

	err := s.publisher.Publish(ctx, TaskEvent{
		Type:      "task.created",
		TaskID:    task.ID,
		Title:     task.Title,
		CreatedAt: task.CreatedAt,
	})
	if err != nil {
		s.logger.Warn("publish task event failed",
			zap.String("component", "events"),
			zap.String("task_id", task.ID),
			zap.Error(err),
		)
	}
}

func parseOptionalDate(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, errors.New("due_date must be in YYYY-MM-DD format")
	}

	return &parsed, nil
}

func sanitizePlainText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return html.EscapeString(value)
}

func toTaskDTO(task repository.Task) Task {
	result := Task{
		ID:          task.ID,
		Title:       task.Title,
		Description: task.Description,
		Done:        task.Done,
		CreatedAt:   task.CreatedAt.UTC().Format(time.RFC3339),
	}

	if task.DueDate != nil {
		result.DueDate = task.DueDate.UTC().Format("2006-01-02")
	}

	return result
}

func toTaskDTOList(tasks []repository.Task) []Task {
	result := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, toTaskDTO(task))
	}
	return result
}

func taskCacheKey(id string) string {
	return fmt.Sprintf("tasks:task:%s", id)
}
