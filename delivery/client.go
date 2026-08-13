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
	"github.com/ronitanilkumar/dispatch/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	// One span per attempt, not one per job: the backoff wait between attempts
	// is deliberately left outside any span so it shows as a gap on the trace.
	ctx, span := telemetry.Tracer().Start(ctx, "job.deliver", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	span.SetAttributes(
		telemetry.AttrJobID.Int64(j.ID),
		telemetry.AttrPriority.Int(int(j.Priority)),
		telemetry.AttrAttempt.Int(j.Attempts+1),
	)

	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}()

	parsedURL, err := url.ParseRequestURI(j.URL)

	if err != nil {
		return false, fmt.Errorf("invalid job URL: %w", err)
	}

	span.SetAttributes(telemetry.AttrHost.String(parsedURL.Host))

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

	span.SetAttributes(telemetry.AttrStatusCode.Int(resp.StatusCode))

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