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
	Error    string          `json:"error,omitempty"`
}
