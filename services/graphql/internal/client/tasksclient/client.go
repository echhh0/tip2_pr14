package tasksclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"tip2/services/graphql/internal/auth"
)

var ErrNotFound = errors.New("task not found")

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"`
	Done        bool   `json:"done"`
	CreatedAt   string `json:"created_at"`
}

type CreateTaskInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
}

type UpdateTaskInput struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
	Done        *bool   `json:"done,omitempty"`
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) List(ctx context.Context) ([]Task, error) {
	var result []Task

	err := c.do(ctx, http.MethodGet, "/v1/tasks", nil, http.StatusOK, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) Get(ctx context.Context, id string) (*Task, error) {
	var result Task

	err := c.do(ctx, http.MethodGet, "/v1/tasks/"+id, nil, http.StatusOK, &result)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Client) Create(ctx context.Context, input CreateTaskInput) (Task, error) {
	var result Task

	err := c.do(ctx, http.MethodPost, "/v1/tasks", input, http.StatusCreated, &result)
	if err != nil {
		return Task{}, err
	}

	return result, nil
}

func (c *Client) Update(ctx context.Context, id string, input UpdateTaskInput) (Task, error) {
	var result Task

	err := c.do(ctx, http.MethodPatch, "/v1/tasks/"+id, input, http.StatusOK, &result)
	if err != nil {
		return Task{}, err
	}

	return result, nil
}

func (c *Client) Delete(ctx context.Context, id string) (bool, error) {
	err := c.do(ctx, http.MethodDelete, "/v1/tasks/"+id, nil, http.StatusNoContent, nil)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, expectedStatus int, out any) error {
	var requestBody *bytes.Reader

	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if authorization := auth.AuthorizationFromContext(ctx); authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if requestID := auth.RequestIDFromContext(ctx); requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call tasks service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}

	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("unexpected tasks status: %d", resp.StatusCode)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode tasks response: %w", err)
	}

	return nil
}
