// Package pdfparser is an HTTP client for the internal pdf-parser microservice.
package pdfparser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"time"
)

// Client calls the loopback pdf-parser service.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for the pdf-parser service at baseURL (e.g.
// http://127.0.0.1:5001). The timeout allows for the OpenRouter fallback.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

// Result is the parser's response.
type Result struct {
	Text   string `json:"text"`
	Method string `json:"method"` // "pdfplumber" | "openrouter"
}

// Parse uploads PDF bytes and returns the extracted text.
func (c *Client) Parse(ctx context.Context, filename string, data []byte) (*Result, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("write pdf bytes: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse", &body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pdf-parser request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return nil, fmt.Errorf("pdf-parser status %d: %s", resp.StatusCode, e.Error)
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode pdf-parser response: %w", err)
	}
	return &result, nil
}
