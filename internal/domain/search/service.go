package search

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/katgen/thothai/internal/infra/queue"
	redisinfra "github.com/katgen/thothai/internal/infra/redis"
)

// Extractor produces structured params from a free-text query.
type Extractor interface {
	ExtractParams(ctx context.Context, query string) (ExtractedParams, error)
	FilterJobs(ctx context.Context, rawJobs []map[string]any, originalQuery string) ([]FilteredJob, error)
}

// JobFetcher fetches raw job postings from an external source (SerpAPI).
type JobFetcher interface {
	Fetch(ctx context.Context, params ExtractedParams) ([]map[string]any, error)
}

// Payload is the asynq task payload for the search pipeline.
type Payload struct {
	TaskID string `json:"task_id"`
	UserID string `json:"user_id"`
	Query  string `json:"query"`
}

// StreamEvent is one SSE frame published to the task's Redis channel.
type StreamEvent struct {
	Status   string        `json:"status"`
	Progress int           `json:"progress"`
	Result   []FilteredJob `json:"result,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// Service coordinates the search pipeline: persistence, queueing, AI, and SSE.
type Service struct {
	repo    *Repository
	queue   *asynq.Client
	rdb     *redis.Client
	ai      Extractor
	fetcher JobFetcher
}

func NewService(repo *Repository, q *asynq.Client, rdb *redis.Client, ai Extractor, fetcher JobFetcher) *Service {
	return &Service{repo: repo, queue: q, rdb: rdb, ai: ai, fetcher: fetcher}
}

// Initiate creates a pending search job and enqueues the pipeline task. The
// asynq task ID is set to the job ID so it is idempotent per job.
func (s *Service) Initiate(ctx context.Context, userID, query string) (string, error) {
	id, err := s.repo.Create(ctx, userID, query)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(Payload{TaskID: id, UserID: userID, Query: query})
	if err != nil {
		return "", err
	}

	task := asynq.NewTask(queue.TypeSearchPipeline, payload)
	if _, err := s.queue.EnqueueContext(ctx, task, asynq.TaskID(id), asynq.MaxRetry(2)); err != nil {
		return "", fmt.Errorf("enqueue search task: %w", err)
	}
	return id, nil
}

// Get returns a search job scoped to the user.
func (s *Service) Get(ctx context.Context, id, userID string) (*SearchJob, error) {
	return s.repo.Get(ctx, id, userID)
}

// List returns paginated search history for a user.
func (s *Service) List(ctx context.Context, userID string, limit, offset int) ([]SearchJob, int, error) {
	return s.repo.List(ctx, userID, limit, offset)
}

// RunPipeline executes the full search pipeline for an already-created job.
// Called by the asynq worker. Progress is published to the job's Redis channel
// so the standalone SSE endpoint can relay it.
func (s *Service) RunPipeline(ctx context.Context, p Payload) error {
	channel := redisinfra.TaskChannel(p.TaskID)
	_, err := s.runCore(ctx, p.TaskID, p.Query, func(ev StreamEvent) {
		s.publish(ctx, channel, ev)
	})
	return err
}

// RunForChat creates a search job and runs the pipeline synchronously in-process,
// forwarding progress through emit (instead of Redis). Used by the chat
// search_jobs tool so there is no worker dependency or pub/sub race. The job is
// still persisted, so it appears in search history. Returns the job ID and the
// final ranked jobs.
func (s *Service) RunForChat(ctx context.Context, userID, query string, emit func(StreamEvent)) (string, []FilteredJob, error) {
	id, err := s.repo.Create(ctx, userID, query)
	if err != nil {
		return "", nil, err
	}
	jobs, err := s.runCore(ctx, id, query, emit)
	return id, jobs, err
}

// runCore is the shared extract → fetch → filter → complete pipeline. It updates
// the DB and forwards each step through emit, returning the final ranked jobs.
func (s *Service) runCore(ctx context.Context, taskID, query string, emit func(StreamEvent)) ([]FilteredJob, error) {
	fail := func(cause error) ([]FilteredJob, error) {
		_ = s.repo.SetFailed(ctx, taskID, cause.Error())
		emit(StreamEvent{Status: StatusFailed, Progress: 0, Error: cause.Error()})
		return nil, cause
	}

	// Step 1 — extract structured search params via DeepSeek.
	_ = s.repo.SetStatus(ctx, taskID, StatusExtracting, 10)
	emit(StreamEvent{Status: StatusExtracting, Progress: 10})

	params, err := s.ai.ExtractParams(ctx, query)
	if err != nil {
		return fail(err)
	}
	_ = s.repo.SetExtractedParams(ctx, taskID, params)

	// Step 2 — fetch raw jobs from Google via SerpAPI.
	_ = s.repo.SetStatus(ctx, taskID, StatusFetchingJobs, 40)
	emit(StreamEvent{Status: StatusFetchingJobs, Progress: 40})

	rawJobs, err := s.fetcher.Fetch(ctx, params)
	if err != nil {
		return fail(err)
	}

	// Step 3 — AI relevance scoring and filtering.
	_ = s.repo.SetStatus(ctx, taskID, StatusAIFiltering, 70)
	emit(StreamEvent{Status: StatusAIFiltering, Progress: 70})

	finalJobs, err := s.ai.FilterJobs(ctx, rawJobs, query)
	if err != nil {
		return fail(err)
	}

	// Step 4 — done.
	if err := s.repo.SetCompleted(ctx, taskID, finalJobs); err != nil {
		return fail(err)
	}
	emit(StreamEvent{Status: StatusCompleted, Progress: 100, Result: finalJobs})
	return finalJobs, nil
}

func (s *Service) publish(ctx context.Context, channel string, ev StreamEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_ = s.rdb.Publish(ctx, channel, data).Err()
}
