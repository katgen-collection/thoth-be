package http

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestClientIP(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString(clientIP(c)) })

	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{
			name:    "X-Real-IP wins (set by Caddy, unspoofable)",
			headers: map[string]string{"X-Real-IP": "9.9.9.9", "X-Forwarded-For": "1.1.1.1"},
			want:    "9.9.9.9",
		},
		{
			name:    "falls back to the last X-Forwarded-For hop",
			headers: map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"},
			want:    "5.6.7.8",
		},
		{
			name:    "client spoof in XFF is ignored (real IP is appended last)",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.9, 8.8.8.8"},
			want:    "8.8.8.8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(fiber.MethodGet, "/", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			if got := string(body); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
