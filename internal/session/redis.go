package session

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
	prefix string
}

func NewRedisStore(addr, password string, db int) *RedisStore {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisStore{
		client: client,
		prefix: "tokenomics:usage:",
	}
}

func (r *RedisStore) GetUsage(tokenHash string) (int64, error) {
	ctx := context.Background()
	val, err := r.client.Get(ctx, r.prefix+tokenHash).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("redis get: %w", err)
	}
	return val, nil
}

func (r *RedisStore) AddUsage(tokenHash string, count int64) (int64, error) {
	ctx := context.Background()
	val, err := r.client.IncrBy(ctx, r.prefix+tokenHash, count).Result()
	if err != nil {
		return 0, fmt.Errorf("redis incrby: %w", err)
	}
	return val, nil
}

func (r *RedisStore) Reset(tokenHash string) error {
	ctx := context.Background()
	if err := r.client.Del(ctx, r.prefix+tokenHash).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}

// Client returns the underlying Redis client for shared use (e.g., memory writers).
func (r *RedisStore) Client() *redis.Client {
	return r.client
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}
