package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"

	"github.com/katgen/thothai/internal/domain/chat"
	"github.com/katgen/thothai/internal/infra/middleware"
)

// maxMessageChars bounds a single chat message. Generous enough to paste a long
// job description, but a hard ceiling against oversized injection / cost-abuse
// payloads from the chat UI.
const maxMessageChars = 16000

type ChatHandler struct {
	svc *chat.Service
}

func NewChatHandler(svc *chat.Service) *ChatHandler {
	return &ChatHandler{svc: svc}
}

// Register wires the chat routes onto a router group.
func (h *ChatHandler) Register(r fiber.Router) {
	r.Post("/conversations", h.createConversation)
	r.Get("/conversations", h.listConversations)
	r.Get("/conversations/:id", h.getConversation)
	r.Delete("/conversations/:id", h.deleteConversation)
	r.Post("/conversations/:id/messages", h.sendMessage)
}

type createConversationRequest struct {
	Title string `json:"title"`
}

func (h *ChatHandler) createConversation(c *fiber.Ctx) error {
	user := middleware.User(c)
	var req createConversationRequest
	_ = c.BodyParser(&req) // body is optional
	conv, err := h.svc.CreateConversation(c.Context(), user.UserID, strings.TrimSpace(req.Title))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(conv)
}

func (h *ChatHandler) listConversations(c *fiber.Ctx) error {
	user := middleware.User(c)
	convs, err := h.svc.ListConversations(c.Context(), user.UserID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if convs == nil {
		convs = []chat.Conversation{}
	}
	return c.JSON(fiber.Map{"items": convs})
}

func (h *ChatHandler) getConversation(c *fiber.Ctx) error {
	user := middleware.User(c)
	conv, msgs, err := h.svc.GetConversation(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, chat.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "conversation not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if msgs == nil {
		msgs = []chat.Message{}
	}
	return c.JSON(fiber.Map{"conversation": conv, "messages": msgs})
}

func (h *ChatHandler) deleteConversation(c *fiber.Ctx) error {
	user := middleware.User(c)
	err := h.svc.DeleteConversation(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, chat.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "conversation not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// maxReferences caps how many @-mentions a single message may carry.
const maxReferences = 20

type messageReference struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type sendMessageRequest struct {
	Content    string             `json:"content"`
	References []messageReference `json:"references"`
}

// sendMessage streams the assistant's response (and any tool activity) as SSE.
func (h *ChatHandler) sendMessage(c *fiber.Ctx) error {
	user := middleware.User(c)
	conversationID := c.Params("id")

	var req sendMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "content must not be empty")
	}
	// Bound the input: enough to paste a long job description, but a hard cap so a
	// single message can't be stuffed with an oversized prompt-injection / cost
	// payload. Count runes so multibyte text isn't unfairly clipped.
	if utf8.RuneCountInString(req.Content) > maxMessageChars {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "message is too long")
	}

	// Verify ownership before opening the stream so errors return proper status.
	if _, _, err := h.svc.GetConversation(c.Context(), conversationID, user.UserID); err != nil {
		if errors.Is(err, chat.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "conversation not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	// Convert @-mention references. The model never sees these raw ids; the chat
	// service resolves them server-side, scoped to the authenticated user.
	var refs []chat.Reference
	if len(req.References) > maxReferences {
		req.References = req.References[:maxReferences]
	}
	for _, r := range req.References {
		t := strings.TrimSpace(r.Type)
		id := strings.TrimSpace(r.ID)
		if id == "" || (t != chat.RefCV && t != chat.RefJob) {
			continue
		}
		refs = append(refs, chat.Reference{Type: t, ID: id})
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	userID := user.UserID
	content := req.Content

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		emit := func(ev chat.Event) {
			data, err := json.Marshal(ev)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				cancel()
				return
			}
			_ = w.Flush()
		}

		if err := h.svc.SendMessage(ctx, conversationID, userID, content, refs, emit); err != nil {
			// SendMessage already emits an error event for stream failures; this
			// covers setup errors (e.g. context loading) before any frame.
			emit(chat.Event{Type: "error", Error: err.Error()})
		}
	}))
	return nil
}
