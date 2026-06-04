package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config configures the S3-compatible client. Endpoint is blank for AWS S3
// and set to the provider URL for Supabase Storage / Cloudflare R2.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

// S3 is an S3-compatible implementation of Storage. Path-style addressing is
// used so it works uniformly across Supabase, R2, and S3.
type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

// NewS3 builds the client. It performs no network I/O.
func NewS3(cfg S3Config) *S3 {
	client := s3.New(s3.Options{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		BaseEndpoint: func() *string {
			if cfg.Endpoint == "" {
				return nil
			}
			return aws.String(cfg.Endpoint)
		}(),
		UsePathStyle: true,
	})
	return &S3{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  cfg.Bucket,
	}
}

func (s *S3) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	return out.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	// Deleting a non-existent key is a no-op on S3; treat NoSuchKey as success.
	var nsk *types.NoSuchKey
	if err != nil && !errors.As(err, &nsk) {
		return fmt.Errorf("delete object %s: %w", key, err)
	}
	return nil
}

func (s *S3) SignedGetURL(ctx context.Context, key string, ttl time.Duration, downloadName string) (string, error) {
	in := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if downloadName != "" {
		// Force a safe download rather than inline rendering.
		in.ResponseContentDisposition = aws.String(fmt.Sprintf("attachment; filename=%q", downloadName))
		in.ResponseContentType = aws.String("application/pdf")
	}
	req, err := s.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign get %s: %w", key, err)
	}
	return req.URL, nil
}

// Ensure S3 satisfies the interface.
var _ Storage = (*S3)(nil)
