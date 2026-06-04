package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"

	"github.com/katgen/thothai/internal/domain/search"
	"github.com/katgen/thothai/internal/infra/middleware"
	redisinfra "github.com/katgen/thothai/internal/infra/redis"
)

type SearchHandler struct {
	svc *search.Service
	rdb *redis.Client
}

func NewSearchHandler(svc *search.Service, rdb *redis.Client) *SearchHandler {
	return &SearchHandler{svc: svc, rdb: rdb}
}

// Register wires the search routes onto a router group.
func (h *SearchHandler) Register(r fiber.Router) {
	r.Post("/initiate", h.initiate)
	r.Get("/stream/:task_id", h.stream)
	r.Get("/results/:task_id", h.results)
	r.Get("/history", h.history)
}

type initiateRequest struct {
	Text string `json:"text"`
}

func (h *SearchHandler) initiate(c *fiber.Ctx) error {
	user := middleware.User(c)

	var req initiateRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Text = strings.TrimSpace(req.Text)
	if len(req.Text) < 3 || len(req.Text) > 500 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "text must be 3–500 characters")
	}

	taskID, err := h.svc.Initiate(c.Context(), user.UserID, req.Text)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"task_id": taskID})
}

func (h *SearchHandler) results(c *fiber.Ctx) error {
	user := middleware.User(c)
	job, err := h.svc.Get(c.Context(), c.Params("task_id"), user.UserID)
	if errors.Is(err, search.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "Task not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(job)
}

func (h *SearchHandler) history(c *fiber.Ctx) error {
	user := middleware.User(c)
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	items, total, err := h.svc.List(c.Context(), user.UserID, limit, offset)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if items == nil {
		items = []search.SearchJob{}
	}
	return c.JSON(fiber.Map{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// stream proxies the task's Redis pub/sub channel to the client as SSE.
func (h *SearchHandler) stream(c *fiber.Ctx) error {
	user := middleware.User(c)
	taskID := c.Params("task_id")

	job, err := h.svc.Get(c.Context(), taskID, user.UserID)
	if errors.Is(err, search.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "Task not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // disable Nginx/Caddy buffering for SSE

	// Terminal states: emit a single frame and close, no subscription needed.
	if job.Status == search.StatusCompleted {
		ev := search.StreamEvent{Status: search.StatusCompleted, Progress: 100, Result: job.Result}
		return writeSingleEvent(c, ev)
	}
	if job.Status == search.StatusFailed {
		ev := search.StreamEvent{Status: search.StatusFailed, Progress: 0, Error: job.Error}
		return writeSingleEvent(c, ev)
	}

	channel := redisinfra.TaskChannel(taskID)
	rdb := h.rdb

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		pubsub := rdb.Subscribe(ctx, channel)
		defer pubsub.Close()
		ch := pubsub.Channel()

		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				// SSE comment keeps the connection alive through proxies.
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", msg.Payload); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}

				var ev search.StreamEvent
				if err := json.Unmarshal([]byte(msg.Payload), &ev); err == nil {
					if ev.Status == search.StatusCompleted || ev.Status == search.StatusFailed {
						return
					}
				}
			}
		}
	}))
	return nil
}

func writeSingleEvent(c *fiber.Ctx, ev search.StreamEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		fmt.Fprintf(w, "data: %s\n\n", data)
		w.Flush()
	}))
	return nil
}
