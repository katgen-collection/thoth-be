package search

import "time"

// Status values for a search job (mirrors the Python state machine).
const (
	StatusPending      = "pending"
	StatusExtracting   = "extracting"
	StatusFetchingJobs = "fetching_jobs"
	StatusAIFiltering  = "ai_filtering"
	StatusCompleted    = "completed"
	StatusFailed       = "failed"
)

// SearchJob is a row in the search_jobs table.
type SearchJob struct {
	ID              string           `json:"id"`
	UserID          string           `json:"user_id"`
	Query           string           `json:"query"`
	Status          string           `json:"status"`
	Progress        int              `json:"progress"`
	ExtractedParams *ExtractedParams `json:"extracted_params,omitempty"`
	Result          []FilteredJob    `json:"result,omitempty"`
	Error           string           `json:"error,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// ExtractedParams is DeepSeek's structured output from the user's free-text query.
type ExtractedParams struct {
	JobTitle        string   `json:"job_title"`
	Location        string   `json:"location"`
	ExperienceLevel string   `json:"experience_level"`
	EmploymentType  string   `json:"employment_type"`
	Keywords        []string `json:"keywords"`
}

// FilteredJob is a single AI-scored job in the final result list.
type FilteredJob struct {
	Title              string `json:"title"`
	Company            string `json:"company"`
	Location           string `json:"location"`
	DescriptionSnippet string `json:"description_snippet"`
	ApplyLink          string `json:"apply_link"`
	RelevanceScore     int    `json:"relevance_score"`
}
