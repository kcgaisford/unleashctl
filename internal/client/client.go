// Package client is a thin, hand-written wrapper over the Unleash Admin API,
// built on the generated types in internal/client/gen. Per spec §2, service
// account tokens and personal access tokens are both just bearer tokens, so
// there is a single auth code path for both.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
)

const maxRetries = 3

// Client is a minimal HTTP client for the Unleash Admin API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	// Debug, when true, logs method/URL/status to stderr. The Authorization
	// header value is never logged (spec §8).
	Debug bool
}

// New returns a Client for baseURL, authenticating with token.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is returned when the Admin API responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("unleash admin api: status %d: %s", e.StatusCode, e.Body)
}

// do sends a request, retrying on 429/5xx with exponential backoff, and
// decodes a JSON response body into out (if out is non-nil).
func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Authorization", c.Token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if c.Debug {
			fmt.Printf("unleashctl: %s %s -> %d\n", method, path, resp.StatusCode)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("reading response body: %w", readErr)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
		}

		if out != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("decoding response from %s %s: %w", method, path, err)
			}
		}
		return nil
	}
	return lastErr
}

// GetEnvironments lists environments known to this instance.
func (c *Client) GetEnvironments(ctx context.Context) (*gen.EnvironmentsSchema, error) {
	var out gen.EnvironmentsSchema
	if err := c.do(ctx, http.MethodGet, "/api/admin/environments", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type exportByTagRequest struct {
	Environment string `json:"environment"`
	Tag         string `json:"tag"`
}

// ExportByTag fetches full feature state (features, strategies, tags, etc.)
// for the given environment, scoped to features carrying the given tag
// (typically "service:<name>", per spec §6.4). This is the scoped
// import-export flow (spec §4 tier 2) that diff/apply build on.
func (c *Client) ExportByTag(ctx context.Context, environment, tag string) (*gen.ExportResultSchema, error) {
	var out gen.ExportResultSchema
	req := exportByTagRequest{Environment: environment, Tag: tag}
	if err := c.do(ctx, http.MethodPost, "/api/admin/features-batch/export", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ValidateImport validates a scoped import payload without applying it.
func (c *Client) ValidateImport(ctx context.Context, project, environment string, data gen.ExportResultSchema) (*gen.ImportTogglesValidateSchema, error) {
	var out gen.ImportTogglesValidateSchema
	req := gen.ImportTogglesSchema{Project: project, Environment: environment, Data: data}
	if err := c.do(ctx, http.MethodPost, "/api/admin/features-batch/validate", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Import applies a scoped import payload. The Admin API returns 200 with no
// body on success.
func (c *Client) Import(ctx context.Context, project, environment string, data gen.ExportResultSchema) error {
	req := gen.ImportTogglesSchema{Project: project, Environment: environment, Data: data}
	return c.do(ctx, http.MethodPost, "/api/admin/features-batch/import", req, nil)
}

// GetFeature fetches a single feature by name. Note: unlike the export
// endpoints, this does NOT populate FeatureSchema.Tags (confirmed against a
// live instance) — use GetFeatureTags for that. Returns an *APIError with
// StatusCode 404 if no such feature exists in the project.
func (c *Client) GetFeature(ctx context.Context, project, name string) (*gen.FeatureSchema, error) {
	var out gen.FeatureSchema
	path := fmt.Sprintf("/api/admin/projects/%s/features/%s", project, name)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type featureTagsResponse struct {
	Tags []gen.TagSchema `json:"tags"`
}

// GetFeatureTags fetches the tags attached to a feature. Returns an
// *APIError with StatusCode 404 if no such feature exists.
func (c *Client) GetFeatureTags(ctx context.Context, name string) ([]gen.TagSchema, error) {
	var out featureTagsResponse
	path := fmt.Sprintf("/api/admin/features/%s/tags", name)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Tags, nil
}
