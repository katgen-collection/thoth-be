package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/redis/go-redis/v9"
	"strings"

	"github.com/katgen/thothai/internal/config"
	"github.com/katgen/thothai/internal/domain/chat"
	"github.com/katgen/thothai/internal/domain/cv"
	"github.com/katgen/thothai/internal/domain/job"
	"github.com/katgen/thothai/internal/domain/search"
	"github.com/katgen/thothai/internal/http/handlers"
	"github.com/katgen/thothai/internal/infra/middleware"
)

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

	// Health — no auth.
	app.Get("/health", handlers.Health)

	// Protected API — all routes receive X-User-* headers from api-gateway.
	api := app.Group("/api/v1", middleware.Auth())

	searchHandler := handlers.NewSearchHandler(d.SearchService, d.Redis)
	searchHandler.Register(api.Group("/search"))

	chatHandler := handlers.NewChatHandler(d.ChatService)
	chatHandler.Register(api.Group("/chat"))

	cvHandler := handlers.NewCVHandler(d.CVService, int64(d.Config.MaxCVFileSizeMB)*1024*1024)
	cvHandler.Register(api.Group("/cvs"))

	jobHandler := handlers.NewJobHandler(d.JobService)
	jobHandler.Register(api.Group("/jobs"))

	// history is documented at /api/v1/history; it is also reachable via the
	// search group's /history. Register the top-level alias here too.
	api.Get("/history", func(c *fiber.Ctx) error {
		return c.Redirect("/api/v1/search/history", fiber.StatusTemporaryRedirect)
	})

	return app
}
