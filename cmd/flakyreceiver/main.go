// Command flakyreceiver is a configurable webhook target for demos and tests.
//
// Routes:
//
//	POST /hook          fails the first -fail attempts overall, then succeeds
//	POST /status/{code} always responds with {code}
//	POST /flaky/{n}     fails the first {n} attempts per job, then succeeds
//
// It exists so the dashboard and trace demos can trigger each of Dispatch's
// retry classifications on demand: 5xx and 429 are retryable, 4xx is terminal.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
)

const addr = ":9090"

// perJobAttempts tracks attempts per job ID for the /flaky/{n} route, so "fail
// twice then succeed" applies to each job independently rather than globally.
var (
	mu             sync.Mutex
	perJobAttempts = map[string]int{}
)

// jobIdentity pulls a stable per-job key out of the payload when present.
// Dispatch reuses the same payload across retries of a job, so any caller-set
// identifier is stable for that job's whole retry sequence.
type jobIdentity struct {
	JobID    any `json:"jobId"`
	DemoJobs any `json:"demoJobId"`
}

func identityOf(body []byte) string {
	var id jobIdentity
	if err := json.Unmarshal(body, &id); err == nil {
		if id.JobID != nil {
			return "job:" + toString(id.JobID)
		}
		if id.DemoJobs != nil {
			return "demo:" + toString(id.DemoJobs)
		}
	}
	// No identifier: fall back to counting globally. Useful for a single job,
	// but concurrent unidentified jobs will share one counter.
	return "global"
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return "unknown"
	}
}

func main() {
	failFirst := flag.Int("fail", 1, "attempts to fail on /hook before succeeding")
	flag.Parse()

	var hits atomic.Int64

	http.HandleFunc("POST /hook", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		defer r.Body.Close()

		n := hits.Add(1)

		if int(n) <= *failFirst {
			log.Printf("/hook hit %d: 503 (forcing retry)", n)
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}

		log.Printf("/hook hit %d: 200", n)
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("POST /status/{code}", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		defer r.Body.Close()

		code, err := strconv.Atoi(r.PathValue("code"))
		if err != nil || code < 100 || code > 599 {
			http.Error(w, "invalid status code", http.StatusBadRequest)
			return
		}

		log.Printf("/status/%d responding %d", code, code)
		w.WriteHeader(code)
	})

	http.HandleFunc("POST /flaky/{n}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()

		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil || n < 0 {
			http.Error(w, "invalid failure count", http.StatusBadRequest)
			return
		}

		key := identityOf(body)

		mu.Lock()
		perJobAttempts[key]++
		attempt := perJobAttempts[key]
		mu.Unlock()

		if attempt <= n {
			log.Printf("/flaky/%d %s attempt %d: 500 (forcing retry)", n, key, attempt)
			http.Error(w, "simulated failure", http.StatusInternalServerError)
			return
		}

		log.Printf("/flaky/%d %s attempt %d: 200", n, key, attempt)
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("flaky receiver listening on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("flaky receiver: %v", err)
	}
}
