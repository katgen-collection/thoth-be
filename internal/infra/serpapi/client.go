package serpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/katgen/thothai/internal/domain/search"
)

const baseURL = "https://serpapi.com/search"

var employmentTypeMap = map[string]string{
	"full_time": "FULLTIME",
	"part_time": "PARTTIME",
	"contract":  "CONTRACTOR",
	"internship": "INTERN",
}

var experienceMap = map[string]string{
	"entry":  "ENTRY_LEVEL",
	"mid":    "MID_LEVEL",
	"senior": "SENIOR",
}

// Client queries SerpAPI's Google Jobs engine.
type Client struct {
	apiKey string
	http   *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Fetch builds a Google Jobs query from extracted params and returns the raw
// jobs_results array as generic maps (handed straight to the AI filter).
func (c *Client) Fetch(ctx context.Context, params search.ExtractedParams) ([]map[string]any, error) {
	q := params.JobTitle
	if params.Location != "" {
		q = q + " " + params.Location
	}
	if len(params.Keywords) > 0 {
		kw := params.Keywords
		if len(kw) > 3 {
			kw = kw[:3]
		}
		q = q + " " + strings.Join(kw, " ")
	}

	qp := url.Values{}
	qp.Set("engine", "google_jobs")
	qp.Set("q", strings.TrimSpace(q))
	qp.Set("api_key", c.apiKey)
	qp.Set("hl", "en")
	qp.Set("num", "30")

	if v, ok := employmentTypeMap[params.EmploymentType]; ok {
		qp.Set("employment_type", v)
	}
	if v, ok := experienceMap[params.ExperienceLevel]; ok {
		qp.Set("experience_requirements", v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"?"+qp.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serpapi request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serpapi returned status %d", resp.StatusCode)
	}

	var body struct {
		JobsResults []map[string]any `json:"jobs_results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode serpapi response: %w", err)
	}

	return body.JobsResults, nil
}
