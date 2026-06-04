// Package storage abstracts object storage so the provider (Supabase Storage,
// Cloudflare R2, AWS S3) is a deployment decision, not a code change.
package storage

import (
	"context"
	"io"
	"time"
)

// Storage is the object-storage contract used by the rest of the app.
type Storage interface {
	// Put stores body at key. size is the exact content length; contentType is
	// the detected MIME (e.g. "application/pdf").
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	// Get opens the object at key for reading. The caller must Close it.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the object at key. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// SignedGetURL returns a time-limited download URL. downloadName, when set,
	// is used as the Content-Disposition attachment filename.
	SignedGetURL(ctx context.Context, key string, ttl time.Duration, downloadName string) (string, error)
}
