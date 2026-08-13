package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// receipt is one recorded webhook delivery observed by the sink.
type receipt struct {
	JobID      int64
	Priority   int
	ReceivedAt time.Time
	Seq        int // sink-side arrival order, used for priority-ordering checks
}

// sinkPayload is the body the benchmark submits and the sink decodes. Carrying
// the job's identity in the payload is what lets the sink attribute each
// delivery back to a specific submission without depending on Dispatch
// internals.
type sinkPayload struct {
	JobID    int64 `json:"jobId"`
	Priority int   `json:"priority"`
}

// sink is a local HTTP server standing in for a webhook destination. It records
// every delivery it receives so the benchmark can verify exactly-once delivery
// and inspect arrival ordering, and it can be told to fail the first N attempts
// for a given job to exercise the retry path.
type sink struct {
	server *httptest.Server

	mu       sync.Mutex
	receipts []receipt
	// attempts counts how many times each job has hit the sink, including
	// deliveries that were deliberately rejected.
	attempts map[int64]int
	// failFirst, when > 0, makes the sink reject the first N attempts of every
	// job with a retryable 503.
	failFirst int
	// latency, when > 0, is artificial per-request processing delay.
	latency time.Duration
}

func newSink(failFirst int, latency time.Duration) *sink {
	s := &sink{
		attempts:  make(map[int64]int),
		failFirst: failFirst,
		latency:   latency,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook", s.handle)
	s.server = httptest.NewServer(mux)

	return s
}

func (s *sink) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()

	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	var p sinkPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "decode body", http.StatusBadRequest)
		return
	}

	if s.latency > 0 {
		time.Sleep(s.latency)
	}

	// Timestamp before taking the lock so recorded latency reflects delivery
	// time rather than contention on the sink's own bookkeeping.
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.attempts[p.JobID]++

	if s.failFirst > 0 && s.attempts[p.JobID] <= s.failFirst {
		// 503 is classified as retryable by delivery.Client, so this drives the
		// worker pool's backoff-and-retry path.
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	s.receipts = append(s.receipts, receipt{
		JobID:      p.JobID,
		Priority:   p.Priority,
		ReceivedAt: now,
		Seq:        len(s.receipts),
	})

	w.WriteHeader(http.StatusOK)
}

// snapshot returns a copy of the recorded receipts, safe to read without the lock.
func (s *sink) snapshot() []receipt {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]receipt, len(s.receipts))
	copy(out, s.receipts)
	return out
}

// deliveredCount reports how many successful deliveries have been recorded.
func (s *sink) deliveredCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.receipts)
}

func (s *sink) url() string {
	return s.server.URL + "/hook"
}

func (s *sink) close() {
	s.server.Close()
}
