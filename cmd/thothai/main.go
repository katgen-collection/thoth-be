package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"

	"github.com/katgen/thothai/internal/config"
	"github.com/katgen/thothai/internal/domain/chat"
	"github.com/katgen/thothai/internal/domain/cv"
	"github.com/katgen/thothai/internal/domain/job"
	"github.com/katgen/thothai/internal/domain/search"
	"github.com/katgen/thothai/internal/domain/user"
	apphttp "github.com/katgen/thothai/internal/http"
	"github.com/katgen/thothai/internal/infra/ai"
	"github.com/katgen/thothai/internal/infra/db"
	"github.com/katgen/thothai/internal/infra/event"
	"github.com/katgen/thothai/internal/infra/fetch"
	"github.com/katgen/thothai/internal/infra/pdfparser"
	"github.com/katgen/thothai/internal/infra/queue"
	redisinfra "github.com/katgen/thothai/internal/infra/redis"
	"github.com/katgen/thothai/internal/infra/serpapi"
	"github.com/katgen/thothai/internal/infra/storage"
	"github.com/katgen/thothai/migrations"
	"github.com/katgen/thothai/worker/tasks"
)

func main() {
	mode := flag.String("mode", "serve", "run mode: serve | worker | migrate")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	switch *mode {
	case "serve":
		runServe(ctx, cfg)
	case "worker":
		runWorker(ctx, cfg)
	case "migrate":
		runMigrate(ctx, cfg)
	default:
		log.Fatalf("unknown mode %q (expected serve | worker | migrate)", *mode)
	}
}

// appDeps bundles the constructed services shared by serve and worker modes.
type appDeps struct {
	rdb     *redisinfra.Client
	search  *search.Service
	chat    *chat.Service
	cv      *cv.Service
	job     *job.Service
	users   *user.Repository
	cleanup func()
}

// build wires the shared dependencies used by both serve and worker modes.
func build(ctx context.Context, cfg *config.Config) appDeps {
	pool, err := db.New(ctx, cfg.DatabaseURL, cfg.SchemaName)
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	rdb, err := redisinfra.New(cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}

	qClient, err := queue.NewClient(cfg.RedisURL)
	if err != nil {
		log.Fatalf("queue client: %v", err)
	}

	deepseek := ai.NewDeepSeek(cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL)
	serp := serpapi.New(cfg.SerpAPIKey, cfg.SerpAPICountry)

	store := storage.NewS3(storage.S3Config{
		Endpoint:        cfg.StorageEndpoint,
		Region:          cfg.StorageRegion,
		Bucket:          cfg.StorageBucket,
		AccessKeyID:     cfg.StorageAccessKeyID,
		SecretAccessKey: cfg.StorageSecretAccessKey,
	})
	pdf := pdfparser.New(cfg.PDFParserURL)

	searchSvc := search.NewService(search.NewRepository(pool), qClient, rdb, deepseek, serp)

	cvSvc := cv.NewService(cv.NewRepository(pool), store, pdf, deepseek, qClient, cv.Config{
		MaxFileBytes:      int64(cfg.MaxCVFileSizeMB) * 1024 * 1024,
		MaxUploadsPerUser: cfg.MaxUploadsPerUser,
		PresignTTL:        cfg.StoragePresignTTL,
	})

	jobSvc := job.NewService(job.NewRepository(pool), fetch.New(fetch.Options{}), deepseek)

	userRepo := user.NewRepository(pool)

	chatTools := []chat.Tool{chat.NewSearchJobsTool(searchSvc)}
	chatTools = append(chatTools, chat.NewCVTools(cvSvc)...)
	chatTools = append(chatTools, chat.NewJobTools(jobSvc)...)
	chatSvc := chat.NewService(chat.NewRepository(pool), deepseek, chatTools)

	cleanup := func() {
		_ = qClient.Close()
		_ = rdb.Close()
		pool.Close()
	}
	return appDeps{rdb: rdb, search: searchSvc, chat: chatSvc, cv: cvSvc, job: jobSvc, users: userRepo, cleanup: cleanup}
}

func runServe(ctx context.Context, cfg *config.Config) {
	deps := build(ctx, cfg)
	defer deps.cleanup()

	// Keep the local user_snapshots cache in sync with user-auth-service via the
	// events.users Redis channel. Runs for the lifetime of the API process.
	subCtx, stopSub := context.WithCancel(ctx)
	defer stopSub()
	event.SubscribeUsers(subCtx, deps.rdb, deps.users)

	app := apphttp.NewApp(apphttp.Deps{
		Config:        cfg,
		SearchService: deps.search,
		ChatService:   deps.chat,
		CVService:     deps.cv,
		JobService:    deps.job,
		Redis:         deps.rdb,
	})

	go func() {
		addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
		log.Printf("thothai-api listening on %s", addr)
		if err := app.Listen(addr); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	waitForSignal()
	log.Println("shutting down api...")
	_ = app.Shutdown()
}

func runWorker(ctx context.Context, cfg *config.Config) {
	deps := build(ctx, cfg)
	defer deps.cleanup()

	srv, err := queue.NewServer(cfg.RedisURL)
	if err != nil {
		log.Fatalf("queue server: %v", err)
	}

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeSearchPipeline, tasks.SearchHandler(deps.search))
	mux.HandleFunc(queue.TypeCVProcess, tasks.CVHandler(deps.cv))

	log.Println("thothai-worker starting...")
	if err := srv.Run(mux); err != nil {
		log.Fatalf("worker: %v", err)
	}
}

func runMigrate(ctx context.Context, cfg *config.Config) {
	pool, err := db.New(ctx, cfg.DatabaseURL, cfg.SchemaName)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations applied successfully")
	os.Exit(0)
}

func waitForSignal() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
