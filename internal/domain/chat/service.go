package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/katgen/thothai/internal/infra/ai"
)

// maxToolIterations caps the agentic loop so a misbehaving model cannot spin
// forever calling tools.
const maxToolIterations = 5

const systemPrompt = `You are Thothai, a friendly AI job-search assistant. You help users find jobs,
analyze their CVs, and manage their job applications. The user may write in Indonesian or English —
reply in the same language they use.

You have tools available. Use a tool only when it is needed to answer the user's request; otherwise
just reply directly and conversationally. After a tool returns, summarize the result for the user in
natural language — do not dump raw JSON.`

// Chatter is the streaming LLM interface the service needs.
type Chatter interface {
	StreamChat(ctx context.Context, messages []ai.Message, tools []ai.ToolSchema, onText func(string)) (string, []ai.ToolCall, error)
}

// Service handles conversation CRUD and the streaming tool-calling loop.
type Service struct {
	repo  *Repository
	ai    Chatter
	tools map[string]Tool
}

func NewService(repo *Repository, chatter Chatter, tools []Tool) *Service {
	reg := make(map[string]Tool, len(tools))
	for _, t := range tools {
		reg[t.Schema.Name] = t
	}
	return &Service{repo: repo, ai: chatter, tools: reg}
}

// CreateConversation creates a new conversation for a user.
func (s *Service) CreateConversation(ctx context.Context, userID, title string) (*Conversation, error) {
	return s.repo.CreateConversation(ctx, userID, title)
}

// ListConversations lists a user's conversations.
func (s *Service) ListConversations(ctx context.Context, userID string) ([]Conversation, error) {
	return s.repo.ListConversations(ctx, userID)
}

// GetConversation returns a conversation plus its messages.
func (s *Service) GetConversation(ctx context.Context, id, userID string) (*Conversation, []Message, error) {
	conv, err := s.repo.GetConversation(ctx, id, userID)
	if err != nil {
		return nil, nil, err
	}
	msgs, err := s.repo.ListMessages(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return conv, msgs, nil
}

// DeleteConversation deletes a conversation and its messages.
func (s *Service) DeleteConversation(ctx context.Context, id, userID string) error {
	return s.repo.DeleteConversation(ctx, id, userID)
}

// toolSchemas returns the schemas exposed to the model.
func (s *Service) toolSchemas() []ai.ToolSchema {
	schemas := make([]ai.ToolSchema, 0, len(s.tools))
	for _, t := range s.tools {
		schemas = append(schemas, t.Schema)
	}
	return schemas
}

// SendMessage persists the user's message, runs the streaming tool-calling loop,
// persists the assistant/tool messages, and forwards every event via emit.
// The caller (HTTP handler) turns each Event into an SSE frame.
func (s *Service) SendMessage(ctx context.Context, conversationID, userID, content string, emit func(Event)) error {
	conv, err := s.repo.GetConversation(ctx, conversationID, userID)
	if err != nil {
		return err
	}

	// Persist the user message and seed the conversation title from it.
	if _, err := s.repo.AddMessage(ctx, Message{
		ConversationID: conv.ID, Role: RoleUser, Content: content,
	}); err != nil {
		return err
	}
	_ = s.repo.UpdateTitleIfEmpty(ctx, conv.ID, truncateTitle(content))

	// Build the model context: system prompt + prior user/assistant text. The
	// just-persisted user message is included via the reload inside buildHistory.
	msgs, err := s.buildHistory(ctx, conv.ID)
	if err != nil {
		return err
	}

	for iter := 0; iter < maxToolIterations; iter++ {
		assistantText, toolCalls, err := s.ai.StreamChat(ctx, msgs, s.toolSchemas(), func(t string) {
			emit(Event{Type: "text", Content: t})
		})
		if err != nil {
			emit(Event{Type: "error", Error: err.Error()})
			return err
		}

		// Record the assistant turn in the in-memory context.
		msgs = append(msgs, ai.Message{Role: RoleAssistant, Content: assistantText, ToolCalls: toolCalls})

		if len(toolCalls) == 0 {
			// Final answer — persist and finish.
			if assistantText != "" {
				_, _ = s.repo.AddMessage(ctx, Message{
					ConversationID: conv.ID, Role: RoleAssistant, Content: assistantText,
				})
			}
			_ = s.repo.Touch(ctx, conv.ID)
			emit(Event{Type: "done"})
			return nil
		}

		// Persist any assistant text that preceded the tool calls.
		if assistantText != "" {
			_, _ = s.repo.AddMessage(ctx, Message{
				ConversationID: conv.ID, Role: RoleAssistant, Content: assistantText,
			})
		}

		// Execute each requested tool, streaming progress and feeding results back.
		for _, call := range toolCalls {
			emit(Event{Type: "tool_call", Tool: call.Name, Args: json.RawMessage(call.Args)})

			tool, ok := s.tools[call.Name]
			if !ok {
				msg := fmt.Sprintf("unknown tool %q", call.Name)
				emit(Event{Type: "tool_result", Tool: call.Name, Summary: msg})
				msgs = append(msgs, ai.Message{Role: RoleTool, ToolCallID: call.ID, Content: msg})
				continue
			}

			outcome, err := tool.Execute(ctx, ToolContext{UserID: userID, Emit: emit}, json.RawMessage(call.Args))
			if err != nil {
				msg := fmt.Sprintf("Tool %s gagal: %v", call.Name, err)
				emit(Event{Type: "tool_result", Tool: call.Name, Summary: msg})
				msgs = append(msgs, ai.Message{Role: RoleTool, ToolCallID: call.ID, Content: msg})
				_, _ = s.repo.AddMessage(ctx, Message{
					ConversationID: conv.ID, Role: RoleTool, Content: msg,
					ToolName: call.Name, ToolArgs: json.RawMessage(call.Args),
				})
				continue
			}

			emit(Event{Type: "tool_result", Tool: call.Name, Summary: outcome.Summary, Result: outcome.Raw})
			msgs = append(msgs, ai.Message{Role: RoleTool, ToolCallID: call.ID, Content: outcome.LLMContent})
			_, _ = s.repo.AddMessage(ctx, Message{
				ConversationID: conv.ID, Role: RoleTool, Content: outcome.Summary,
				ToolName:    call.Name,
				ToolArgs:    json.RawMessage(call.Args),
				ToolResult:  outcome.Raw,
				SearchJobID: outcome.SearchJobID,
			})
		}
		// Loop again so the model can turn tool output into a natural reply.
	}

	// Tool-iteration budget exhausted.
	emit(Event{Type: "error", Error: "tool iteration limit reached"})
	return fmt.Errorf("tool iteration limit reached for conversation %s", conv.ID)
}

// buildHistory loads prior user/assistant text turns (tool rows are skipped to
// keep the replayed context valid for the model). The current user message is
// already persisted, so it is included by the reload.
func (s *Service) buildHistory(ctx context.Context, conversationID string) ([]ai.Message, error) {
	stored, err := s.repo.ListMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	msgs := []ai.Message{{Role: "system", Content: systemPrompt}}
	for _, m := range stored {
		if m.Role != RoleUser && m.Role != RoleAssistant {
			continue
		}
		if m.Content == "" {
			continue
		}
		msgs = append(msgs, ai.Message{Role: m.Role, Content: m.Content})
	}
	return msgs, nil
}

func truncateTitle(s string) string {
	const max = 60
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
