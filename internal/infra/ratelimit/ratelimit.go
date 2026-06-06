// Package ratelimit provides a Redis-backed fixed-window rate limiter as Fiber
// middleware. It is used to throttle abuse and to underpin per-user usage quotas
// (the basis for future paid tiers).
//
// Two granularities are wired in the HTTP layer:
//   - IP level  — coarse protection against a single source hammering the API
//     (catches e.g. one IP farming many accounts), keyed by the real client IP.
//   - User level — per signed-in user, with tighter limits on the expensive
//     endpoints (chat + search) that cost real money (LLM + SerpAPI) per call.
package ratelimit

import (
	"context"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// incrWindow atomically increments a counter and sets its TTL on first use, so
// the window is anchored at the first request and self-expires. Returns the new
// count. Atomic, so concurrent requests can't race the EXPIRE.
var incrWindow = redis.NewScript(`
local n = redis.call("INCR", KEYS[1])
if n == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return n
`)

// Rule configures one limiter instance.
type Rule struct {
	// Scope namespaces the Redis key (e.g. "ip", "user", "chat").
	Scope string
	// Max requests allowed per Window.
	Max int
	// Window length (e.g. time.Minute).
	Window time.Duration
	// Key returns the identity to limit on. Returning "" skips this request
	// (e.g. apply a chat limit only to the send-message route).
	Key func(c *fiber.Ctx) string
}

// New builds Fiber middleware enforcing rule with a Redis fixed window.
//
// It FAILS OPEN: if Redis is unreachable the request is allowed. A transient
// cache outage should degrade throttling, not take the whole API down.
func New(rdb *redis.Client, rule Rule) fiber.Handler {
	windowMS := strconv.FormatInt(rule.Window.Milliseconds(), 10)
	limit := strconv.Itoa(rule.Max)

	return func(c *fiber.Ctx) error {
		id := rule.Key(c)
		if id == "" {
			return c.Next()
		}

		key := "rl:" + rule.Scope + ":" + id
		ctx, cancel := context.WithTimeout(c.UserContext(), 200*time.Millisecond)
		defer cancel()

		n, err := incrWindow.Run(ctx, rdb, []string{key}, windowMS).Int()
		if err != nil {
			return c.Next() // fail open
		}

		remaining := max(rule.Max-n, 0)
		c.Set("X-RateLimit-Limit", limit)
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if n > rule.Max {
			c.Set("Retry-After", strconv.Itoa(int(rule.Window.Seconds())))
			return fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded — please slow down a moment")
		}
		return c.Next()
	}
}
