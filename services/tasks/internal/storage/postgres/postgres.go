package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"tip2/services/tasks/internal/repository"

	_ "github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return db, nil
}

func (r *Repository) Create(ctx context.Context, params repository.CreateTaskParams) (repository.Task, error) {
	const query = `
		INSERT INTO tasks (title, description, due_date, done)
		VALUES ($1, $2, $3, false)
		RETURNING id, title, description, due_date, done, created_at
	`

	task, err := scanTask(r.db.QueryRowContext(ctx, query, params.Title, params.Description, params.DueDate))
	if err != nil {
		return repository.Task{}, fmt.Errorf("insert task: %w", err)
	}

	return task, nil
}

func (r *Repository) List(ctx context.Context) ([]repository.Task, error) {
	const query = `
		SELECT id, title, description, due_date, done, created_at
		FROM tasks
		ORDER BY created_at DESC, id DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			panic(err)
		}
	}(rows)

	return readTasks(rows)
}

func (r *Repository) Get(ctx context.Context, id string) (repository.Task, error) {
	const query = `
		SELECT id, title, description, due_date, done, created_at
		FROM tasks
		WHERE id = $1
	`

	task, err := scanTask(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.Task{}, repository.ErrNotFound
		}
		return repository.Task{}, fmt.Errorf("get task: %w", err)
	}

	return task, nil
}

func (r *Repository) SearchByTitle(ctx context.Context, title string) ([]repository.Task, error) {
	const query = `
		SELECT id, title, description, due_date, done, created_at
		FROM tasks
		WHERE title = $1
		ORDER BY created_at DESC, id DESC
	`

	rows, err := r.db.QueryContext(ctx, query, title)
	if err != nil {
		return nil, fmt.Errorf("search tasks by title: %w", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			panic(err)
		}
	}(rows)

	return readTasks(rows)
}

func (r *Repository) Update(ctx context.Context, id string, params repository.UpdateTaskParams) (repository.Task, error) {
	const query = `
		UPDATE tasks
		SET title = $2,
			description = $3,
			due_date = $4,
			done = $5
		WHERE id = $1
		RETURNING id, title, description, due_date, done, created_at
	`

	task, err := scanTask(r.db.QueryRowContext(ctx, query, id, params.Title, params.Description, params.DueDate, params.Done))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repository.Task{}, repository.ErrNotFound
		}
		return repository.Task{}, fmt.Errorf("update task: %w", err)
	}

	return task, nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM tasks WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete task rows affected: %w", err)
	}

	if affected == 0 {
		return repository.ErrNotFound
	}

	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (repository.Task, error) {
	var task repository.Task
	var dueDate sql.NullTime

	if err := row.Scan(
		&task.ID,
		&task.Title,
		&task.Description,
		&dueDate,
		&task.Done,
		&task.CreatedAt,
	); err != nil {
		return repository.Task{}, err
	}

	if dueDate.Valid {
		task.DueDate = &dueDate.Time
	}

	return task, nil
}

func readTasks(rows *sql.Rows) ([]repository.Task, error) {
	tasks := make([]repository.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task rows: %w", err)
	}

	return tasks, nil
}
