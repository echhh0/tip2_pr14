package cache

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisConfig struct {
	Addrs    []string
	Password string
	DB       int
	TTL      time.Duration
	Jitter   time.Duration
	Timeout  time.Duration
	Seed     int64
}

type RedisCache struct {
	client  redis.UniversalClient
	ttl     time.Duration
	jitter  time.Duration
	timeout time.Duration
	rand    *rand.Rand
	mu      sync.Mutex
}

func NewRedis(ctx context.Context, cfg RedisConfig) (*RedisCache, error) {
	addrs := make([]string, 0, len(cfg.Addrs))
	for _, addr := range cfg.Addrs {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			addrs = append(addrs, addr)
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("redis addrs are empty")
	}
	if cfg.TTL <= 0 {
		return nil, fmt.Errorf("redis ttl must be positive")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 300 * time.Millisecond
	}
	if cfg.Seed == 0 {
		cfg.Seed = time.Now().UnixNano()
	}

	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:        addrs,
		Password:     cfg.Password,
		DB:           cfg.DB,
		ReadTimeout:  cfg.Timeout,
		WriteTimeout: cfg.Timeout,
		DialTimeout:  cfg.Timeout,
		PoolTimeout:  cfg.Timeout,
		MaxRetries:   1,
	})

	pingCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &RedisCache{
		client:  client,
		ttl:     cfg.TTL,
		jitter:  cfg.Jitter,
		timeout: cfg.Timeout,
		rand:    rand.New(rand.NewSource(cfg.Seed)),
	}, nil
}

func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	opCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	value, err := c.client.Get(opCtx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}

	return value, true, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, value []byte) error {
	opCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	return c.client.Set(opCtx, key, value, c.ttlWithJitter()).Err()
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	opCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	return c.client.Del(opCtx, key).Err()
}

func (c *RedisCache) Close() error {
	return c.client.Close()
}

func (c *RedisCache) ttlWithJitter() time.Duration {
	if c.jitter <= 0 {
		return c.ttl
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.ttl + time.Duration(c.rand.Int63n(int64(c.jitter)+1))
}
