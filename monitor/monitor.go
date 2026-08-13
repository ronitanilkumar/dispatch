// Package monitor collects read-only observability state for the dashboard.
//
// It exists as a separate component rather than reading Dispatch's internals
// directly for two reasons. First, Queue.Dequeue removes a job from jobsByID,
// so the queue has no record of a job once a worker takes it and cannot answer
// "what happened to job N". Second, job.Status is written by worker goroutines
// without synchronization, so reading it from an HTTP handler would be a data
// race. Workers instead report transitions here, where they are recorded under
// the monitor's own lock.
package monitor

import (
	"sync"
	"time"

	"github.com/ronitanilkumar/dispatch/job"
)

// FailureKind distinguishes the delivery classifications made by
// delivery.Client, so the dashboard can show retry logic rather than hiding it.
type FailureKind string

const (
	// FailureRetryable is a 429/5xx or transport error: the worker will back
	// off and try again.
	FailureRetryable FailureKind = "retryable"
	// FailureTerminal is a 3xx/4xx: the worker gives up immediately.
	FailureTerminal FailureKind = "terminal"
	// FailureExhausted means retries were available but maxAttempts was hit.
	FailureExhausted FailureKind = "exhausted"
)

// JobState is a point-in-time snapshot of one job's progress.
type JobState struct {
	ID          int64      `json:"id"`
	Priority    int        `json:"priority"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	URL         string     `json:"url"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastError   string     `json:"lastError,omitempty"`
	LastStatus  int        `json:"lastStatusCode,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// Event is one entry in the retry/failure feed.
type Event struct {
	Seq        int64       `json:"seq"`
	JobID      int64       `json:"jobId"`
	At         time.Time   `json:"at"`
	Kind       FailureKind `json:"kind"`
	Attempt    int         `json:"attempt"`
	StatusCode int         `json:"statusCode,omitempty"`
	Message    string      `json:"message"`
	// RetryInMs is the backoff delay before the next attempt, when retryable.
	RetryInMs int64 `json:"retryInMs,omitempty"`
}

// QueueSample is one queue-depth reading for the time-series chart.
type QueueSample struct {
	At    time.Time `json:"at"`
	Depth int       `json:"depth"`
	Busy  int       `json:"busy"`
}

const (
	maxJobs    = 200
	maxEvents  = 100
	maxSamples = 120
)

// Monitor records job lifecycle transitions reported by the worker pool.
// All exported methods are safe for concurrent use.
type Monitor struct {
	mu sync.RWMutex

	jobs     map[int64]*JobState
	jobOrder []int64

	events  []Event
	nextSeq int64

	samples []QueueSample

	numWorkers int
	busy       int

	// High-water marks since the last Sample call. See Sample for why the chart
	// plots peaks rather than instantaneous values.
	peakDepth int
	peakBusy  int
}

func New(numWorkers int) *Monitor {
	return &Monitor{
		jobs:       make(map[int64]*JobState),
		numWorkers: numWorkers,
	}
}

// track records a newly submitted job, evicting the oldest once maxJobs is
// reached so a long-running demo cannot grow memory without bound.
func (m *Monitor) track(j *job.Job, status string) *JobState {
	st, ok := m.jobs[j.ID]
	if ok {
		return st
	}

	st = &JobState{
		ID:        j.ID,
		Priority:  int(j.Priority),
		Status:    status,
		Attempts:  j.Attempts,
		URL:       j.URL,
		CreatedAt: j.CreatedAt,
		UpdatedAt: time.Now(),
	}

	m.jobs[j.ID] = st
	m.jobOrder = append(m.jobOrder, j.ID)

	if len(m.jobOrder) > maxJobs {
		oldest := m.jobOrder[0]
		m.jobOrder = m.jobOrder[1:]
		delete(m.jobs, oldest)
	}

	return st
}

// JobSubmitted records a job accepted by the API and queued.
func (m *Monitor) JobSubmitted(j *job.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.track(j, "pending")
	st.Status = "pending"
	st.UpdatedAt = time.Now()
}

// AttemptStarted records a worker picking up a job. It also increments the busy
// worker count.
func (m *Monitor) AttemptStarted(j *job.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.track(j, "in-flight")
	st.Status = "in-flight"
	// Attempts counts completed retries, so the attempt now starting is +1.
	st.Attempts = j.Attempts + 1
	st.UpdatedAt = time.Now()

	if m.busy < m.numWorkers {
		m.busy++
	}

	if m.busy > m.peakBusy {
		m.peakBusy = m.busy
	}
}

// AttemptFinished releases the worker slot taken by AttemptStarted.
func (m *Monitor) AttemptFinished() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.busy > 0 {
		m.busy--
	}
}

// JobSucceeded records a terminal success.
func (m *Monitor) JobSucceeded(j *job.Job) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	st := m.track(j, "succeeded")
	st.Status = "succeeded"
	st.Attempts = j.Attempts
	st.UpdatedAt = now
	st.CompletedAt = &now
	st.LastError = ""
}

// JobRetrying records a retryable failure and the backoff before the next try.
func (m *Monitor) JobRetrying(j *job.Job, statusCode int, msg string, retryIn time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.track(j, "pending")
	st.Status = "retrying"
	st.Attempts = j.Attempts
	st.UpdatedAt = time.Now()
	st.LastError = msg
	st.LastStatus = statusCode

	m.appendEvent(Event{
		JobID:      j.ID,
		Kind:       FailureRetryable,
		Attempt:    j.Attempts,
		StatusCode: statusCode,
		Message:    msg,
		RetryInMs:  retryIn.Milliseconds(),
	})
}

// JobFailed records a terminal failure, either non-retryable or exhausted.
// attempt is the 1-based attempt number that produced the failure.
func (m *Monitor) JobFailed(j *job.Job, kind FailureKind, attempt int, statusCode int, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	st := m.track(j, "failed")
	st.Status = "failed"
	st.Attempts = attempt
	st.UpdatedAt = now
	st.CompletedAt = &now
	st.LastError = msg
	st.LastStatus = statusCode

	m.appendEvent(Event{
		JobID:      j.ID,
		Kind:       kind,
		Attempt:    attempt,
		StatusCode: statusCode,
		Message:    msg,
	})
}

// appendEvent must be called with m.mu held.
func (m *Monitor) appendEvent(e Event) {
	m.nextSeq++
	e.Seq = m.nextSeq
	e.At = time.Now()

	m.events = append(m.events, e)
	if len(m.events) > maxEvents {
		m.events = m.events[len(m.events)-maxEvents:]
	}
}

// Sample records a queue-depth reading for the chart.
//
// Depth and Busy are reported as the peak seen since the previous sample, not
// the instantaneous value. Loopback deliveries finish in microseconds, so
// point-in-time polling almost always observes an empty queue and idle workers
// even while thousands of jobs are flowing; a high-water mark makes real bursts
// visible on the chart.
func (m *Monitor) Sample(depth int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if depth > m.peakDepth {
		m.peakDepth = depth
	}

	m.samples = append(m.samples, QueueSample{
		At:    time.Now(),
		Depth: m.peakDepth,
		Busy:  m.peakBusy,
	})

	// Reset the marks so the next sample reflects the next window only.
	m.peakDepth = depth
	m.peakBusy = m.busy

	if len(m.samples) > maxSamples {
		m.samples = m.samples[len(m.samples)-maxSamples:]
	}
}

// StartSampler polls queue depth on an interval until stop is closed. depthFn
// is supplied by the caller so monitor does not depend on the queue package.
func (m *Monitor) StartSampler(interval time.Duration, depthFn func() int, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			m.Sample(depthFn())
		}
	}
}

// Snapshot returns copies of the recorded state, newest jobs and events first.
func (m *Monitor) Snapshot() ([]JobState, []Event, []QueueSample, int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	jobs := make([]JobState, 0, len(m.jobOrder))
	for i := len(m.jobOrder) - 1; i >= 0; i-- {
		if st, ok := m.jobs[m.jobOrder[i]]; ok {
			jobs = append(jobs, *st)
		}
	}

	events := make([]Event, 0, len(m.events))
	for i := len(m.events) - 1; i >= 0; i-- {
		events = append(events, m.events[i])
	}

	samples := make([]QueueSample, len(m.samples))
	copy(samples, m.samples)

	return jobs, events, samples, m.busy, m.numWorkers
}
