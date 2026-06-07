package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/katgen/thothai/internal/domain/job"
	"github.com/katgen/thothai/internal/infra/middleware"
)

type JobHandler struct {
	svc *job.Service
}

func NewJobHandler(svc *job.Service) *JobHandler {
	return &JobHandler{svc: svc}
}

// Register wires the job-workflow routes onto a router group.
func (h *JobHandler) Register(r fiber.Router) {
	r.Post("/save", h.save)
	r.Get("/saved", h.listSaved)
	// PUT, not PATCH: the shared api-gateway's CORS only allows
	// GET,POST,PUT,DELETE,OPTIONS, so a PATCH preflight is blocked in browsers.
	r.Put("/saved/:id/status", h.updateStatus)
	r.Delete("/saved/:id", h.deleteSaved)
	r.Post("/analyze-url", h.analyzeURL)
	r.Post("/saved/:id/interview-prep", h.interviewPrep)
}

func (h *JobHandler) save(c *fiber.Ctx) error {
	user := middleware.User(c)
	var j job.SavedJob
	if err := c.BodyParser(&j); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(j.Title) == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "title is required")
	}
	saved, err := h.svc.Save(c.Context(), user.UserID, j)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(saved)
}

func (h *JobHandler) listSaved(c *fiber.Ctx) error {
	user := middleware.User(c)
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !job.ValidStatus(status) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid status filter")
	}
	jobs, err := h.svc.List(c.Context(), user.UserID, status)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if jobs == nil {
		jobs = []job.SavedJob{}
	}
	return c.JSON(fiber.Map{"items": jobs})
}

type updateStatusRequest struct {
	Status string `json:"status"`
}

func (h *JobHandler) updateStatus(c *fiber.Ctx) error {
	user := middleware.User(c)
	var req updateStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if !job.ValidStatus(req.Status) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "invalid status")
	}
	updated, err := h.svc.UpdateStatus(c.Context(), c.Params("id"), user.UserID, req.Status)
	if errors.Is(err, job.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "saved job not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(updated)
}

func (h *JobHandler) deleteSaved(c *fiber.Ctx) error {
	user := middleware.User(c)
	err := h.svc.Delete(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, job.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "saved job not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type analyzeURLRequest struct {
	URL string `json:"url"`
}

func (h *JobHandler) analyzeURL(c *fiber.Ctx) error {
	var req analyzeURLRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.URL) == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "url is required")
	}
	analysis, err := h.svc.AnalyzeURL(c.Context(), req.URL)
	if err != nil {
		// SSRF rejections and fetch failures are client-supplied-URL problems.
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(analysis)
}

func (h *JobHandler) interviewPrep(c *fiber.Ctx) error {
	user := middleware.User(c)
	prep, err := h.svc.InterviewPrep(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, job.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "saved job not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"interview_prep": prep})
}
