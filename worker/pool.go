package worker

import (
	"context"
	"log"
	"math/rand"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/ronitanilkumar/dispatch/delivery"
	"github.com/ronitanilkumar/dispatch/job"
	"github.com/ronitanilkumar/dispatch/monitor"
	"github.com/ronitanilkumar/dispatch/queue"
	"go.opentelemetry.io/otel/trace"
)

const maxAttempts = 5
const baseDelay = 500 * time.Millisecond
const maxDelay = 30 * time.Second

// Observer receives job lifecycle transitions for observability. It is
// optional: a Pool with a nil observer behaves exactly as before.
type Observer interface {
	AttemptStarted(j *job.Job)
	AttemptFinished()
	JobSucceeded(j *job.Job)
	JobRetrying(j *job.Job, statusCode int, msg string, retryIn time.Duration)
	JobFailed(j *job.Job, kind monitor.FailureKind, attempt int, statusCode int, msg string)
}

type Pool struct {
	qRef           *queue.Queue
	deliveryClient *delivery.Client
	numWorkers     int
	wg             sync.WaitGroup
	obs            Observer
}

func NewPool(q *queue.Queue, d *delivery.Client, numWorkers int) *Pool {
	return &Pool{
		qRef:           q,
		deliveryClient: d,
		numWorkers:     numWorkers,
	}
}

// WithObserver attaches an Observer and returns the pool, for chaining at
// construction time.
func (p *Pool) WithObserver(o Observer) *Pool {
	p.obs = o
	return p
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for {
		j, ok := p.qRef.Dequeue()
		if !ok {
			break
		}
		j.Status = job.InFlight
		p.observeAttemptStarted(j)

		// The submitting goroutine is long gone, so rebuild a context from the
		// span context the job carried through the queue. Each attempt then
		// opens its own span parented to the original submission.
		deliverCtx := trace.ContextWithSpanContext(context.Background(), j.SpanCtx)
		shouldRetry, err := p.deliveryClient.Deliver(deliverCtx, j)
		p.observeAttemptFinished()

		if err != nil {
			if shouldRetry {
				j.Attempts++
				if j.Attempts >= maxAttempts {
					j.Status = job.Failed
					log.Printf("job %d exhausted %d attempts, giving up: %v", j.ID, j.Attempts, err)
					p.observeFailed(j, monitor.FailureExhausted, err)
					continue
				}
				delay := backoff(j.Attempts)
				p.observeRetrying(j, err, delay)
				p.wg.Add(1)
				time.AfterFunc(delay, func() {
					defer p.wg.Done()
					if err := p.qRef.Enqueue(j); err != nil {
						log.Printf("job %d dropped, queue closed during backoff (attempt %d)", j.ID, j.Attempts)
					}
				})
			} else {
				j.Status = job.Failed
				log.Printf("job %d delivery failed (not retryable): %v", j.ID, err)
				p.observeFailed(j, monitor.FailureTerminal, err)
			}
		} else {
			j.Status = job.Succeeded
			p.observeSucceeded(j)
		}
	}
}

func (p *Pool) Start() {
	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

func (p *Pool) Stop() {
	p.qRef.Close()
	p.wg.Wait()
}

// statusCodePattern extracts the HTTP status from the delivery error message.
// delivery.Client formats these as "webhook delivery returned status: %d"; the
// code is not otherwise available on the error, and adding a typed error would
// be a wider change to that package than the dashboard warrants.
var statusCodePattern = regexp.MustCompile(`returned status: (\d{3})`)

func statusCodeFrom(err error) int {
	if err == nil {
		return 0
	}

	match := statusCodePattern.FindStringSubmatch(err.Error())
	if len(match) < 2 {
		return 0
	}

	code, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return 0
	}

	return code
}

func (p *Pool) observeAttemptStarted(j *job.Job) {
	if p.obs != nil {
		p.obs.AttemptStarted(j)
	}
}

func (p *Pool) observeAttemptFinished() {
	if p.obs != nil {
		p.obs.AttemptFinished()
	}
}

func (p *Pool) observeSucceeded(j *job.Job) {
	if p.obs != nil {
		p.obs.JobSucceeded(j)
	}
}

func (p *Pool) observeRetrying(j *job.Job, err error, retryIn time.Duration) {
	if p.obs != nil {
		p.obs.JobRetrying(j, statusCodeFrom(err), err.Error(), retryIn)
	}
}

func (p *Pool) observeFailed(j *job.Job, kind monitor.FailureKind, err error) {
	if p.obs == nil {
		return
	}

	// Attempts is only incremented on the retryable path, so a terminal failure
	// on the first try still reads 0. Report the 1-based attempt number that
	// actually just happened.
	attempt := j.Attempts
	if kind == monitor.FailureTerminal {
		attempt = j.Attempts + 1
	}

	p.obs.JobFailed(j, kind, attempt, statusCodeFrom(err), err.Error())
}

func backoff(attempts int) time.Duration {
	d := min(baseDelay << (attempts - 1), maxDelay)
	jitter := time.Duration(rand.Int63n(int64(d) / 2))
	return d/2 + jitter
}
