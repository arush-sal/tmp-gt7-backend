// Package tui is the terminal dashboard client for the backend HTTP API. The API client and the
// render functions are pure/testable; cmd/tui wires them into an interactive bubbletea app.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arush-sal/garage/backend/rtps/coach"
)

// SessionSummary mirrors the API's GET /v1/sessions element (snake_case JSON).
type SessionSummary struct {
	ID         string    `json:"id"`
	CarID      int32     `json:"car_id"`
	Status     string    `json:"status"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	FrameCount uint64    `json:"frame_count"`
	GapCount   uint64    `json:"gap_count"`
	CurrentLap int32     `json:"current_lap"`
	TotalLaps  int32     `json:"total_laps"`
}

// Client talks to the backend read API.
type Client struct {
	base string
	http *http.Client
}

// NewClient returns a client for the API at base (e.g. http://localhost:8080).
func NewClient(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 10 * time.Second}}
}

// Sessions fetches the session list.
func (c *Client) Sessions(ctx context.Context) ([]SessionSummary, error) {
	var out []SessionSummary
	return out, c.getJSON(ctx, "/v1/sessions", &out)
}

// Coaching fetches one session's coaching report (analysis + coaching layer).
func (c *Client) Coaching(ctx context.Context, sessionID string) (coach.Report, error) {
	var rep coach.Report
	return rep, c.getJSON(ctx, "/v1/sessions/"+url.PathEscape(sessionID)+"/coaching", &rep)
}

// errorBody is the API's stable error shape.
type errorBody struct {
	Error string `json:"error"`
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		var e errorBody
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return fmt.Errorf("%s: %s", resp.Status, e.Error)
		}
		return fmt.Errorf("%s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
