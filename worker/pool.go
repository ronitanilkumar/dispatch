package worker

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/ronitanilkumar/dispatch/delivery"
	"github.com/ronitanilkumar/dispatch/queue"
	"github.com/ronitanilkumar/dispatch/job"
)

const maxAttempts = 5
const baseDelay = 500 * time.Millisecond
const maxDelay = 30 * time.Second

type Pool struct {
	qRef           *queue.Queue
	deliveryClient *delivery.Client
	numWorkers     int
	wg             sync.WaitGroup
}

func NewPool(q *queue.Queue, d *delivery.Client, numWorkers int) *Pool {
	return &Pool{
		qRef:           q,
		deliveryClient: d,
		numWorkers:     numWorkers,
	}
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for {
		j, ok := p.qRef.Dequeue()
		if !ok {
			break
		}
		j.Status = job.InFlight

		deliverCtx := context.Background()
		shouldRetry, err := p.deliveryClient.Deliver(deliverCtx, j)
		if err != nil {
			if shouldRetry {
				j.Attempts++
				if j.Attempts >= maxAttempts {
					j.Status = job.Failed
					log.Printf("job %d exhausted %d attempts, giving up: %v", j.ID, j.Attempts, err)
					continue
				}
				p.wg.Add(1)
				time.AfterFunc(backoff(j.Attempts), func() {
					defer p.wg.Done()
					if err := p.qRef.Enqueue(j); err != nil {
						log.Printf("job %d dropped, queue closed during backoff (attempt %d)", j.ID, j.Attempts)
					}
				})
			} else {
				j.Status = job.Failed
				log.Printf("job %d delivery failed (not retryable): %v", j.ID, err)
			}
		} else {
			j.Status = job.Succeeded
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

func backoff(attempts int) time.Duration {
	d := min(baseDelay << (attempts - 1), maxDelay)
	jitter := time.Duration(rand.Int63n(int64(d) / 2))
	return d/2 + jitter
}
