package ai

import (
	"context"
	"encoding/json"
	"fmt"

	openai "github.com/sashabaranov/go-openai"

	"github.com/katgen/thothai/internal/domain/search"
)

const extractSystem = `Extract structured job search parameters from the user's natural language query.
Return a JSON object with exactly these fields:
- job_title: string (required, the primary role being searched)
- location: string or null (city, country, or "remote")
- experience_level: "entry" | "mid" | "senior" | null
- employment_type: "full_time" | "part_time" | "contract" | "internship" | null
- keywords: array of strings (additional skills or requirements mentioned)
Return only valid JSON. No markdown, no explanation.`

const filterSystem = `You are a job relevance evaluator. Given a list of job postings and a user's original search query,
score each job 0–10 for relevance to the query. Include only jobs with score >= 6.
Return a JSON object: { "jobs": [...] }
Each job must have:
- title: string
- company: string
- location: string
- description_snippet: string (max 200 chars, most relevant excerpt)
- apply_link: string or null
- relevance_score: number (6–10)
Sort by relevance_score descending. Return only valid JSON.`

// DeepSeek wraps the OpenAI-compatible DeepSeek chat API.
type DeepSeek struct {
	client *openai.Client
	model  string
}

// NewDeepSeek configures the client against DeepSeek's base URL.
func NewDeepSeek(apiKey, baseURL string) *DeepSeek {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &DeepSeek{
		client: openai.NewClientWithConfig(cfg),
		model:  "deepseek-chat",
	}
}

// ExtractParams turns a free-text query into structured search parameters.
func (d *DeepSeek) ExtractParams(ctx context.Context, query string) (search.ExtractedParams, error) {
	var params search.ExtractedParams
	resp, err := d.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       d.model,
		Temperature: 0.1,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: extractSystem},
			{Role: openai.ChatMessageRoleUser, Content: query},
		},
	})
	if err != nil {
		return params, fmt.Errorf("deepseek extract params: %w", err)
	}
	if len(resp.Choices) == 0 {
		return params, fmt.Errorf("deepseek extract params: empty response")
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &params); err != nil {
		return params, fmt.Errorf("parse extracted params: %w", err)
	}
	return params, nil
}

// FilterJobs scores and filters raw SerpAPI jobs against the original query.
func (d *DeepSeek) FilterJobs(ctx context.Context, rawJobs []map[string]any, originalQuery string) ([]search.FilteredJob, error) {
	if len(rawJobs) == 0 {
		return []search.FilteredJob{}, nil
	}

	if len(rawJobs) > 30 {
		rawJobs = rawJobs[:30]
	}
	jobsJSON, err := json.Marshal(rawJobs)
	if err != nil {
		return nil, fmt.Errorf("marshal raw jobs: %w", err)
	}

	resp, err := d.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       d.model,
		Temperature: 0.2,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: filterSystem},
			{Role: openai.ChatMessageRoleUser, Content: fmt.Sprintf("Query: %s\n\nJobs:\n%s", originalQuery, jobsJSON)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("deepseek filter jobs: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("deepseek filter jobs: empty response")
	}

	var wrapper struct {
		Jobs []search.FilteredJob `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &wrapper); err != nil {
		return nil, fmt.Errorf("parse filtered jobs: %w", err)
	}
	return wrapper.Jobs, nil
}
