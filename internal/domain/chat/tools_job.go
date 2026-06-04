package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/katgen/thothai/internal/domain/job"
	"github.com/katgen/thothai/internal/infra/ai"
)

// JobService is the subset of job.Service the chat tools use.
type JobService interface {
	Save(ctx context.Context, userID string, j job.SavedJob) (*job.SavedJob, error)
	List(ctx context.Context, userID, statusFilter string) ([]job.SavedJob, error)
	AnalyzeURL(ctx context.Context, url string) (ai.JobAnalysis, error)
	InterviewPrepFromText(ctx context.Context, description string) (string, error)
}

// NewJobTools returns the job-workflow chat tools: save_job, get_saved_jobs,
// analyze_job_url, prep_interview.
func NewJobTools(svc JobService) []Tool {
	return []Tool{
		{
			Schema: ai.ToolSchema{
				Name:        "save_job",
				Description: "Save a job to the user's tracker. Provide the job details you already know from the conversation.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"title": {"type": "string"},
						"company": {"type": "string"},
						"location": {"type": "string"},
						"description": {"type": "string"},
						"apply_link": {"type": "string"}
					},
					"required": ["title"]
				}`),
			},
			Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
				var in job.SavedJob
				if err := json.Unmarshal(args, &in); err != nil {
					return ToolOutcome{}, fmt.Errorf("invalid save_job args: %w", err)
				}
				if strings.TrimSpace(in.Title) == "" {
					return ToolOutcome{}, fmt.Errorf("save_job requires a title")
				}
				in.Source = args // keep the raw model-provided payload
				saved, err := svc.Save(ctx, tc.UserID, in)
				if err != nil {
					return ToolOutcome{}, err
				}
				raw, _ := json.Marshal(saved)
				return ToolOutcome{
					Summary:    fmt.Sprintf("Disimpan: %s di %s", saved.Title, saved.Company),
					LLMContent: fmt.Sprintf("Job tersimpan (JSON):\n%s", raw),
					Raw:        raw,
				}, nil
			},
		},
		{
			Schema: ai.ToolSchema{
				Name:        "get_saved_jobs",
				Description: "List the jobs the user has saved, optionally filtered by tracker status.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"status": {"type": "string", "enum": ["saved","applied","interview","offer","rejected"]}
					}
				}`),
			},
			Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
				var in struct {
					Status string `json:"status"`
				}
				_ = json.Unmarshal(args, &in)
				jobs, err := svc.List(ctx, tc.UserID, strings.TrimSpace(in.Status))
				if err != nil {
					return ToolOutcome{}, err
				}
				raw, _ := json.Marshal(jobs)
				return ToolOutcome{
					Summary:    fmt.Sprintf("%d job tersimpan", len(jobs)),
					LLMContent: fmt.Sprintf("Daftar job tersimpan (JSON):\n%s", raw),
					Raw:        raw,
				}, nil
			},
		},
		{
			Schema: ai.ToolSchema{
				Name:        "analyze_job_url",
				Description: "Fetch a job posting from a URL and extract its title, company, location, and summary.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {"url": {"type": "string", "description": "The job posting URL (http/https)."}},
					"required": ["url"]
				}`),
			},
			Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
				var in struct {
					URL string `json:"url"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return ToolOutcome{}, fmt.Errorf("invalid analyze_job_url args: %w", err)
				}
				if strings.TrimSpace(in.URL) == "" {
					return ToolOutcome{}, fmt.Errorf("analyze_job_url requires a url")
				}
				analysis, err := svc.AnalyzeURL(ctx, in.URL)
				if err != nil {
					return ToolOutcome{}, err
				}
				raw, _ := json.Marshal(analysis)
				return ToolOutcome{
					Summary:    fmt.Sprintf("Dianalisis: %s di %s", analysis.Title, analysis.Company),
					LLMContent: fmt.Sprintf("Analisis job (JSON):\n%s", raw),
					Raw:        raw,
				}, nil
			},
		},
		{
			Schema: ai.ToolSchema{
				Name:        "prep_interview",
				Description: "Generate interview preparation (likely questions + tips) for a job described in text.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {"job_description": {"type": "string"}},
					"required": ["job_description"]
				}`),
			},
			Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
				job, err := jobDescriptionArg(args)
				if err != nil {
					return ToolOutcome{}, err
				}
				prep, err := svc.InterviewPrepFromText(ctx, job)
				if err != nil {
					return ToolOutcome{}, err
				}
				return ToolOutcome{
					Summary:    "Persiapan interview dibuat",
					LLMContent: prep,
					Raw:        mustJSON(map[string]string{"interview_prep": prep}),
				}, nil
			},
		},
	}
}
