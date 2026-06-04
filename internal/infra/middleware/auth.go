package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// CurrentUser is the authenticated principal, populated from the X-User-* headers
// injected by api-gateway. Same trust model as chat-service.
type CurrentUser struct {
	UserID   string
	Email    string
	Username string
	Roles    []string
}

const currentUserKey = "currentUser"

// Auth reads the X-User-* headers set by api-gateway. The gateway has already
// validated the JWT — this service only trusts the headers. Requests without
// X-User-Id are rejected with 401.
func Auth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-Id")
		if userID == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "Unauthorized")
		}

		var roles []string
		if r := c.Get("X-User-Roles"); r != "" {
			for _, part := range strings.Split(r, ",") {
				if t := strings.TrimSpace(part); t != "" {
					roles = append(roles, t)
				}
			}
		}

		c.Locals(currentUserKey, &CurrentUser{
			UserID:   userID,
			Email:    c.Get("X-User-Email"),
			Username: c.Get("X-User-Username"),
			Roles:    roles,
		})
		return c.Next()
	}
}

// User retrieves the authenticated user from the request context. It is only
// valid on routes protected by Auth.
func User(c *fiber.Ctx) *CurrentUser {
	if u, ok := c.Locals(currentUserKey).(*CurrentUser); ok {
		return u
	}
	return nil
}
