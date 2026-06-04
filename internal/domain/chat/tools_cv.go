package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/katgen/thothai/internal/domain/cv"
	"github.com/katgen/thothai/internal/infra/ai"
)

// CVService is the subset of cv.Service the chat tools use.
type CVService interface {
	AnalyzeDefault(ctx context.Context, userID string) (*cv.CV, error)
	MatchDefaultToJob(ctx context.Context, userID, jobDescription string) (ai.MatchResult, error)
	CoverLetterDefault(ctx context.Context, userID, jobDescription string) (string, error)
	SuggestEditsDefault(ctx context.Context, userID, jobDescription string) (ai.CVEdits, error)
}

const noCVMessage = "User belum punya CV yang siap. Minta mereka mengunggah CV dulu."

// NewCVTools returns the chat tools backed by the CV service: analyze_cv,
// match_cv_to_job, generate_cover_letter. All operate on the user's default CV.
func NewCVTools(svc CVService) []Tool {
	jobArgSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"job_description": {
				"type": "string",
				"description": "The job posting text or a clear description of the role to compare against."
			}
		},
		"required": ["job_description"]
	}`)

	return []Tool{
		{
			Schema: ai.ToolSchema{
				Name:        "analyze_cv",
				Description: "Analyze the user's default CV and return its structured contents (skills, experience, education). Use when the user asks about their CV.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			Execute: func(ctx context.Context, tc ToolContext, _ json.RawMessage) (ToolOutcome, error) {
				c, err := svc.AnalyzeDefault(ctx, tc.UserID)
				if err != nil {
					return cvErr(err)
				}
				raw, _ := json.Marshal(c.ParsedData)
				return ToolOutcome{
					Summary:    fmt.Sprintf("CV \"%s\" dianalisis", c.Filename),
					LLMContent: fmt.Sprintf("Data CV terstruktur (JSON):\n%s", raw),
					Raw:        c.ParsedData,
				}, nil
			},
		},
		{
			Schema: ai.ToolSchema{
				Name:        "match_cv_to_job",
				Description: "Score how well the user's default CV fits a given job, with strengths and gaps.",
				Parameters:  jobArgSchema,
			},
			Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
				job, err := jobDescriptionArg(args)
				if err != nil {
					return ToolOutcome{}, err
				}
				res, err := svc.MatchDefaultToJob(ctx, tc.UserID, job)
				if err != nil {
					return cvErr(err)
				}
				raw, _ := json.Marshal(res)
				return ToolOutcome{
					Summary:    fmt.Sprintf("Skor kecocokan: %d/100", res.Score),
					LLMContent: fmt.Sprintf("Hasil pencocokan (JSON):\n%s", raw),
					Raw:        raw,
				}, nil
			},
		},
		{
			Schema: ai.ToolSchema{
				Name:        "generate_cover_letter",
				Description: "Generate a cover letter tailored to a job using the user's default CV.",
				Parameters:  jobArgSchema,
			},
			Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
				job, err := jobDescriptionArg(args)
				if err != nil {
					return ToolOutcome{}, err
				}
				letter, err := svc.CoverLetterDefault(ctx, tc.UserID, job)
				if err != nil {
					return cvErr(err)
				}
				return ToolOutcome{
					Summary:    "Cover letter dibuat",
					LLMContent: letter,
					Raw:        mustJSON(map[string]string{"cover_letter": letter}),
				}, nil
			},
		},
		{
			Schema: ai.ToolSchema{
				Name:        "suggest_cv_edits",
				Description: "Suggest concrete edits to tailor the user's default CV to a specific job (rephrase/reorder/emphasize what's already there).",
				Parameters:  jobArgSchema,
			},
			Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
				job, err := jobDescriptionArg(args)
				if err != nil {
					return ToolOutcome{}, err
				}
				edits, err := svc.SuggestEditsDefault(ctx, tc.UserID, job)
				if err != nil {
					return cvErr(err)
				}
				raw, _ := json.Marshal(edits)
				return ToolOutcome{
					Summary:    fmt.Sprintf("%d saran perbaikan CV", len(edits.Edits)),
					LLMContent: fmt.Sprintf("Saran tailoring CV (JSON):\n%s", raw),
					Raw:        raw,
				}, nil
			},
		},
	}
}

func jobDescriptionArg(args json.RawMessage) (string, error) {
	var in struct {
		JobDescription string `json:"job_description"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.JobDescription) == "" {
		return "", fmt.Errorf("job_description is required")
	}
	return in.JobDescription, nil
}

// cvErr maps a missing CV to a friendly tool outcome (so the model can ask the
// user to upload one) and surfaces other errors as failures.
func cvErr(err error) (ToolOutcome, error) {
	if errors.Is(err, cv.ErrNotFound) {
		return ToolOutcome{Summary: noCVMessage, LLMContent: noCVMessage}, nil
	}
	return ToolOutcome{}, err
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
