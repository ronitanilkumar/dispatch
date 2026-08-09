package worker

import (
	"context"
	"github.com/ronitanilkumar/dispatch/delivery"
	"github.com/ronitanilkumar/dispatch/queue"
	"sync"
	"log"
)

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
		job, ok := p.qRef.Dequeue()
		if !ok {
			break
		}

		deliverCtx := context.Background()
		shouldRetry, err := p.deliveryClient.Deliver(deliverCtx, job)
		if err != nil {
			if shouldRetry {
				log.Printf("job %d delivery failed (retryable): %v", job.ID, err)
			} else {
				log.Printf("job %d delivery failed (not retryable): %v", job.ID, err)
			}
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
