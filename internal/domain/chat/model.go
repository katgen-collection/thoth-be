package chat

import (
	"encoding/json"
	"time"
)

// Roles for a message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Reference is a workspace item the user @-mentioned in a message (a CV or a
// saved job). The id is resolved server-side, scoped to the authenticated user
// — the model never receives a raw id it could act on.
type Reference struct {
	Type string `json:"type"` // RefCV | RefJob
	ID   string `json:"id"`
}

// Reference types.
const (
	RefCV  = "cv"
	RefJob = "job"
)

// Conversation is a row in the conversations table.
type Conversation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message is a row in the messages table.
type Message struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolArgs       json.RawMessage `json:"tool_args,omitempty"`
	ToolResult     json.RawMessage `json:"tool_result,omitempty"`
	SearchJobID    string          `json:"search_job_id,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Event is a single SSE frame streamed to the client during a chat response.
// Mirrors the event format in the implementation plan.
type Event struct {
	Type     string          `json:"type"` // text | tool_call | tool_progress | tool_result | done | error
	Content  string          `json:"content,omitempty"`
	Tool     string          `json:"tool,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
	Status   string          `json:"status,omitempty"`
	Progress int             `json:"progress,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	// Result carries the raw tool payload (same bytes persisted to
	// messages.tool_result) so the client can render rich result cards live,
	// without waiting for a refetch of the persisted conversation.
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}
