package worker

import (
	"github.com/ronitanilkumar/dispatch/queue"
	"sync"
	"context"
)

type Pool struct {
	qRef	   *queue.Queue
	numWorkers int
	wg		   sync.WaitGroup
	done       chan struct{}
	stopOnce   sync.Once
}

func NewPool(q *queue.Queue, numWorkers int) *Pool {
	return &Pool{
		qRef:		q,
		numWorkers: numWorkers,
		done:       make(chan struct{}),
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