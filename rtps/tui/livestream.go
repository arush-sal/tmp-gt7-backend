package tui

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// StreamLive connects to the SSE live stream and emits one LiveSnapshot per event. It reconnects with
// a short backoff whenever the connection drops, until ctx is cancelled. Connection errors (dial
// failures, non-OK HTTP status) are delivered on the error channel; the stream keeps retrying after a
// backoff pause. That backoff is applied before every reconnect attempt, including a clean server EOF,
// so a server that closes the stream immediately cannot turn this into a busy-spin reconnect loop.
// Malformed SSE events are skipped silently, not delivered as errors. Both channels are closed when
// ctx is done.
func (c *Client) StreamLive(ctx context.Context) (<-chan LiveSnapshot, <-chan error) {
	snaps := make(chan LiveSnapshot)
	errs := make(chan error, 1)
	go func() {
		defer close(snaps)
		defer close(errs)
		const backoff = 250 * time.Millisecond
		for ctx.Err() == nil {
			err := c.readStream(ctx, snaps)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				select {
				case errs <- err:
				default:
				}
			}
			// Always pause before reconnecting — a clean EOF (readStream returns nil) must not
			// re-dial instantly, or a server that closes the stream turns this into a hammer loop.
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}()
	return snaps, errs
}

// readStream holds one connection open, parsing SSE events until it errors or ctx ends. It uses a
// dedicated HTTP client with no request timeout: c.http carries a 10s timeout meant for the short
// request/response calls in client.go, but that timeout applies to the whole round trip including
// body reads, which would kill a long-lived SSE connection after 10s. ctx cancellation is the only
// way this request should end.
func (c *Client) readStream(ctx context.Context, out chan<- LiveSnapshot) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/live/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	streamHTTP := &http.Client{Transport: c.http.Transport}
	resp, err := streamHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("live stream: unexpected status %s", resp.Status)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // frames are large (all fields)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "": // event boundary — dispatch what we accumulated
			if data.Len() > 0 {
				if s, err := ParseSnapshot([]byte(data.String())); err == nil {
					select {
					case out <- s:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				data.Reset()
			}
		case strings.HasPrefix(line, ":"): // comment / heartbeat — ignore
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return sc.Err()
}
