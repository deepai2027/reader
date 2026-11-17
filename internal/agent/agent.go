// Package agent is Reader's HTTP client for polling the Agent API, per
// the README's documented contract. See
// specs/2026-11-10-mvp/plan.md Group 7 and
// specs/2026-11-10-mvp/requirements.md's "Agent contract assumptions"
// open item: this is built strictly from documentation, with no real
// Agent instance to validate against.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrNoFileAvailable is returned by Poll when the Agent has nothing to
// hand out right now - an expected wait condition for the caller's
// polling loop, never a failure to log as an error.
var ErrNoFileAvailable = errors.New("agent: no file available")

// ErrFileInReading is returned by Poll when the Agent responds 409
// file_in_reading (README's single-file concurrency contract) - also
// an expected wait condition, never a hard error.
var ErrFileInReading = errors.New("agent: file already in reading")

// PolledFile is what a successful Poll call returns.
type PolledFile struct {
	FileID   string `json:"file_id"`
	Priority int    `json:"priority"`
}

// Client polls the Agent API for the next file to process.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// New returns a Client. httpClient should already have an explicit
// Timeout set (per specs/tech-stack.md); New does not set one.
func New(httpClient *http.Client, baseURL, apiKey string) *Client {
	return &Client{httpClient: httpClient, baseURL: baseURL, apiKey: apiKey}
}

// Poll asks the Agent for the next file to process.
//
// NOTE: the endpoint path "/api/reader/new_file/", the request/response shapes,
// and the status-code semantics below are this implementation's best
// reading of the README's documented Agent contract - there is no
// real Agent instance available to validate against (see
// requirements.md). Confirm these against the real Agent team before
// deploying against an actual Agent deployment.
func (c *Client) Poll(ctx context.Context) (PolledFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/reader/new_file/", nil)
	if err != nil {
		return PolledFile{}, fmt.Errorf("agent: building poll request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return PolledFile{}, fmt.Errorf("agent: poll request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var file PolledFile
		if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
			return PolledFile{}, fmt.Errorf("agent: decoding poll response: %w", err)
		}
		return file, nil
	case http.StatusNoContent:
		return PolledFile{}, ErrNoFileAvailable
	case http.StatusConflict:
		return PolledFile{}, ErrFileInReading
	default:
		return PolledFile{}, fmt.Errorf("agent: unexpected status %d polling for a file", resp.StatusCode)
	}
}
