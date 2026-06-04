package job

import (
	"encoding/json"
	"time"
)

// Tracker status values.
const (
	StatusSaved     = "saved"
	StatusApplied   = "applied"
	StatusInterview = "interview"
	StatusOffer     = "offer"
	StatusRejected  = "rejected"
)

// ValidStatus reports whether s is a recognized tracker status.
func ValidStatus(s string) bool {
	switch s {
	case StatusSaved, StatusApplied, StatusInterview, StatusOffer, StatusRejected:
		return true
	}
	return false
}

// SavedJob is a row in the saved_jobs table.
type SavedJob struct {
	ID          string          `json:"id"`
	UserID      string          `json:"user_id"`
	Title       string          `json:"title"`
	Company     string          `json:"company"`
	Location    string          `json:"location"`
	Description string          `json:"description"`
	ApplyLink   string          `json:"apply_link,omitempty"`
	Source      json.RawMessage `json:"source,omitempty"`
	Status      string          `json:"status"`
	Notes       string          `json:"notes,omitempty"`
	AppliedAt   *time.Time      `json:"applied_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
