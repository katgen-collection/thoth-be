package cv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/katgen/thothai/internal/infra/ai"
	"github.com/katgen/thothai/internal/infra/pdfparser"
	"github.com/katgen/thothai/internal/infra/queue"
	"github.com/katgen/thothai/internal/infra/storage"
	"github.com/katgen/thothai/internal/infra/upload"
)

// ErrQuotaExceeded is returned when a user is at their upload limit.
var ErrQuotaExceeded = errors.New("upload quota exceeded")

// AI is the subset of the DeepSeek client the CV service needs.
type AI interface {
	ParseCV(ctx context.Context, text string) (json.RawMessage, error)
	MatchCVToJob(ctx context.Context, cvData, jobDescription string) (ai.MatchResult, error)
	GenerateCoverLetter(ctx context.Context, cvData, jobDescription string) (string, error)
	SuggestCVEdits(ctx context.Context, cvData, jobDescription string) (ai.CVEdits, error)
}

// Config holds the tunables the service needs.
type Config struct {
	MaxFileBytes    int64
	MaxUploadsPerUser int
	PresignTTL      time.Duration
}

// Service orchestrates CV upload, storage, parsing, and AI features.
type Service struct {
	repo  *Repository
	store storage.Storage
	pdf   *pdfparser.Client
	ai    AI
	queue *asynq.Client
	cfg   Config
}

func NewService(repo *Repository, store storage.Storage, pdf *pdfparser.Client, aiClient AI, q *asynq.Client, cfg Config) *Service {
	return &Service{repo: repo, store: store, pdf: pdf, ai: aiClient, queue: q, cfg: cfg}
}

// Upload validates is assumed done by the caller; this stores the bytes, creates
// the row, and enqueues async processing. data must be the full validated file.
func (s *Service) Upload(ctx context.Context, userID string, v upload.Validated, data []byte) (*CV, error) {
	count, err := s.repo.CountByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if count >= s.cfg.MaxUploadsPerUser {
		return nil, ErrQuotaExceeded
	}

	id := uuid.NewString()
	key := fmt.Sprintf("cvs/%s/%s.pdf", userID, id)

	if err := s.store.Put(ctx, key, bytes.NewReader(data), v.Size, v.MimeType); err != nil {
		return nil, err
	}

	c := CV{
		ID: id, UserID: userID, Filename: v.Filename, StorageKey: key,
		FileSize: v.Size, MimeType: v.MimeType, SHA256: v.SHA256, Status: StatusUploaded,
	}
	if err := s.repo.Create(ctx, c); err != nil {
		_ = s.store.Delete(ctx, key) // avoid orphaned object
		return nil, err
	}

	s.enqueueProcess(ctx, id, userID)
	return &c, nil
}

// enqueueProcess best-effort enqueues the processing task. Failure is non-fatal:
// the chat tools can still process synchronously on demand.
func (s *Service) enqueueProcess(ctx context.Context, cvID, userID string) {
	payload, err := json.Marshal(Payload{CVID: cvID, UserID: userID})
	if err != nil {
		return
	}
	task := asynq.NewTask(queue.TypeCVProcess, payload)
	_, _ = s.queue.EnqueueContext(ctx, task, asynq.TaskID("cv-"+cvID), asynq.MaxRetry(2))
}

// Process runs the extraction pipeline: storage → pdf-parser → DeepSeek parse →
// DB. Invoked by the worker and synchronously by chat tools when needed.
func (s *Service) Process(ctx context.Context, cvID string) error {
	c, err := s.repo.GetByID(ctx, cvID)
	if err != nil {
		return err
	}

	_ = s.repo.SetStatus(ctx, cvID, StatusParsing)

	fail := func(cause error) error {
		_ = s.repo.SetStatus(ctx, cvID, StatusFailed)
		return cause
	}

	rc, err := s.store.Get(ctx, c.StorageKey)
	if err != nil {
		return fail(err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return fail(err)
	}

	parsed, err := s.pdf.Parse(ctx, c.Filename, data)
	if err != nil {
		return fail(err)
	}

	structured, err := s.ai.ParseCV(ctx, parsed.Text)
	if err != nil {
		return fail(err)
	}

	if err := s.repo.SetProcessed(ctx, cvID, parsed.Text, structured, StatusReady); err != nil {
		return fail(err)
	}
	return nil
}

// List, Get, SetDefault pass through to the repository (ownership-scoped).
func (s *Service) List(ctx context.Context, userID string) ([]CV, error) {
	return s.repo.List(ctx, userID)
}

func (s *Service) Get(ctx context.Context, id, userID string) (*CV, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) SetDefault(ctx context.Context, id, userID string) error {
	return s.repo.SetDefault(ctx, id, userID)
}

// Reanalyze re-runs processing for a CV the user owns.
func (s *Service) Reanalyze(ctx context.Context, id, userID string) error {
	if _, err := s.repo.Get(ctx, id, userID); err != nil {
		return err
	}
	return s.Process(ctx, id)
}

// Delete removes the CV row and its stored object.
func (s *Service) Delete(ctx context.Context, id, userID string) error {
	key, err := s.repo.Delete(ctx, id, userID)
	if err != nil {
		return err
	}
	return s.store.Delete(ctx, key)
}

// DownloadURL returns a short-lived presigned download URL (ownership-checked).
func (s *Service) DownloadURL(ctx context.Context, id, userID string) (string, error) {
	c, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return "", err
	}
	return s.store.SignedGetURL(ctx, c.StorageKey, s.cfg.PresignTTL, c.Filename)
}

// MatchToJob matches a specific CV against a job description.
func (s *Service) MatchToJob(ctx context.Context, id, userID, jobDescription string) (ai.MatchResult, error) {
	c, err := s.ensureReady(ctx, id, userID)
	if err != nil {
		return ai.MatchResult{}, err
	}
	return s.ai.MatchCVToJob(ctx, cvContext(c), jobDescription)
}

// MatchDefaultToJob matches the user's default CV (used by chat).
func (s *Service) MatchDefaultToJob(ctx context.Context, userID, jobDescription string) (ai.MatchResult, error) {
	c, err := s.defaultReady(ctx, userID)
	if err != nil {
		return ai.MatchResult{}, err
	}
	return s.ai.MatchCVToJob(ctx, cvContext(c), jobDescription)
}

// CoverLetter generates a cover letter from a specific CV.
func (s *Service) CoverLetter(ctx context.Context, id, userID, jobDescription string) (string, error) {
	c, err := s.ensureReady(ctx, id, userID)
	if err != nil {
		return "", err
	}
	return s.ai.GenerateCoverLetter(ctx, cvContext(c), jobDescription)
}

// CoverLetterDefault generates a cover letter from the user's default CV (chat).
func (s *Service) CoverLetterDefault(ctx context.Context, userID, jobDescription string) (string, error) {
	c, err := s.defaultReady(ctx, userID)
	if err != nil {
		return "", err
	}
	return s.ai.GenerateCoverLetter(ctx, cvContext(c), jobDescription)
}

// AnalyzeDefault returns the user's default CV, processing it first if needed.
func (s *Service) AnalyzeDefault(ctx context.Context, userID string) (*CV, error) {
	return s.defaultReady(ctx, userID)
}

// SuggestEdits proposes tailoring changes for a specific CV against a job.
func (s *Service) SuggestEdits(ctx context.Context, id, userID, jobDescription string) (ai.CVEdits, error) {
	c, err := s.ensureReady(ctx, id, userID)
	if err != nil {
		return ai.CVEdits{}, err
	}
	return s.ai.SuggestCVEdits(ctx, cvContext(c), jobDescription)
}

// SuggestEditsDefault proposes tailoring changes for the user's default CV (chat).
func (s *Service) SuggestEditsDefault(ctx context.Context, userID, jobDescription string) (ai.CVEdits, error) {
	c, err := s.defaultReady(ctx, userID)
	if err != nil {
		return ai.CVEdits{}, err
	}
	return s.ai.SuggestCVEdits(ctx, cvContext(c), jobDescription)
}

// ensureReady fetches a CV and processes it synchronously if not yet ready.
func (s *Service) ensureReady(ctx context.Context, id, userID string) (*CV, error) {
	c, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusReady {
		if err := s.Process(ctx, id); err != nil {
			return nil, err
		}
		c, err = s.repo.Get(ctx, id, userID)
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (s *Service) defaultReady(ctx context.Context, userID string) (*CV, error) {
	c, err := s.repo.GetDefault(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.ensureReady(ctx, c.ID, userID)
}

// cvContext picks the richest representation of a CV for the model: the
// structured parse if available, otherwise the raw extracted text.
func cvContext(c *CV) string {
	if len(c.ParsedData) > 0 {
		return string(c.ParsedData)
	}
	return c.ExtractedText
}
