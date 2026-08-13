// Command benchmarks runs load scenarios against Dispatch, measuring
// throughput and end-to-end latency percentiles while verifying exactly-once
// delivery and priority ordering under real concurrent contention.
//
// See benchmarks/README.md for usage.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ronitanilkumar/dispatch/api"
	"github.com/ronitanilkumar/dispatch/api/dedup"
	"github.com/ronitanilkumar/dispatch/delivery"
	"github.com/ronitanilkumar/dispatch/job"
	"github.com/ronitanilkumar/dispatch/queue"
	"github.com/ronitanilkumar/dispatch/ratelimit"
	"github.com/ronitanilkumar/dispatch/worker"
)

const (
	deliveryTimeout = 5 * time.Second
	dedupTTL        = 5 * time.Minute

	// The delivery rate limiter is per-destination-host, and every job in a
	// benchmark targets the same sink host. Production defaults (50 burst, 10/s
	// refill) would therefore cap sustained throughput at ~10 jobs/sec and the
	// benchmark would be measuring the rate limiter rather than the queue,
	// worker pool, and delivery path. These values are set high enough to take
	// the limiter out of the measurement path. See README.md.
	benchMaxTokens  = 1_000_000.0
	benchRefillRate = 1_000_000.0

	// Production defaults, mirroring cmd/dispatch/main.go. Used by the
	// realistic-latency scenarios.
	prodMaxTokens  = 50.0
	prodRefillRate = 10.0

	// realisticRTT stands in for wide-area webhook delivery latency. Loopback
	// delivery costs microseconds, 2-3 orders of magnitude off from what a real
	// HTTPS call to an external endpoint costs.
	realisticRTT = 50 * time.Millisecond

	// How long to wait for the pool to drain after submission completes before
	// declaring jobs missing.
	drainTimeout = 60 * time.Second

	// Rate-limited scenarios drain far slower (a 150-job burst to one host at
	// 10/s takes ~15s of token refill alone, plus retry backoff).
	realisticDrainTimeout = 180 * time.Second

	// quiesceWindow is how long the sink must see zero new deliveries before a
	// drop-expecting scenario concludes nothing else is in flight. It must
	// exceed the largest backoff a job can be sleeping through: at attempt 4
	// that is up to 4s*0.5 + jitter, so 25s is comfortably clear of it.
	quiesceWindow = 25 * time.Second

	// maxAttempts mirrors worker.maxAttempts, which is unexported. Used only
	// for reporting; if the worker constant changes this should follow.
	maxAttempts = 5
)

// submission records what the benchmark sent and when, for latency and
// ordering analysis.
type submission struct {
	jobID       int64
	priority    int
	submittedAt time.Time
}

type scenario struct {
	name        string
	jobs        int
	submitters  int
	workers     int
	failFirst   int
	sinkLatency time.Duration
	// mixedPriority submits a spread of High/Normal/Low instead of all Low.
	mixedPriority bool

	// maxTokens/refillRate configure the per-host rate limiter. Zero means use
	// the isolated (effectively unlimited) benchmark defaults.
	maxTokens  float64
	refillRate float64

	// realistic marks scenarios run with production rate limits and simulated
	// WAN latency. Under those settings a burst to a single host can exceed
	// what the retry ceiling can absorb, so drops are an expected, measured
	// outcome rather than a correctness failure. See README.md.
	realistic bool
}

// limits returns the rate limiter settings for this scenario, defaulting to the
// isolated values that take the limiter out of the measurement path.
func (sc scenario) limits() (maxTokens, refillRate float64) {
	if sc.maxTokens == 0 && sc.refillRate == 0 {
		return benchMaxTokens, benchRefillRate
	}
	return sc.maxTokens, sc.refillRate
}

// drain returns how long to wait for deliveries to finish after submission.
func (sc scenario) drain() time.Duration {
	if sc.realistic {
		return realisticDrainTimeout
	}
	return drainTimeout
}

type result struct {
	scenario   scenario
	throughput float64
	wallClock  time.Duration
	// submitRate is jobs/sec accepted by the HTTP API, ignoring delivery. When
	// this is close to throughput, the benchmark's own submission loop — not
	// Dispatch's delivery path — is what is being measured.
	submitRate  float64
	latency     latencyStats
	correctness correctness
	retried     bool
}

// harness owns one fully wired Dispatch instance plus its sink.
type harness struct {
	sink    *sink
	pool    *worker.Pool
	server  *httptest.Server
	queue   *queue.Queue
	client  *http.Client
	baseURL string
}

func newHarness(sc scenario) *harness {
	s := newSink(sc.failFirst, sc.sinkLatency)

	q := queue.NewQueue()
	maxTokens, refillRate := sc.limits()
	l := ratelimit.NewLimiter(maxTokens, refillRate)
	dc := delivery.NewClient(deliveryTimeout, l)
	pool := worker.NewPool(q, dc, sc.workers)
	dedupCache := dedup.NewDedupCache(dedupTTL)
	handler := api.NewHandler(q, dedupCache)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", handler.SubmitJobHandler)
	mux.HandleFunc("DELETE /jobs/{id}", handler.CancelJobHandler)

	server := httptest.NewServer(mux)

	pool.Start()

	return &harness{
		sink:   s,
		pool:   pool,
		server: server,
		queue:  q,
		// A dedicated client with a large connection pool; the default
		// MaxIdleConnsPerHost of 2 would throttle concurrent submitters and
		// show up as submission latency that has nothing to do with Dispatch.
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        512,
				MaxIdleConnsPerHost: 512,
			},
		},
		baseURL: server.URL,
	}
}

func (h *harness) close() {
	h.pool.Stop()
	h.server.Close()
	h.sink.close()
}

// submitOne posts a single job and returns the ID Dispatch assigned it.
func (h *harness) submitOne(idemKey string, priority int, jobIdx int64) (int64, error) {
	payload, err := json.Marshal(sinkPayload{JobID: jobIdx, Priority: priority})
	if err != nil {
		return 0, fmt.Errorf("marshal payload: %w", err)
	}

	body, err := json.Marshal(map[string]any{
		"payload":  json.RawMessage(payload),
		"priority": priority,
		"url":      h.sink.url(),
		"idemKey":  idemKey,
	})

	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := h.client.Post(h.baseURL+"/jobs", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("submit request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		msg, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("submit returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}

	var decoded api.SubmitJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return 0, fmt.Errorf("decode submit response: %w", err)
	}

	return decoded.ID, nil
}

// priorityFor spreads jobs across priorities so ordering can be checked.
func priorityFor(idx int, mixed bool) int {
	if !mixed {
		return int(job.Low)
	}

	switch idx % 3 {
	case 0:
		return int(job.High)
	case 1:
		return int(job.Normal)
	default:
		return int(job.Low)
	}
}

func run(sc scenario) (result, error) {
	h := newHarness(sc)
	defer h.close()

	submissions := make([]submission, sc.jobs)
	errs := make([]error, sc.jobs)

	// The payload JobID is the benchmark's own index rather than Dispatch's
	// assigned ID, because the sink needs to attribute a delivery before the
	// submitter has necessarily recorded the response.
	var wg sync.WaitGroup
	work := make(chan int, sc.jobs)

	for i := 0; i < sc.jobs; i++ {
		work <- i
	}
	close(work)

	start := time.Now()

	for w := 0; w < sc.submitters; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				priority := priorityFor(idx, sc.mixedPriority)
				sentAt := time.Now()

				id, err := h.submitOne(
					fmt.Sprintf("bench-%s-%d", sc.name, idx),
					priority,
					int64(idx),
				)

				if err != nil {
					errs[idx] = err
					continue
				}

				submissions[idx] = submission{
					jobID:       int64(idx),
					priority:    priority,
					submittedAt: sentAt,
				}
				_ = id
			}
		}()
	}

	wg.Wait()
	submitDone := time.Now()
	submitWall := submitDone.Sub(start)

	for _, err := range errs {
		if err != nil {
			return result{}, fmt.Errorf("submission failed: %w", err)
		}
	}

	// Wait for the pool to drain rather than sleeping a fixed amount: poll
	// until the sink has every job or the timeout expires.
	//
	// Scenarios that expect drops would otherwise always burn the full timeout,
	// since the final count never reaches sc.jobs. For those, stop once the
	// system goes quiet for longer than the maximum retry backoff, which means
	// nothing further is in flight.
	deadline := time.Now().Add(sc.drain())
	lastCount := -1
	lastProgress := time.Now()

	for time.Now().Before(deadline) {
		count := h.sink.deliveredCount()
		if count >= sc.jobs {
			break
		}

		if count != lastCount {
			lastCount = count
			lastProgress = time.Now()
		} else if sc.realistic && time.Since(lastProgress) > quiesceWindow {
			break
		}

		time.Sleep(2 * time.Millisecond)
	}

	receipts := h.sink.snapshot()

	// Throughput is measured over submission start -> last delivery, which is
	// the interval during which the system was actually doing work.
	var lastDelivery time.Time
	for _, r := range receipts {
		if r.ReceivedAt.After(lastDelivery) {
			lastDelivery = r.ReceivedAt
		}
	}

	if lastDelivery.IsZero() {
		lastDelivery = submitDone
	}

	wall := lastDelivery.Sub(start)

	submittedIDs := make([]int64, 0, sc.jobs)
	for _, s := range submissions {
		submittedIDs = append(submittedIDs, s.jobID)
	}

	c := checkExactlyOnce(submittedIDs, receipts)
	c.expectDrops = sc.realistic

	// End-to-end latency: submission -> successful delivery at the sink.
	submittedAt := make(map[int64]time.Time, len(submissions))
	for _, s := range submissions {
		submittedAt[s.jobID] = s.submittedAt
	}

	samples := make([]time.Duration, 0, len(receipts))
	for _, r := range receipts {
		if sent, ok := submittedAt[r.JobID]; ok {
			samples = append(samples, r.ReceivedAt.Sub(sent))
		}
	}

	if sc.mixedPriority {
		inv, cmp := checkPriorityOrdering(submissions, receipts)
		c.priorityChecked = true
		c.inversions = inv
		c.comparisons = cmp
		if cmp > 0 {
			c.inversionPct = float64(inv) / float64(cmp) * 100
		}
	}

	throughput := 0.0
	if wall > 0 {
		throughput = float64(len(receipts)) / wall.Seconds()
	}

	submitRate := 0.0
	if submitWall > 0 {
		submitRate = float64(sc.jobs) / submitWall.Seconds()
	}

	return result{
		scenario:    sc,
		throughput:  throughput,
		wallClock:   wall,
		submitRate:  submitRate,
		latency:     computeLatency(samples),
		correctness: c,
		retried:     sc.failFirst > 0,
	}, nil
}

func main() {
	quick := flag.Bool("quick", false, "run a reduced set of scenarios")
	only := flag.String("only", "", "run only scenarios whose name contains this substring")
	flag.Parse()

	scenarios := []scenario{
		{name: "baseline-c1-w16", jobs: 2000, submitters: 1, workers: 16},
		{name: "concurrent-c8-w16", jobs: 2000, submitters: 8, workers: 16},
		{name: "concurrent-c32-w16", jobs: 2000, submitters: 32, workers: 16},
		{name: "concurrent-c64-w16", jobs: 2000, submitters: 64, workers: 16},
		{name: "workers-c32-w4", jobs: 2000, submitters: 32, workers: 4},
		{name: "workers-c32-w32", jobs: 2000, submitters: 32, workers: 32},
		{name: "workers-c32-w64", jobs: 2000, submitters: 32, workers: 64},
		{
			name: "slow-sink-c32-w16", jobs: 1000, submitters: 32, workers: 16,
			sinkLatency: 5 * time.Millisecond,
		},
		{
			name: "retry-c16-w16", jobs: 500, submitters: 16, workers: 16,
			failFirst: 1,
		},
		{
			name: "priority-c32-w16", jobs: 2000, submitters: 32, workers: 16,
			mixedPriority: true,
		},

		// Realistic profile: production per-host rate limits plus simulated WAN
		// RTT. Job counts are much smaller because a 10/s per-host budget means
		// even 150 jobs takes ~15s of pure token refill to clear.
		{
			name: "real-c8-w16", jobs: 150, submitters: 8, workers: 16,
			sinkLatency: realisticRTT, maxTokens: prodMaxTokens,
			refillRate: prodRefillRate, realistic: true,
		},
		{
			name: "real-c32-w16", jobs: 150, submitters: 32, workers: 16,
			sinkLatency: realisticRTT, maxTokens: prodMaxTokens,
			refillRate: prodRefillRate, realistic: true,
		},
		// Worker-count sweep under realistic conditions, to test whether the
		// loopback finding (fewer workers won, being CPU-bound) inverts once
		// workers are blocked on I/O instead of contending for CPU.
		{
			name: "real-c32-w4", jobs: 150, submitters: 32, workers: 4,
			sinkLatency: realisticRTT, maxTokens: prodMaxTokens,
			refillRate: prodRefillRate, realistic: true,
		},
		{
			name: "real-c32-w64", jobs: 150, submitters: 32, workers: 64,
			sinkLatency: realisticRTT, maxTokens: prodMaxTokens,
			refillRate: prodRefillRate, realistic: true,
		},
	}

	if *quick {
		scenarios = scenarios[:3]
	}

	if *only != "" {
		filtered := scenarios[:0:0]
		for _, sc := range scenarios {
			if strings.Contains(sc.name, *only) {
				filtered = append(filtered, sc)
			}
		}
		scenarios = filtered
	}

	if len(scenarios) == 0 {
		log.Fatal("no scenarios matched")
	}

	results := make([]result, 0, len(scenarios))

	for _, sc := range scenarios {
		fmt.Fprintf(os.Stderr, "running %s (jobs=%d submitters=%d workers=%d)...\n",
			sc.name, sc.jobs, sc.submitters, sc.workers)

		r, err := run(sc)
		if err != nil {
			log.Fatalf("scenario %s: %v", sc.name, err)
		}

		results = append(results, r)
	}

	printTable(results)
}
