package redis

import (
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Client is the redis client type used across the app.
type Client = redis.Client

// New creates a go-redis client from a redis:// URL. This client is used for
// pub/sub (SSE progress streaming) — asynq manages its own connection pool.
func New(redisURL string) (*redis.Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return redis.NewClient(opt), nil
}

// TaskChannel is the pub/sub channel name for a given task's SSE progress
// stream. Mirrors the Python scaffold's `task_stream_{task_id}` convention.
func TaskChannel(taskID string) string {
	return "task_stream_" + taskID
}
