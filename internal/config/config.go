package config

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration, loaded from environment variables.
// On the VPS these come from /etc/thothai/service.env (loaded by systemd).
type Config struct {
	// Database
	DatabaseURL string
	// SchemaName isolates this service's tables in a shared Postgres (Supabase).
	// Empty = use the connection's default search_path (typically "public").
	SchemaName string

	// Redis — asynq broker + pub/sub for SSE
	RedisURL string

	// AI — DeepSeek (OpenAI-compatible)
	DeepSeekAPIKey  string
	DeepSeekBaseURL string

	// External
	SerpAPIKey string
	// Google country (gl) for SerpAPI job search. Without it Google geolocates
	// requests to the US and city-scoped queries (e.g. "Jakarta") return nothing.
	SerpAPICountry string

	// pdf-parser service (loopback)
	PDFParserURL string

	// App
	Port               int
	CORSAllowedOrigins []string

	// File storage (S3-compatible — Supabase Storage / Cloudflare R2 / AWS S3)
	StorageEndpoint        string // blank = AWS default endpoint
	StorageRegion          string
	StorageBucket          string
	StorageAccessKeyID     string
	StorageSecretAccessKey string
	StoragePresignTTL      time.Duration

	MaxCVFileSizeMB   int
	MaxUploadsPerUser int

	// Rate limiting (Redis-backed). Per-minute caps. IP/User are coarse abuse
	// guards; Chat/Search are tighter because each call drives paid work (LLM +
	// SerpAPI) and is the basis for future paid tiers.
	RateLimitEnabled      bool
	RateLimitIPPerMin     int
	RateLimitUserPerMin   int
	RateLimitChatPerMin   int
	RateLimitSearchPerMin int
}

// Load reads configuration from the process environment, applying sane defaults
// that mirror the Python scaffold's .env.example.
func Load() *Config {
	return &Config{
		DatabaseURL: databaseURL(),
		SchemaName:  getEnv("SCHEMA_NAME", ""),

		RedisURL: getEnv("REDIS_URL", "redis://localhost:6379/0"),

		DeepSeekAPIKey:  getEnv("DEEPSEEK_API_KEY", ""),
		DeepSeekBaseURL: getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),

		SerpAPIKey:     getEnv("SERPAPI_KEY", ""),
		SerpAPICountry: getEnv("SERPAPI_GL", "id"),

		PDFParserURL: getEnv("PDF_PARSER_URL", "http://127.0.0.1:5001"),

		Port:               getEnvInt("PORT", 8000),
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")),

		StorageEndpoint:        getEnv("STORAGE_ENDPOINT", ""),
		StorageRegion:          getEnv("STORAGE_REGION", "auto"),
		StorageBucket:          getEnv("STORAGE_BUCKET", "thothai-uploads"),
		StorageAccessKeyID:     getEnv("STORAGE_ACCESS_KEY_ID", ""),
		StorageSecretAccessKey: getEnv("STORAGE_SECRET_ACCESS_KEY", ""),
		StoragePresignTTL:      time.Duration(getEnvInt("STORAGE_PRESIGN_TTL_SECONDS", 300)) * time.Second,

		MaxCVFileSizeMB:   getEnvInt("MAX_CV_FILE_SIZE_MB", 10),
		MaxUploadsPerUser: getEnvInt("MAX_UPLOADS_PER_USER", 50),

		RateLimitEnabled:      getEnvBool("RATE_LIMIT_ENABLED", true),
		RateLimitIPPerMin:     getEnvInt("RATE_LIMIT_IP_PER_MIN", 120),
		RateLimitUserPerMin:   getEnvInt("RATE_LIMIT_USER_PER_MIN", 90),
		RateLimitChatPerMin:   getEnvInt("RATE_LIMIT_CHAT_PER_MIN", 15),
		RateLimitSearchPerMin: getEnvInt("RATE_LIMIT_SEARCH_PER_MIN", 20),
	}
}

// databaseURL prefers an explicit DATABASE_URL; otherwise it assembles a libpq
// URI from discrete DB_* parts (the convention used across the katgen
// ecosystem). url.UserPassword handles escaping of special chars in the
// password. sslmode defaults to "require" for managed Postgres (Supabase).
func databaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(getEnv("DB_USER", "postgres"), getEnv("DB_PASSWORD", "")),
		Host:   net.JoinHostPort(getEnv("DB_HOST", "localhost"), getEnv("DB_PORT", "5432")),
		Path:   "/" + getEnv("DB_NAME", "postgres"),
	}
	q := url.Values{}
	q.Set("sslmode", getEnv("DB_SSLMODE", "require"))
	u.RawQuery = q.Encode()
	return u.String()
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
