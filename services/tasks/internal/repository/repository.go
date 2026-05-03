package repository

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("task not found")

type Task struct {
	ID          string
	Title       string
	Description string
	DueDate     *time.Time
	Done        bool
	CreatedAt   time.Time
}

type CreateTaskParams struct {
	Title       string
	Description string
	DueDate     *time.Time
}

type UpdateTaskParams struct {
	Title       string
	Description string
	DueDate     *time.Time
	Done        bool
}

type TaskRepository interface {
	Create(ctx context.Context, params CreateTaskParams) (Task, error)
	List(ctx context.Context) ([]Task, error)
	Get(ctx context.Context, id string) (Task, error)
	SearchByTitle(ctx context.Context, title string) ([]Task, error)
	Update(ctx context.Context, id string, params UpdateTaskParams) (Task, error)
	Delete(ctx context.Context, id string) error
}
