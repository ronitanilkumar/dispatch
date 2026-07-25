package delivery

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
	"github.com/ronitanilkumar/dispatch/job"
	"io"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Deliver(ctx context.Context, j *job.Job) (shouldRetry bool, err error) {
	reader := bytes.NewReader(j.Payload)
	
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		j.URL,
		reader,
	)

	if err != nil {
		return false, fmt.Errorf("build webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)

	if err != nil {
		shouldRetry = true
		return shouldRetry, fmt.Errorf("send webhook request: %w", err)
	}

	io.Copy(io.Discard, resp.Body)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		shouldRetry = true
		return shouldRetry, fmt.Errorf("webhook delivery returned status: %d", resp.StatusCode)
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 500 {
		shouldRetry = false
		return shouldRetry, fmt.Errorf("webhook delivery returned status: %d", resp.StatusCode)
	}

	return false, nil
}