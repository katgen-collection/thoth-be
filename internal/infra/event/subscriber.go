// Package event subscribes to ecosystem domain events over Redis pub/sub.
package event

import (
	"context"
	"encoding/json"
	"log"

	"github.com/redis/go-redis/v9"

	"github.com/katgen/thothai/internal/domain/user"
)

// usersChannel is the Redis channel user-auth-service publishes user lifecycle
// events to. Must match its publisher exactly.
const usersChannel = "events.users"

// userEnvelope matches the {"event","data"} shape published by
// user-auth-service; Data unmarshals straight into a user.Snapshot.
type userEnvelope struct {
	Event string        `json:"event"`
	Data  user.Snapshot `json:"data"`
}

// SnapshotUpserter is the slice of the user repository this subscriber needs.
type SnapshotUpserter interface {
	Upsert(ctx context.Context, s *user.Snapshot) error
}

// SubscribeUsers keeps the local user_snapshots cache in sync with
// UserCreated / UserUpdated events on the events.users channel. It spawns a
// goroutine that runs until ctx is cancelled. Mirrors chat-service.
func SubscribeUsers(ctx context.Context, rdb *redis.Client, repo SnapshotUpserter) {
	pubsub := rdb.Subscribe(ctx, usersChannel)
	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()
		log.Printf("event: subscribed to %s for user snapshots", usersChannel)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var env userEnvelope
				if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
					log.Printf("event: bad user payload: %v", err)
					continue
				}
				if env.Event != "UserCreated" && env.Event != "UserUpdated" {
					continue // ignore unrelated events on the channel
				}
				if env.Data.ID == "" {
					log.Printf("event: %s with empty user id, skipping", env.Event)
					continue
				}
				if err := repo.Upsert(ctx, &env.Data); err != nil {
					log.Printf("event: upsert user %s failed: %v", env.Data.ID, err)
					continue
				}
				log.Printf("event: user snapshot %s synced (%s)", env.Data.ID, env.Event)
			}
		}
	}()
}
