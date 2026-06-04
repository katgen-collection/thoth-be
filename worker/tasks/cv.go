package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/katgen/thothai/internal/domain/cv"
)

// CVHandler returns an asynq handler that runs the CV extraction pipeline.
func CVHandler(svc *cv.Service) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var p cv.Payload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("unmarshal cv payload: %w: %w", err, asynq.SkipRetry)
		}
		return svc.Process(ctx, p.CVID)
	}
}
