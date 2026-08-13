package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/ronitanilkumar/dispatch/api"
	"github.com/ronitanilkumar/dispatch/api/dedup"
	"github.com/ronitanilkumar/dispatch/delivery"
	"github.com/ronitanilkumar/dispatch/monitor"
	"github.com/ronitanilkumar/dispatch/queue"
	"github.com/ronitanilkumar/dispatch/ratelimit"
	"github.com/ronitanilkumar/dispatch/telemetry"
	"github.com/ronitanilkumar/dispatch/worker"
)

const numWorkers = 16
const deliveryTimeout = 5 * time.Second
const shutdownTimeout = 5 * time.Second
const ttl = 5 * time.Minute
const sweepInterval = 2 * time.Minute
const maxTokens = 50.0
const refillRate = 10.0
const otlpEndpoint = "localhost:4318"

// sampleInterval is how often queue depth is sampled for the dashboard chart.
// Loopback delivery completes in well under a millisecond, so a coarse interval
// misses the queue entirely and the chart reads flat zero under real load.
const sampleInterval = 100 * time.Millisecond

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	shutdownTracing, err := telemetry.Init(context.Background(), otlpEndpoint)
	if err != nil {
		log.Fatalf("init tracing: %v", err)
	}

	q := queue.NewQueue()
	l := ratelimit.NewLimiter(maxTokens, refillRate)
	client := delivery.NewClient(deliveryTimeout, l)
	mon := monitor.New(numWorkers)
	pool := worker.NewPool(q, client, numWorkers).WithObserver(mon)
	dedupCache := dedup.NewDedupCache(ttl)
	go dedupCache.StartSweeper(sweepInterval)
	handler := api.NewHandler(q, dedupCache).WithObserver(mon)
	dashboard := api.NewDashboardHandler(mon, q.Depth)

	samplerStop := make(chan struct{})
	defer close(samplerStop)
	go mon.StartSampler(sampleInterval, q.Depth, samplerStop)

	pool.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", handler.SubmitJobHandler)
	mux.HandleFunc("DELETE /jobs/{id}", handler.CancelJobHandler)
	mux.HandleFunc("GET /api/queue", dashboard.QueueDepthHandler)
	mux.HandleFunc("GET /api/jobs", dashboard.JobsHandler)
	mux.HandleFunc("GET /api/workers", dashboard.WorkersHandler)
	mux.HandleFunc("GET /api/stats", dashboard.StatsHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: api.CORS(mux),
	}

	serverErrCh := make(chan error, 1)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-serverErrCh:
		log.Printf("server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}

	pool.Stop()

	// Flush after the pool drains so spans from in-flight deliveries are
	// exported. shutdownCtx may already be spent by the drain, so give the
	// export its own budget.
	flushCtx, flushCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer flushCancel()

	if err := shutdownTracing(flushCtx); err != nil {
		log.Printf("tracing shutdown: %v", err)
	}
}
