package cache

import "context"

type TaskCache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Close() error
}

type NoopCache struct{}

func NewNoop() *NoopCache {
	return &NoopCache{}
}

func (c *NoopCache) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}

func (c *NoopCache) Set(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (c *NoopCache) Delete(_ context.Context, _ string) error {
	return nil
}

func (c *NoopCache) Close() error {
	return nil
}
