package http

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/redis/go-redis/v9"

	"github.com/katgen/thothai/internal/config"
	"github.com/katgen/thothai/internal/domain/chat"
	"github.com/katgen/thothai/internal/domain/cv"
	"github.com/katgen/thothai/internal/domain/job"
	"github.com/katgen/thothai/internal/domain/search"
	"github.com/katgen/thothai/internal/http/handlers"
	"github.com/katgen/thothai/internal/infra/middleware"
	"github.com/katgen/thothai/internal/infra/ratelimit"
)

// clientIP resolves the real caller IP behind Caddy + the api-gateway. Caddy
// sets X-Real-IP to the true TCP peer (it overwrites, so a client can't spoof
// it), which the gateway forwards. We fall back to the last X-Forwarded-For hop,
// then the direct peer.
func clientIP(c *fiber.Ctx) string {
	if v := strings.TrimSpace(c.Get("X-Real-IP")); v != "" {
		return v
	}
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	return c.IP()
}

// authedUserID is the gateway-validated user id (empty pre-auth → limiter skips).
func authedUserID(c *fiber.Ctx) string {
	return strings.TrimSpace(c.Get("X-User-Id"))
}

// Deps bundles everything the HTTP layer needs.
type Deps struct {
	Config        *config.Config
	SearchService *search.Service
	ChatService   *chat.Service
	CVService     *cv.Service
	JobService    *job.Service
	Redis         *redis.Client
}

// NewApp builds the Fiber app with all routes registered.
func NewApp(d Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "thothai-api",
		DisableStartupMessage: false,
		// SSE handlers stream manually; keep the default body limit modest but
		// allow CV uploads later via per-route overrides.
		BodyLimit: 16 * 1024 * 1024,
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     strings.Join(d.Config.CORSAllowedOrigins, ","),
		AllowMethods:     "GET,POST,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "*",
		AllowCredentials: true,
	}))

	// Health — no auth, no rate limit (registered before the limiters below).
	app.Get("/health", handlers.Health)

	cfg := d.Config
	// IP-level limiter: coarse protection across the whole API. Registered here
	// (after /health) so health checks are never throttled.
	if cfg.RateLimitEnabled {
		app.Use(ratelimit.New(d.Redis, ratelimit.Rule{
			Scope: "ip", Max: cfg.RateLimitIPPerMin, Window: time.Minute, Key: clientIP,
		}))
	}

	// Protected API — all routes receive X-User-* headers from api-gateway.
	api := app.Group("/api/v1", middleware.Auth())

	// User-level limiter: general per-user cap on every authenticated route.
	if cfg.RateLimitEnabled {
		api.Use(ratelimit.New(d.Redis, ratelimit.Rule{
			Scope: "user", Max: cfg.RateLimitUserPerMin, Window: time.Minute, Key: authedUserID,
		}))
	}

	// Tighter per-user limiters on the costly endpoints (each call drives paid
	// LLM + SerpAPI work). Returning "" from Key skips non-targeted requests.
	chatSendLimit := ratelimit.New(d.Redis, ratelimit.Rule{
		Scope: "chat", Max: cfg.RateLimitChatPerMin, Window: time.Minute,
		Key: func(c *fiber.Ctx) string {
			if c.Method() == fiber.MethodPost && strings.HasSuffix(c.Path(), "/messages") {
				return authedUserID(c)
			}
			return ""
		},
	})
	searchLimit := ratelimit.New(d.Redis, ratelimit.Rule{
		Scope: "search", Max: cfg.RateLimitSearchPerMin, Window: time.Minute,
		Key: func(c *fiber.Ctx) string {
			if c.Method() == fiber.MethodPost {
				return authedUserID(c)
			}
			return ""
		},
	})

	searchGroup := api.Group("/search")
	chatGroup := api.Group("/chat")
	if cfg.RateLimitEnabled {
		searchGroup.Use(searchLimit)
		chatGroup.Use(chatSendLimit)
	}

	handlers.NewSearchHandler(d.SearchService, d.Redis).Register(searchGroup)
	handlers.NewChatHandler(d.ChatService).Register(chatGroup)
	handlers.NewCVHandler(d.CVService, int64(cfg.MaxCVFileSizeMB)*1024*1024).Register(api.Group("/cvs"))
	handlers.NewJobHandler(d.JobService).Register(api.Group("/jobs"))

	// history is documented at /api/v1/history; it is also reachable via the
	// search group's /history. Register the top-level alias here too.
	api.Get("/history", func(c *fiber.Ctx) error {
		return c.Redirect("/api/v1/search/history", fiber.StatusTemporaryRedirect)
	})

	return app
}
