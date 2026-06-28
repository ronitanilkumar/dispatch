# Dispatch

A concurrent webhook delivery engine in Go, built to be observable and reliable under load, not just functional.

> **Status: in progress (Tier 1).** This README will be updated as components land. Currently implemented: job model, heap-based priority queue with thread-safe mutex/condition-variable wrapper. In progress: worker pool. Not yet started: HTTP delivery client, submission API.

## What it is

Dispatch accepts webhook jobs via an HTTP API: target URL, JSON payload, priority, and delivers them concurrently through a pool of workers, with every job execution observable from submission to completion.

## Why

Webhook delivery at scale is a real infrastructure problem: bursty traffic, slow or flaky target endpoints, the need for fairness between high- and low-priority jobs, and the requirement that the system degrade gracefully rather than fall over under load. Dispatch is built to explore and demonstrate the concurrency and systems-design patterns that solve this, the same patterns used in production webhook infrastructure at companies like Stripe.

## Architecture (planned, Tier 1)

```
Job submitted via HTTP API
        |
        v
Bounded priority queue (heap, mutex + sync.Cond)
        |
        v
Worker pool (N goroutines)
        |
        v
HTTP delivery client -> target URL
```

- **Priority queue**: `container/heap`-based, ordered by priority (high/normal/low) with earliest-submitted-first as a tiebreaker. Wrapped in a mutex and `sync.Cond` so it's safe under concurrent access and workers block efficiently when empty rather than busy-polling.
- **Worker pool**: a fixed number of goroutines pulling jobs from the queue and delivering them concurrently, with lifecycle managed via `sync.WaitGroup` and graceful shutdown via `context.Context`.
- **HTTP delivery client**: performs the actual webhook POST to the job's target URL.
- **Submission API**: `POST /jobs` to enqueue new work.

## Roadmap

**Tier 1: core engine**
Bounded priority queue, worker pool, HTTP delivery client, submission API.

**Tier 2: full systems story**
Per-domain token bucket rate limiter, hand-built LRU dedup cache, retry with exponential backoff and jitter, job cancellation, graceful shutdown.

**Tier 3: observability**
End-to-end OpenTelemetry tracing exported to Jaeger, load benchmarks.

## Tech

Go, `container/heap`, `sync` (Mutex, Cond, WaitGroup), `context`, `net/http`.

## License

TBD.
