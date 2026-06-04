package handlers

import (
	"errors"
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/katgen/thothai/internal/domain/cv"
	"github.com/katgen/thothai/internal/infra/middleware"
	"github.com/katgen/thothai/internal/infra/upload"
)

type CVHandler struct {
	svc          *cv.Service
	maxFileBytes int64
}

func NewCVHandler(svc *cv.Service, maxFileBytes int64) *CVHandler {
	return &CVHandler{svc: svc, maxFileBytes: maxFileBytes}
}

// Register wires the CV routes onto a router group.
func (h *CVHandler) Register(r fiber.Router) {
	r.Post("/", h.upload)
	r.Get("/", h.list)
	r.Get("/:id", h.get)
	r.Delete("/:id", h.delete)
	r.Patch("/:id/default", h.setDefault)
	r.Post("/:id/analyze", h.analyze)
	r.Get("/:id/download", h.download)
	r.Post("/match", h.match)
	r.Post("/:id/cover-letter", h.coverLetter)
	r.Post("/:id/suggest-edits", h.suggestEdits)
}

func (h *CVHandler) upload(c *fiber.Ctx) error {
	user := middleware.User(c)

	fh, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "missing 'file' field")
	}
	// Reject oversize before reading the whole body.
	if fh.Size > h.maxFileBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "file too large")
	}

	f, err := fh.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "cannot read upload")
	}
	defer f.Close()

	// Hard cap the read in case Size lied.
	data, err := io.ReadAll(io.LimitReader(f, h.maxFileBytes+1))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "cannot read upload")
	}

	v, err := upload.ValidatePDF(data, fh.Filename, h.maxFileBytes)
	if err != nil {
		switch {
		case errors.Is(err, upload.ErrTooLarge):
			return fiber.NewError(fiber.StatusRequestEntityTooLarge, err.Error())
		case errors.Is(err, upload.ErrNotPDF), errors.Is(err, upload.ErrEncryptedPDF), errors.Is(err, upload.ErrEmpty):
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		default:
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}

	created, err := h.svc.Upload(c.Context(), user.UserID, v, data)
	if errors.Is(err, cv.ErrQuotaExceeded) {
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(created)
}

func (h *CVHandler) list(c *fiber.Ctx) error {
	user := middleware.User(c)
	cvs, err := h.svc.List(c.Context(), user.UserID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if cvs == nil {
		cvs = []cv.CV{}
	}
	return c.JSON(fiber.Map{"items": cvs})
}

func (h *CVHandler) get(c *fiber.Ctx) error {
	user := middleware.User(c)
	got, err := h.svc.Get(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "cv not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(got)
}

func (h *CVHandler) delete(c *fiber.Ctx) error {
	user := middleware.User(c)
	err := h.svc.Delete(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "cv not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *CVHandler) setDefault(c *fiber.Ctx) error {
	user := middleware.User(c)
	err := h.svc.SetDefault(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "cv not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *CVHandler) analyze(c *fiber.Ctx) error {
	user := middleware.User(c)
	err := h.svc.Reanalyze(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "cv not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	got, _ := h.svc.Get(c.Context(), c.Params("id"), user.UserID)
	return c.JSON(got)
}

func (h *CVHandler) download(c *fiber.Ctx) error {
	user := middleware.User(c)
	url, err := h.svc.DownloadURL(c.Context(), c.Params("id"), user.UserID)
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "cv not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Redirect(url, fiber.StatusTemporaryRedirect)
}

type matchRequest struct {
	CVID           string `json:"cv_id"`
	JobDescription string `json:"job_description"`
}

func (h *CVHandler) match(c *fiber.Ctx) error {
	user := middleware.User(c)
	var req matchRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.JobDescription) == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "job_description is required")
	}

	var (
		result any
		err    error
	)
	if strings.TrimSpace(req.CVID) != "" {
		result, err = h.svc.MatchToJob(c.Context(), req.CVID, user.UserID, req.JobDescription)
	} else {
		result, err = h.svc.MatchDefaultToJob(c.Context(), user.UserID, req.JobDescription)
	}
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "no CV found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(result)
}

type coverLetterRequest struct {
	JobDescription string `json:"job_description"`
}

func (h *CVHandler) coverLetter(c *fiber.Ctx) error {
	user := middleware.User(c)
	var req coverLetterRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.JobDescription) == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "job_description is required")
	}

	letter, err := h.svc.CoverLetter(c.Context(), c.Params("id"), user.UserID, req.JobDescription)
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "cv not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"cover_letter": letter})
}

func (h *CVHandler) suggestEdits(c *fiber.Ctx) error {
	user := middleware.User(c)
	var req coverLetterRequest // reuses { job_description }
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.JobDescription) == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "job_description is required")
	}

	edits, err := h.svc.SuggestEdits(c.Context(), c.Params("id"), user.UserID, req.JobDescription)
	if errors.Is(err, cv.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "cv not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(edits)
}
