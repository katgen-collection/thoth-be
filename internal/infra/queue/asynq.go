package queue

import (
	"fmt"

	"github.com/hibiken/asynq"
)

// Task type names. Each maps to a worker handler.
const (
	TypeSearchPipeline = "search:pipeline"
	TypeCVProcess      = "cv:process"
)

// RedisOpt parses a redis:// URL into an asynq connection option.
func RedisOpt(redisURL string) (asynq.RedisConnOpt, error) {
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis uri for asynq: %w", err)
	}
	return opt, nil
}

// NewClient creates an asynq client used to enqueue tasks from the API server.
func NewClient(redisURL string) (*asynq.Client, error) {
	opt, err := RedisOpt(redisURL)
	if err != nil {
		return nil, err
	}
	return asynq.NewClient(opt), nil
}

// NewServer creates the asynq worker server.
func NewServer(redisURL string) (*asynq.Server, error) {
	opt, err := RedisOpt(redisURL)
	if err != nil {
		return nil, err
	}
	srv := asynq.NewServer(opt, asynq.Config{
		Concurrency: 10,
		Queues:      map[string]int{"default": 1},
	})
	return srv, nil
}
