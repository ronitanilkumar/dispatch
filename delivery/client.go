package delivery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ronitanilkumar/dispatch/job"
	"github.com/ronitanilkumar/dispatch/ratelimit"
)

type Client struct {
	httpClient *http.Client
	limiter    *ratelimit.Limiter
}

func NewClient(timeout time.Duration, limiter *ratelimit.Limiter) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		limiter: limiter,
	}
}

func (c *Client) Deliver(ctx context.Context, j *job.Job) (shouldRetry bool, err error) {
	parsedURL, err := url.ParseRequestURI(j.URL)

	if err != nil {
		return false, fmt.Errorf("invalid job URL: %w", err)
	}
	if !c.limiter.Allow(parsedURL.Host) {
		return true, fmt.Errorf("rate limited: too many requests to %s", parsedURL.Host)
	}
	
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