# Dispatch dashboard

A single-page dashboard that makes Dispatch's runtime behavior visible: queue
depth, worker pool utilization, live job state transitions, and the retry
classification that otherwise only appears in logs.

Built with Vite + React + TypeScript, Tailwind v4, [lucide](https://lucide.dev)
icons, and Recharts.

## Running it

Three processes. Each in its own terminal, from the repo root:

```bash
# 1. Webhook target (provides /status/{code} and /flaky/{n} routes)
go run ./cmd/flakyreceiver

# 2. Dispatch itself — API on :8080
go run ./cmd/dispatch

# 3. Dashboard — http://localhost:5173
cd dashboard && npm install && npm run dev
```

Then open **http://localhost:5173**.

Vite proxies `/api` and `/jobs` to `localhost:8080`, so the browser stays on one
origin. The Go server also sets permissive CORS headers, so opening the built
assets directly works too.

Jaeger is optional here. If it is not running, Dispatch still serves the API and
the dashboard works — you will just see exporter warnings in its log.

## Demo flow

The point of the dashboard is watching a job move through states, so drive it
with the preset buttons in **Submit test job**:

**1. Happy path.** Click `Succeeds`, submit one job. It appears in the jobs
table and settles on `Succeeded` within a poll cycle or two. Establishes the
baseline.

**2. Retry with visible backoff — the main event.** Click `Fails twice, then OK`
(`/flaky/2`) and submit. The receiver returns 500 for the first two attempts of
that job, then 200. Watch:
- the job move `Pending → In-flight → Retrying → Succeeded`
- the attempts badge climb 1 → 2 → 3
- the **Retry & failure feed** show two `Retryable` entries tagged `HTTP 500`,
  each stating the backoff before the next attempt (e.g. 288ms, then 967ms —
  the exponential growth is visible in the numbers)

**3. Terminal vs. retryable.** Click `404 (terminal)` and submit. The feed shows
a single `Terminal` entry and the job goes straight to `Failed` with no retries.
Side by side with step 2, this is the retry classification made visible: 5xx and
429 are retryable, 4xx is not.

**4. Exhaustion.** Click `Always 500 (exhausts)` and submit. The job retries up
to `maxAttempts` (5), then the feed shows a red `Exhausted` entry.

**5. Concurrency.** Set **How many** to 50 with `Fails twice, then OK` and
submit. The queue depth chart spikes, worker slots light up, and the jobs table
fills. Note that a burst this size against one host also trips the per-host rate
limiter — you will see `rate limited: too many requests` in the feed, which is
real system behavior worth explaining rather than a bug.

## What each panel reads from

| Panel | Endpoint | Notes |
|---|---|---|
| Stat tiles | `GET /api/stats` | queue depth, busy/total workers, status counts |
| Queue chart | `GET /api/stats` | `queue.history`, a rolling window of samples |
| Worker grid | `GET /api/stats` | one square per worker in the pool |
| Jobs table | `GET /api/stats` | most recent 200 jobs, newest first |
| Failure feed | `GET /api/stats` | most recent 100 retry/failure events |

Individual endpoints (`/api/queue`, `/api/jobs`, `/api/workers`) exist and are
usable, but the dashboard polls the combined `/api/stats` every 400ms so every
panel renders one internally consistent snapshot. Separate requests could
interleave with worker updates and show a queue depth disagreeing with the job
list.

## Two things worth knowing about the numbers

**The chart plots peaks, not instantaneous values.** Loopback delivery finishes
in microseconds, so point-in-time polling almost always catches an empty queue
and idle workers even while thousands of jobs flow through. The monitor tracks a
high-water mark per 100ms window instead, which is why the chart shows real
spikes. The stat tiles, by contrast, *are* instantaneous — so "Queue depth 0"
next to a spiking chart is expected, not a contradiction.

**Job history is capped** at 200 jobs and 100 events, oldest evicted first, so a
long-running demo cannot grow memory without bound. The counts in the stat tiles
are over tracked jobs, not all-time totals.
