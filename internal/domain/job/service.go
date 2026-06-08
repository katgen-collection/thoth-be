package job

import (
	"context"
	"fmt"
	"strings"

	"github.com/katgen/thothai/internal/infra/ai"
	"github.com/katgen/thothai/internal/infra/fetch"
)

// maxJobPageChars bounds how much fetched page text we send to the model.
const maxJobPageChars = 20000

// JobAI is the subset of the DeepSeek client this service needs.
type JobAI interface {
	AnalyzeJobPosting(ctx context.Context, content string) (ai.JobAnalysis, error)
	InterviewPrep(ctx context.Context, title, company, description string) (string, error)
}

// Service handles the saved-job tracker plus URL analysis and interview prep.
type Service struct {
	repo    *Repository
	fetcher *fetch.Fetcher
	ai      JobAI
}

func NewService(repo *Repository, fetcher *fetch.Fetcher, aiClient JobAI) *Service {
	return &Service{repo: repo, fetcher: fetcher, ai: aiClient}
}

// Save stores a job in the user's tracker.
func (s *Service) Save(ctx context.Context, userID string, j SavedJob) (*SavedJob, error) {
	j.UserID = userID
	return s.repo.Create(ctx, j)
}

// List returns the user's saved jobs, optionally filtered by status.
func (s *Service) List(ctx context.Context, userID, statusFilter string) ([]SavedJob, error) {
	return s.repo.List(ctx, userID, statusFilter)
}

// Get returns one saved job the user owns (used to resolve an @-mentioned job).
func (s *Service) Get(ctx context.Context, id, userID string) (*SavedJob, error) {
	return s.repo.Get(ctx, id, userID)
}

// UpdateStatus moves a job along the tracker.
func (s *Service) UpdateStatus(ctx context.Context, id, userID, status string) (*SavedJob, error) {
	if !ValidStatus(status) {
		return nil, fmt.Errorf("invalid status %q", status)
	}
	return s.repo.UpdateStatus(ctx, id, userID, status)
}

// Delete removes a saved job.
func (s *Service) Delete(ctx context.Context, id, userID string) error {
	return s.repo.Delete(ctx, id, userID)
}

// AnalyzeURL fetches a job posting (SSRF-guarded) and extracts structured info.
func (s *Service) AnalyzeURL(ctx context.Context, url string) (ai.JobAnalysis, error) {
	res, err := s.fetcher.Get(ctx, url)
	if err != nil {
		return ai.JobAnalysis{}, fmt.Errorf("fetch job url: %w", err)
	}
	// Many job boards (LinkedIn especially) answer automated requests with a
	// block/login page and a non-2xx status. Surface that as an error so the
	// caller can fall back to saving the URL manually instead of extracting
	// garbage from a wall page.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ai.JobAnalysis{}, fmt.Errorf("job site returned status %d (likely blocking automated access)", res.StatusCode)
	}
	// Prefer structured data (meta/OpenGraph/JSON-LD) — modern job boards render
	// the body with JS but still emit these for SEO, so they carry the role even
	// when the visible text is sparse. Fall back to / augment with body text.
	structured := fetch.ExtractStructured(res.Body)
	body := fetch.HTMLToText(res.Body, maxJobPageChars)
	text := strings.TrimSpace(structured + "\n\n" + body)
	if len(text) < 60 {
		return ai.JobAnalysis{}, fmt.Errorf("job page had no readable content (likely requires login or JavaScript)")
	}
	if maxJobPageChars > 0 && len(text) > maxJobPageChars {
		text = text[:maxJobPageChars]
	}
	analysis, err := s.ai.AnalyzeJobPosting(ctx, text)
	if err != nil {
		return ai.JobAnalysis{}, err
	}
	if analysis.ApplyLink == "" {
		analysis.ApplyLink = res.FinalURL
	}
	return analysis, nil
}

// InterviewPrep generates prep for a saved job the user owns.
func (s *Service) InterviewPrep(ctx context.Context, id, userID string) (string, error) {
	j, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return "", err
	}
	return s.ai.InterviewPrep(ctx, j.Title, j.Company, j.Description)
}

// InterviewPrepFromText generates prep from a free-text job description (chat).
func (s *Service) InterviewPrepFromText(ctx context.Context, description string) (string, error) {
	return s.ai.InterviewPrep(ctx, "", "", description)
}
