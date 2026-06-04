package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a conversation does not exist or belongs to
// another user.
var ErrNotFound = errors.New("conversation not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// CreateConversation inserts a conversation and returns it.
func (r *Repository) CreateConversation(ctx context.Context, userID, title string) (*Conversation, error) {
	var c Conversation
	err := r.pool.QueryRow(ctx,
		`INSERT INTO conversations (user_id, title)
		 VALUES ($1, NULLIF($2, ''))
		 RETURNING id::text, user_id, COALESCE(title, ''), created_at, updated_at`,
		userID, title,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return &c, nil
}

// ListConversations returns a user's conversations, newest-updated first.
func (r *Repository) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, user_id, COALESCE(title, ''), created_at, updated_at
		 FROM conversations WHERE user_id = $1 ORDER BY updated_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetConversation fetches a single conversation scoped to a user.
func (r *Repository) GetConversation(ctx context.Context, id, userID string) (*Conversation, error) {
	var c Conversation
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, user_id, COALESCE(title, ''), created_at, updated_at
		 FROM conversations WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return &c, nil
}

// DeleteConversation removes a conversation (messages cascade).
func (r *Repository) DeleteConversation(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM conversations WHERE id = $1 AND user_id = $2`, id, userID,
	)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateTitleIfEmpty sets the title only if it is currently null/empty.
func (r *Repository) UpdateTitleIfEmpty(ctx context.Context, id, title string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE conversations SET title = $2, updated_at = now()
		 WHERE id = $1 AND (title IS NULL OR title = '')`,
		id, title,
	)
	return err
}

// Touch bumps updated_at on a conversation.
func (r *Repository) Touch(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1`, id)
	return err
}

// ListMessages returns all messages for a conversation, oldest first.
func (r *Repository) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, conversation_id::text, role, COALESCE(content, ''),
		        COALESCE(tool_name, ''), tool_args, tool_result,
		        COALESCE(search_job_id::text, ''), created_at
		 FROM messages WHERE conversation_id = $1 ORDER BY created_at ASC`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var (
			m          Message
			toolArgs   []byte
			toolResult []byte
		)
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.Role, &m.Content,
			&m.ToolName, &toolArgs, &toolResult, &m.SearchJobID, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		m.ToolArgs = json.RawMessage(toolArgs)
		m.ToolResult = json.RawMessage(toolResult)
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMessage inserts a message and returns its ID. Empty string fields and nil
// JSON are stored as NULL.
func (r *Repository) AddMessage(ctx context.Context, m Message) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO messages
		   (conversation_id, role, content, tool_name, tool_args, tool_result, search_job_id)
		 VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, NULLIF($7, '')::uuid)
		 RETURNING id::text`,
		m.ConversationID, m.Role, m.Content, m.ToolName,
		nullableJSON(m.ToolArgs), nullableJSON(m.ToolResult), m.SearchJobID,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("add message: %w", err)
	}
	return id, nil
}

// nullableJSON returns nil for empty raw JSON so it stores as SQL NULL.
func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}
