package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// Message is a provider-agnostic chat message used by the chat domain so it does
// not depend directly on the OpenAI SDK types.
type Message struct {
	Role       string     // system | user | assistant | tool
	Content    string     // text content (may be empty for tool-call turns)
	ToolCalls  []ToolCall // set on assistant turns that request tools
	ToolCallID string     // set on tool-result messages
}

// ToolCall is a function call the model requested (or a completed one being
// replayed in history).
type ToolCall struct {
	ID   string
	Name string
	Args string // raw JSON arguments
}

// ToolSchema is a function definition exposed to the model.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON Schema object
}

// StreamChat runs one streaming chat completion. Text deltas are forwarded to
// onText as they arrive. It returns the full assistant text and any tool calls
// the model requested this turn.
func (d *DeepSeek) StreamChat(
	ctx context.Context,
	messages []Message,
	tools []ToolSchema,
	onText func(string),
) (string, []ToolCall, error) {
	req := openai.ChatCompletionRequest{
		Model:    d.model,
		Messages: toOpenAIMessages(messages),
		Stream:   true,
	}
	if len(tools) > 0 {
		req.Tools = toOpenAITools(tools)
	}

	stream, err := d.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", nil, err
	}
	defer stream.Close()

	var text strings.Builder

	// Tool-call fragments arrive split across chunks, keyed by index.
	type partial struct {
		id, name, args string
	}
	parts := map[int]*partial{}
	var order []int

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return text.String(), nil, err
		}
		if len(resp.Choices) == 0 {
			continue
		}
		delta := resp.Choices[0].Delta

		if delta.Content != "" {
			text.WriteString(delta.Content)
			if onText != nil {
				onText(delta.Content)
			}
		}

		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			p, ok := parts[idx]
			if !ok {
				p = &partial{}
				parts[idx] = p
				order = append(order, idx)
			}
			if tc.ID != "" {
				p.id = tc.ID
			}
			if tc.Function.Name != "" {
				p.name = tc.Function.Name
			}
			p.args += tc.Function.Arguments
		}
	}

	var calls []ToolCall
	for _, idx := range order {
		p := parts[idx]
		if p.name == "" {
			continue
		}
		calls = append(calls, ToolCall{ID: p.id, Name: p.name, Args: p.args})
	}
	return text.String(), calls, nil
}

func toOpenAIMessages(messages []Message) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, m := range messages {
		om := openai.ChatCompletionMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			idx := 0
			om.ToolCalls = append(om.ToolCalls, openai.ToolCall{
				Index: &idx,
				ID:    tc.ID,
				Type:  openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      tc.Name,
					Arguments: tc.Args,
				},
			})
		}
		out = append(out, om)
	}
	return out
}

func toOpenAITools(tools []ToolSchema) []openai.Tool {
	out := make([]openai.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}
