package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/katgen/thothai/internal/domain/search"
)

// SearchHandler returns an asynq handler that runs the job-search pipeline.
func SearchHandler(svc *search.Service) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var p search.Payload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			// A malformed payload is unrecoverable — do not retry.
			return fmt.Errorf("unmarshal search payload: %w: %w", err, asynq.SkipRetry)
		}
		return svc.RunPipeline(ctx, p)
	}
}
