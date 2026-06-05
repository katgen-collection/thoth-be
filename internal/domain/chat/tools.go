package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/katgen/thothai/internal/domain/search"
	"github.com/katgen/thothai/internal/infra/ai"
)

// ToolContext carries per-execution state into a tool.
type ToolContext struct {
	UserID string
	Emit   func(Event) // forward tool_progress events to the SSE stream
}

// ToolOutcome is what a tool returns after executing.
type ToolOutcome struct {
	Summary     string          // short human-facing line (tool_result event + stored content)
	LLMContent  string          // content fed back to the model for its final answer
	Raw         json.RawMessage // raw result persisted to messages.tool_result
	SearchJobID string          // optional link to a search_jobs row
}

// ToolExecutor runs a tool with the model-provided arguments.
type ToolExecutor func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error)

// Tool bundles a schema (exposed to the model) with its executor.
type Tool struct {
	Schema  ai.ToolSchema
	Execute ToolExecutor
}

// NewSearchJobsTool wires the Phase 2 search pipeline in as a chat tool. It runs
// the pipeline synchronously and relays each step as a tool_progress event.
func NewSearchJobsTool(searchSvc *search.Service) Tool {
	return Tool{
		Schema: ai.ToolSchema{
			Name:        "search_jobs",
			Description: "Search for job openings based on a natural-language query (role, location, seniority, skills). Use when the user asks to find or look for jobs.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "The full job-search query in natural language, e.g. 'backend engineer remote Jakarta senior Go'"
					}
				},
				"required": ["query"]
			}`),
		},
		Execute: func(ctx context.Context, tc ToolContext, args json.RawMessage) (ToolOutcome, error) {
			var in struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return ToolOutcome{}, fmt.Errorf("invalid search_jobs args: %w", err)
			}
			in.Query = strings.TrimSpace(in.Query)
			if in.Query == "" {
				return ToolOutcome{}, fmt.Errorf("search_jobs requires a non-empty query")
			}

			jobID, jobs, err := searchSvc.RunForChat(ctx, tc.UserID, in.Query, func(ev search.StreamEvent) {
				// Forward pipeline progress, but not the terminal completed/failed
				// frame — the chat loop emits its own tool_result instead.
				if ev.Status == search.StatusCompleted || ev.Status == search.StatusFailed {
					return
				}
				tc.Emit(Event{Type: "tool_progress", Status: ev.Status, Progress: ev.Progress})
			})
			if err != nil {
				return ToolOutcome{}, err
			}

			raw, _ := json.Marshal(jobs)
			summary := fmt.Sprintf("Ditemukan %d posisi relevan", len(jobs))

			llmContent := fmt.Sprintf("%s. Hasil pencarian (JSON):\n%s", summary, raw)
			if len(jobs) == 0 {
				// Steer the model to broaden and retry instead of telling the user
				// the "database is empty" (these results are live Google Jobs).
				llmContent = fmt.Sprintf(
					"The live job search for %q returned no results. This is a real-time search, not a fixed database. "+
						"Try ONE more search_jobs call with a broader query — drop the seniority/keywords, widen the "+
						"location to the country or 'remote', or use a more general role title. Only if a broadened "+
						"search also returns nothing should you tell the user no matches were found right now.",
					in.Query,
				)
			}

			return ToolOutcome{
				Summary:     summary,
				LLMContent:  llmContent,
				Raw:         raw,
				SearchJobID: jobID,
			}, nil
		},
	}
}
