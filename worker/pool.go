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
	done           chan struct{}
	stopOnce       sync.Once
}

func NewPool(q *queue.Queue, d *delivery.Client, numWorkers int) *Pool {
	return &Pool{
		qRef:           q,
		deliveryClient: d,
		numWorkers:     numWorkers,
		done:           make(chan struct{}),
	}
}

func (p *Pool) watchContext(ctx context.Context) {
	<-ctx.Done()
	p.Stop()
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
				log.Printf("job %d delivery failed (retryable): %v", job.Id, err)
			} else {
				log.Printf("job %d delivery failed (not retryable): %v", job.Id, err)
			}
		}
	}
}

func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	go p.watchContext(ctx)
}

func (p *Pool) Stop() {
	p.qRef.Close()
	p.wg.Wait()
	p.stopOnce.Do(func() {
		close(p.done)
	})
}
