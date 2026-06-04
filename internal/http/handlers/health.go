package handlers

import "github.com/gofiber/fiber/v2"

// Health is a liveness check (no auth).
func Health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok", "service": "thothai-api"})
}
