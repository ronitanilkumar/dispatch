# Dispatch

A concurrent webhook delivery engine in Go.

Dispatch accepts webhook jobs over HTTP (target URL, JSON payload, priority), queues them, and delivers them through a pool of worker goroutines. It handles retries with exponential backoff, per-destination rate limiting, request deduplication, job cancellation, and full request tracing. There's also a load benchmark suite and a live dashboard for watching jobs move through the system.

## Why

Webhook delivery at scale runs into the same problems every time: bursty traffic, slow or flaky destinations, the need to prioritize some jobs over others, and the requirement that the system degrade gracefully instead of falling over. Dispatch exists to build and demonstrate the concurrency and systems-design patterns that solve this: priority queues under contention, safe goroutine lifecycle management, backoff and retry, per-destination rate limiting, and distributed tracing across goroutine boundaries.

## Architecture

```
HTTP request
     |
     v
SubmitJobHandler (dedup check, validation)
     |
     v
Priority queue (heap, mutex + sync.Cond)
     |
     v
Worker pool (N goroutines)
     |
     v
Delivery client --> rate limiter --> target URL
     |
     v
On retryable failure: backoff, then re-enqueue
```

- **Priority queue** (`queue/`): `container/heap`-based, ordered by priority (High/Normal/Low) with earliest-submitted-first as a tiebreaker. Wrapped in a mutex and `sync.Cond` so workers block efficiently when the queue is empty instead of polling. Unbounded by design: draining takes priority over rejecting work at the queue level.
- **Worker pool** (`worker/`): a fixed number of goroutines pulling jobs from the queue and delivering them concurrently. Lifecycle managed with `sync.WaitGroup`; shutdown drains the queue instead of dropping in-flight work.
- **Delivery client** (`delivery/`): performs the webhook POST and classifies the outcome. 429 and 5xx are retryable; other 4xx are terminal.
- **Rate limiter** (`ratelimit/`): per-destination-host token bucket, so one slow or hostile destination can't affect delivery to any other host.
- **Dedup cache** (`api/dedup/`): rejects resubmission of the same idempotency key within a TTL window.
- **Retry engine** (in `worker/`): exponential backoff with jitter, capped attempts. Retries re-enqueue the same job rather than blocking a worker on a sleep.
- **Tracing** (`telemetry/`, wired through `job/`, `api/`, `worker/`, `delivery/`): every job's full lifecycle, submission through every delivery attempt, is one OpenTelemetry trace, exported to Jaeger.
- **Monitor + dashboard API** (`monitor/`, `api/dashboard.go`): tracks live job state and worker activity for the dashboard, without touching the hot path's locking.
- **Benchmarks** (`benchmarks/`): an in-process load-testing harness that verifies exactly-once delivery and priority ordering under real concurrency, not just raw throughput.
- **Dashboard** (`dashboard/`): a React SPA that shows queue depth, worker activity, and live job state over the API above.

## Running it

```bash
go run ./cmd/dispatch
```

Starts the API on `:8080`.

```
POST   /jobs          submit a job
DELETE /jobs/{id}      cancel a queued job
GET    /api/stats      queue depth, worker activity, jobs, retry/failure feed
GET    /api/queue      queue depth + history
GET    /api/jobs       recent jobs
GET    /api/workers    busy/idle worker counts
```

Submit a job:

```bash
curl -X POST localhost:8080/jobs -H 'Content-Type: application/json' -d '{
  "payload": {"hello": "world"},
  "priority": 0,
  "url": "https://example.com/webhook",
  "idemKey": "unique-key-1"
}'
```

`priority` is `0` (High), `1` (Normal), or `2` (Low).

### Tracing

```bash
docker compose up -d          # Jaeger, OTLP/HTTP on :4318, UI on :16686
go run ./cmd/dispatch
```

Every job produces one trace: a submission span, an enqueue span, and one delivery span per attempt (not one span covering all retries, so backoff gaps are visible on the timeline). View traces at `localhost:16686`.

### Dashboard

```bash
go run ./cmd/flakyreceiver     # test webhook target with configurable failure modes
go run ./cmd/dispatch
cd dashboard && npm install && npm run dev
```

Open `localhost:5173`. See `dashboard/README.md` for the demo flow and what each panel reads from.

## Benchmarks

`benchmarks/` is a Go program (not a shell script wrapping a load tool) that drives Dispatch in-process and verifies behavior under concurrency, not just raw speed:

- **Throughput and latency percentiles** (p50/p95/p99), not just an average.
- **Exactly-once delivery**, checked directly against every submitted job ID rather than inferred from HTTP status codes.
- **Priority ordering** under real concurrent contention, reported as an inversion rate rather than a binary pass/fail, since a strictly ordered queue can still show reordering at the delivery level once N workers dequeue concurrently and take variable-length I/O.

```bash
go run ./benchmarks              # full suite, ~5 minutes
go run ./benchmarks -quick       # first three scenarios only
go run ./benchmarks -only real   # realistic-profile scenarios only
```

It runs two profiles. **Isolated** disables the rate limiter and delivers over loopback, measuring the upper-bound capacity of the queue, worker pool, and delivery path. **Realistic** uses production rate limits (50 burst, 10/s per host) and adds 50ms of simulated network latency, measuring what a single destination actually experiences. The two differ by roughly 500x in throughput, and that gap is itself the finding: it's the rate limiter, not Dispatch, setting the ceiling.

### Results

Measured on an 8-core Apple Silicon machine, Go 1.26.2. Single-run numbers; throughput varies meaningfully between runs.

**A. Isolated delivery path** (rate limiter disabled, loopback sink)

| Scenario | Jobs | Submitters | Workers | Throughput (jobs/s) | Submit rate (jobs/s) | p50 | p95 | p99 | max | Exactly-once |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|:---:|
| baseline-c1-w16 | 2000 | 1 | 16 | 6633 | 6634 | 0.16ms | 0.26ms | 0.42ms | 6.0ms | PASS |
| concurrent-c8-w16 | 2000 | 8 | 16 | 7705 | 7705 | 1.2ms | 1.6ms | 2.2ms | 3.5ms | PASS |
| concurrent-c32-w16 | 2000 | 32 | 16 | 9606 | 14055 | 67.3ms | 72.4ms | 73.0ms | 73.8ms | PASS |
| concurrent-c64-w16 | 2000 | 64 | 16 | 9135 | 15251 | 87.2ms | 92.9ms | 95.2ms | 95.5ms | PASS |
| workers-c32-w4 | 2000 | 32 | 4 | 9336 | 18554 | 86.2ms | 104.2ms | 107.2ms | 107.7ms | PASS |
| workers-c32-w32 | 2000 | 32 | 32 | 6227 | 6306 | 8.8ms | 15.2ms | 16.6ms | 17.7ms | PASS |
| workers-c32-w64 | 2000 | 32 | 64 | 4393 | 4393 | 7.1ms | 12.3ms | 17.9ms | 23.8ms | PASS |
| slow-sink-c32-w16 | 1000 | 32 | 16 | 2613 | 9733 | 156.2ms | 268.9ms | 281.6ms | 283.3ms | PASS |
| retry-c16-w16 | 500 | 16 | 16 | 891 | 8088 | 387.6ms | 495.2ms | 503.5ms | 506.9ms | PASS |
| priority-c32-w16 | 2000 | 32 | 16 | 9069 | 13371 | 45.9ms | 142.4ms | 158.7ms | 172.3ms | PASS |

Priority ordering: 29 of 1,333,333 ordered pairs inverted (under 0.01%). Retry scenario: sink returned 503 on the first attempt of all 500 jobs; all 500 still delivered exactly once.

**B. Realistic profile** (production rate limits: 50 burst, 10/s per host; 50ms simulated RTT)

| Scenario | Jobs | Submitters | Workers | Throughput (jobs/s) | Submit rate (jobs/s) | Delivered | Dropped | p50 | p95 | p99 | Exactly-once |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|:---:|
| real-c8-w16 | 150 | 8 | 16 | 16.7 | 6737 | 120 | 30 (20%) | 1.15s | 6.54s | 7.11s | PASS, no dupes |
| real-c32-w16 | 150 | 32 | 16 | 16.8 | 5623 | 121 | 29 (19%) | 1.14s | 6.57s | 7.04s | PASS, no dupes |
| real-c32-w4 | 150 | 32 | 4 | 16.4 | 6631 | 126 | 24 (16%) | 1.59s | 7.06s | 7.55s | PASS, no dupes |
| real-c32-w64 | 150 | 32 | 64 | 17.1 | 5104 | 119 | 31 (21%) | 1.05s | 6.46s | 6.84s | PASS, no dupes |

### The main finding: retry ceiling vs. rate limiter

At production settings, 16 to 21% of a 150-job burst to a single host gets dropped, having exhausted all 5 delivery attempts without ever reaching the destination. This isn't a bug, it's a measurable interaction between two independent limits:

- The per-host limiter refills at 10 tokens/sec, so a 150-job burst needs about 15 seconds of token refill to clear, after the initial 50-token burst is spent.
- A job's total retry budget is about 5.6 seconds: `maxAttempts=5` with exponential backoff from a 500ms base (average waits of roughly 0.38s, 0.75s, 1.5s, 3.0s).

A job that gets rate-limited on every attempt runs out of retries roughly 3x sooner than the queue can drain. The limiter's rejection is classified as retryable, so the job burns its attempt budget on backoff waits instead of on genuine delivery failures, and is eventually dropped.

Zero duplicates occurred in any run. At-most-once delivery held even where at-least-once didn't. The failure mode is dropping, never double-sending, which is the safer direction for webhook delivery.

If this trade-off isn't the one you want: raise `maxAttempts`, raise the backoff ceiling, raise the per-host `refillRate`, or don't consume a retry attempt on a local rate-limit rejection (arguably the most correct fix, since being throttled by your own limiter isn't a delivery failure).

### Worker count: the loopback finding inverts under realistic load

Under the isolated profile, fewer workers won: 4 workers hit 9,336 jobs/s while 64 workers managed only 4,393. Loopback delivery costs microseconds, so workers are effectively CPU-bound, and past the machine's core count, extra goroutines add scheduler and queue-mutex contention without adding useful parallelism.

Under the realistic profile, that finding disappears entirely: 4 workers and 64 workers both deliver about 16 to 17 jobs/s, a spread well inside run-to-run noise. Once each delivery blocks on 50ms of simulated RTT, workers spend nearly all their time parked on I/O rather than competing for CPU, so pool size stops being the constraint. The per-host rate limiter becomes the only bottleneck and pins throughput regardless of worker count.

The "fewer workers is better" result is an artifact of loopback benchmarking, not something that should drive production `numWorkers` tuning. With real network latency the pool is idle-blocked, not contended, and sizing should be driven by target-host concurrency limits instead.

### Caveats

- **The isolated profile's throughput does not describe production.** No DNS, no TCP handshake, no TLS, no bandwidth limit, no packet loss. It measures the queue and worker pool's own overhead, nothing else.
- **The realistic profile's throughput is the rate limiter's number, not Dispatch's.** 17 jobs/s is just the configured 10/s refill plus amortized burst. Changing `refillRate` moves it directly.
- **Several isolated-profile rows are bottlenecked by the benchmark's own submission loop, not by Dispatch.** The Submit rate column shows this: where submit rate is roughly equal to throughput (baseline-c1-w16, concurrent-c8-w16, workers-c32-w64), the benchmark can't generate load fast enough, and the throughput number is a floor, not a ceiling, on what Dispatch can do.
- **Latency percentiles bunch up under saturation.** At 32+ submitters, p50/p95/p99 sit close together because nearly every job waits behind the same queue backlog; the percentiles are measuring queue depth, not per-delivery variance. The low-concurrency rows reflect actual per-delivery cost.
- **Priority ordering is an inversion rate, not pass/fail**, because N workers dequeuing concurrently and taking variable-length I/O means a high-priority job dequeued first can still finish after a low-priority job dequeued a moment later. That's expected behavior for a concurrent pool, not a queue defect. A rate near 0% confirms prioritization works; near 50% would mean ordering is effectively random.

## Tech

Go, `container/heap`, `sync` (Mutex, Cond, WaitGroup), OpenTelemetry, React, TypeScript, Recharts.
