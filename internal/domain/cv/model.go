package cv

import (
	"encoding/json"
	"time"
)

// Upload/parse status state machine.
const (
	StatusUploaded = "uploaded"
	StatusScanning = "scanning" // reserved for Phase 3.5 AV
	StatusParsing  = "parsing"
	StatusReady    = "ready"
	StatusRejected = "rejected"
	StatusFailed   = "failed"
)

// CV is a row in the cvs table.
type CV struct {
	ID            string          `json:"id"`
	UserID        string          `json:"user_id"`
	Filename      string          `json:"filename"`
	StorageKey    string          `json:"-"` // never exposed to clients
	FileSize      int64           `json:"file_size"`
	MimeType      string          `json:"mime_type"`
	SHA256        string          `json:"sha256"`
	Status        string          `json:"status"`
	ExtractedText string          `json:"extracted_text,omitempty"`
	ParsedData    json.RawMessage `json:"parsed_data,omitempty"`
	IsDefault     bool            `json:"is_default"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Payload is the asynq task payload for CV processing.
type Payload struct {
	CVID   string `json:"cv_id"`
	UserID string `json:"user_id"`
}
