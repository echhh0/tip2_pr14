package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"tip2/services/tasks/internal/repository"

	"go.uber.org/zap"
)

type taskRepoStub struct {
	createFn        func(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error)
	listFn          func(ctx context.Context) ([]repository.Task, error)
	getFn           func(ctx context.Context, id string) (repository.Task, error)
	searchByTitleFn func(ctx context.Context, title string) ([]repository.Task, error)
	updateFn        func(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error)
	deleteFn        func(ctx context.Context, id string) error
}

func (s *taskRepoStub) Create(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
	return s.createFn(ctx, params)
}

func (s *taskRepoStub) List(ctx context.Context) ([]repository.Task, error) {
	return s.listFn(ctx)
}

func (s *taskRepoStub) Get(ctx context.Context, id string) (repository.Task, error) {
	return s.getFn(ctx, id)
}

func (s *taskRepoStub) SearchByTitle(ctx context.Context, title string) ([]repository.Task, error) {
	return s.searchByTitleFn(ctx, title)
}

func (s *taskRepoStub) Update(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
	return s.updateFn(ctx, id, params)
}

func (s *taskRepoStub) Delete(ctx context.Context, id string) error {
	return s.deleteFn(ctx, id)
}

type taskCacheStub struct {
	getFn    func(ctx context.Context, key string) ([]byte, bool, error)
	setFn    func(ctx context.Context, key string, value []byte) error
	deleteFn func(ctx context.Context, key string) error
}

func (s *taskCacheStub) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if s.getFn == nil {
		return nil, false, nil
	}
	return s.getFn(ctx, key)
}

func (s *taskCacheStub) Set(ctx context.Context, key string, value []byte) error {
	if s.setFn == nil {
		return nil
	}
	return s.setFn(ctx, key, value)
}

func (s *taskCacheStub) Delete(ctx context.Context, key string) error {
	if s.deleteFn == nil {
		return nil
	}
	return s.deleteFn(ctx, key)
}

func (s *taskCacheStub) Close() error { return nil }

type eventPublisherStub struct {
	published []TaskEvent
	err       error
}

func (s *eventPublisherStub) Publish(ctx context.Context, event TaskEvent) error {
	s.published = append(s.published, event)
	return s.err
}

func TestCreateSanitizesInput(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)

	repo := &taskRepoStub{
		createFn: func(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
			if params.Title != "Task title" {
				t.Fatalf("unexpected title: %q", params.Title)
			}
			if params.Description != "&lt;b&gt;unsafe&lt;/b&gt;" {
				t.Fatalf("unexpected description: %q", params.Description)
			}
			if params.DueDate == nil || params.DueDate.Format("2006-01-02") != "2026-05-01" {
				t.Fatalf("unexpected due date: %#v", params.DueDate)
			}

			return repository.Task{
				ID:          "task-1",
				Title:       params.Title,
				Description: params.Description,
				DueDate:     params.DueDate,
				Done:        false,
				CreatedAt:   now,
			}, nil
		},
		listFn: func(ctx context.Context) ([]repository.Task, error) { return nil, nil },
		getFn:  func(ctx context.Context, id string) (repository.Task, error) { return repository.Task{}, nil },
		searchByTitleFn: func(ctx context.Context, title string) ([]repository.Task, error) {
			return nil, nil
		},
		updateFn: func(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		deleteFn: func(ctx context.Context, id string) error { return nil },
	}

	svc := New(repo, &taskCacheStub{}, zap.NewNop())

	task, err := svc.Create(context.Background(), CreateTaskInput{
		Title:       "   Task title   ",
		Description: "<b>unsafe</b>",
		DueDate:     "2026-05-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if task.ID != "task-1" {
		t.Fatalf("unexpected id: %q", task.ID)
	}
	if task.Description != "&lt;b&gt;unsafe&lt;/b&gt;" {
		t.Fatalf("unexpected description in dto: %q", task.Description)
	}
	if task.DueDate != "2026-05-01" {
		t.Fatalf("unexpected due date in dto: %q", task.DueDate)
	}
}

func TestCreatePublishesTaskCreatedEvent(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	repo := &taskRepoStub{
		createFn: func(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
			return repository.Task{
				ID:          "task-1",
				Title:       params.Title,
				Description: params.Description,
				Done:        false,
				CreatedAt:   now,
			}, nil
		},
		listFn: func(ctx context.Context) ([]repository.Task, error) { return nil, nil },
		getFn:  func(ctx context.Context, id string) (repository.Task, error) { return repository.Task{}, nil },
		searchByTitleFn: func(ctx context.Context, title string) ([]repository.Task, error) {
			return nil, nil
		},
		updateFn: func(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		deleteFn: func(ctx context.Context, id string) error { return nil },
	}
	publisher := &eventPublisherStub{}

	svc := New(repo, &taskCacheStub{}, zap.NewNop(), publisher)

	_, err := svc.Create(context.Background(), CreateTaskInput{
		Title:       "Rabbit",
		Description: "publish event",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(publisher.published) != 1 {
		t.Fatalf("expected one event, got %d", len(publisher.published))
	}

	event := publisher.published[0]
	if event.Type != "task.created" {
		t.Fatalf("unexpected event type: %q", event.Type)
	}
	if event.TaskID != "task-1" || event.Title != "Rabbit" {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.CreatedAt != now.Format(time.RFC3339) {
		t.Fatalf("unexpected created_at: %q", event.CreatedAt)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	repo := &taskRepoStub{
		createFn: func(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		listFn: func(ctx context.Context) ([]repository.Task, error) { return nil, nil },
		getFn:  func(ctx context.Context, id string) (repository.Task, error) { return repository.Task{}, nil },
		searchByTitleFn: func(ctx context.Context, title string) ([]repository.Task, error) {
			return nil, nil
		},
		updateFn: func(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		deleteFn: func(ctx context.Context, id string) error { return nil },
	}

	svc := New(repo, &taskCacheStub{}, zap.NewNop())

	_, err := svc.Create(context.Background(), CreateTaskInput{
		Title: "   ",
	})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Error() != "title is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetUsesCacheAside(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	repoCalls := 0
	cacheSets := 0

	repo := &taskRepoStub{
		createFn: func(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		listFn: func(ctx context.Context) ([]repository.Task, error) { return nil, nil },
		getFn: func(ctx context.Context, id string) (repository.Task, error) {
			repoCalls++
			return repository.Task{
				ID:          id,
				Title:       "Cache me",
				Description: "Redis",
				Done:        false,
				CreatedAt:   now,
			}, nil
		},
		searchByTitleFn: func(ctx context.Context, title string) ([]repository.Task, error) { return nil, nil },
		updateFn: func(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		deleteFn: func(ctx context.Context, id string) error { return nil },
	}

	var cached []byte
	cache := &taskCacheStub{
		getFn: func(ctx context.Context, key string) ([]byte, bool, error) {
			if key != "tasks:task:task-1" {
				t.Fatalf("unexpected key: %q", key)
			}
			if cached == nil {
				return nil, false, nil
			}
			return cached, true, nil
		},
		setFn: func(ctx context.Context, key string, value []byte) error {
			cacheSets++
			cached = append([]byte(nil), value...)
			return nil
		},
	}

	svc := New(repo, cache, zap.NewNop())

	first, err := svc.Get(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("unexpected error on first get: %v", err)
	}
	second, err := svc.Get(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("unexpected error on second get: %v", err)
	}

	if repoCalls != 1 {
		t.Fatalf("expected repo to be called once, got %d", repoCalls)
	}
	if cacheSets != 1 {
		t.Fatalf("expected cache set once, got %d", cacheSets)
	}
	if first.ID != second.ID || second.Title != "Cache me" {
		t.Fatalf("unexpected cached task: %#v %#v", first, second)
	}
}

func TestGetFallsBackWhenCacheFails(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	repoCalls := 0

	repo := &taskRepoStub{
		createFn: func(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		listFn: func(ctx context.Context) ([]repository.Task, error) { return nil, nil },
		getFn: func(ctx context.Context, id string) (repository.Task, error) {
			repoCalls++
			return repository.Task{
				ID:        id,
				Title:     "From DB",
				CreatedAt: now,
			}, nil
		},
		searchByTitleFn: func(ctx context.Context, title string) ([]repository.Task, error) { return nil, nil },
		updateFn: func(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		deleteFn: func(ctx context.Context, id string) error { return nil },
	}

	cache := &taskCacheStub{
		getFn: func(ctx context.Context, key string) ([]byte, bool, error) {
			return nil, false, errors.New("redis unavailable")
		},
		setFn: func(ctx context.Context, key string, value []byte) error {
			return errors.New("redis unavailable")
		},
	}

	svc := New(repo, cache, zap.NewNop())

	task, err := svc.Get(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repoCalls != 1 {
		t.Fatalf("expected repo call, got %d", repoCalls)
	}
	if task.Title != "From DB" {
		t.Fatalf("unexpected task: %#v", task)
	}
}

func TestUpdateChangesFields(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	deletedKeys := make([]string, 0, 1)

	repo := &taskRepoStub{
		createFn: func(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
			return repository.Task{}, nil
		},
		listFn: func(ctx context.Context) ([]repository.Task, error) { return nil, nil },
		getFn: func(ctx context.Context, id string) (repository.Task, error) {
			return repository.Task{
				ID:          id,
				Title:       "Old title",
				Description: "Old description",
				Done:        false,
				CreatedAt:   now,
			}, nil
		},
		searchByTitleFn: func(ctx context.Context, title string) ([]repository.Task, error) {
			return nil, nil
		},
		updateFn: func(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
			if id != "task-1" {
				t.Fatalf("unexpected id: %q", id)
			}
			if params.Title != "New title" {
				t.Fatalf("unexpected title: %q", params.Title)
			}
			if params.Description != "&lt;script&gt;1&lt;/script&gt;" {
				t.Fatalf("unexpected description: %q", params.Description)
			}
			if !params.Done {
				t.Fatalf("expected done=true")
			}

			return repository.Task{
				ID:          id,
				Title:       params.Title,
				Description: params.Description,
				Done:        params.Done,
				CreatedAt:   now,
			}, nil
		},
		deleteFn: func(ctx context.Context, id string) error { return nil },
	}

	cache := &taskCacheStub{
		deleteFn: func(ctx context.Context, key string) error {
			deletedKeys = append(deletedKeys, key)
			return nil
		},
	}

	svc := New(repo, cache, zap.NewNop())

	title := "  New title  "
	description := "<script>1</script>"
	done := true

	task, err := svc.Update(context.Background(), "task-1", UpdateTaskInput{
		Title:       &title,
		Description: &description,
		Done:        &done,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if task.Title != "New title" {
		t.Fatalf("unexpected dto title: %q", task.Title)
	}
	if task.Description != "&lt;script&gt;1&lt;/script&gt;" {
		t.Fatalf("unexpected dto description: %q", task.Description)
	}
	if !task.Done {
		t.Fatalf("expected dto done=true")
	}
	if len(deletedKeys) != 1 || deletedKeys[0] != "tasks:task:task-1" {
		t.Fatalf("unexpected invalidation keys: %#v", deletedKeys)
	}
}
