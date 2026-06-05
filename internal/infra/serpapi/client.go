package serpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/katgen/thothai/internal/domain/search"
)

const baseURL = "https://serpapi.com/search"

// defaultCountry is the Google `gl` used when none is configured. Without a
// country code Google geolocates the request to the US and city-scoped queries
// (e.g. "Jakarta") return zero results.
const defaultCountry = "id"

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
	apiKey  string
	country string // Google `gl` country code, e.g. "id"
	http    *http.Client
}

// New builds a client. country is the Google `gl` code; empty falls back to
// defaultCountry ("id").
func New(apiKey, country string) *Client {
	if country == "" {
		country = defaultCountry
	}
	return &Client{
		apiKey:  apiKey,
		country: country,
		http:    &http.Client{Timeout: 30 * time.Second},
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
	qp.Set("gl", c.country)
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

	// SerpAPI returns HTTP 200 even when Google served no jobs, signalling it via
	// `error` / `search_information.jobs_results_state`. Decode those so an empty
	// result isn't silently indistinguishable from an API problem.
	var body struct {
		JobsResults       []map[string]any `json:"jobs_results"`
		Error             string           `json:"error"`
		SearchInformation struct {
			JobsResultsState string `json:"jobs_results_state"`
		} `json:"search_information"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode serpapi response: %w", err)
	}

	if len(body.JobsResults) == 0 && body.Error != "" {
		// "Google hasn't returned any results for this query." is a benign empty
		// result, not a failure — log it for visibility and return zero jobs.
		slog.Info("serpapi returned no jobs",
			"query", strings.TrimSpace(q),
			"gl", c.country,
			"serp_error", body.Error,
			"state", body.SearchInformation.JobsResultsState)
		return []map[string]any{}, nil
	}

	return body.JobsResults, nil
}
