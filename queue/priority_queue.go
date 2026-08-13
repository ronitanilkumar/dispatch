package queue

import (
	"container/heap"
	"github.com/ronitanilkumar/dispatch/job"
	"sync"
	"errors"
)

type PriorityQueue []*job.Job

var ErrClosed error = errors.New("queue is closed")
var ErrJobNotFound error = errors.New("job cancellation failed")

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority < pq[j].Priority
	}
	return pq[i].CreatedAt.Before(pq[j].CreatedAt)
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *PriorityQueue) Push(x any) {
	n := len(*pq)
	j := x.(*job.Job)
	j.Index = n
	*pq = append(*pq, j)
}

func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	j := old[n-1]
	old[n-1] = nil
	j.Index = -1
	*pq = old[0 : n-1]
	return j
}

// Mutex, thread-safe wrapper
type Queue struct {
	mu		   sync.Mutex
	cond 	   *sync.Cond
	pq		   PriorityQueue
	closed     bool
	jobsByID   map[int64]*job.Job
}

func NewQueue() *Queue {
	q := &Queue{}
	q.cond = sync.NewCond(&q.mu)
	q.jobsByID = make(map[int64]*job.Job)
	return q
}

func (q *Queue) Dequeue() (*job.Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.pq) == 0 && !q.closed {
		q.cond.Wait()
	}

	if len(q.pq) == 0 {
		return nil, false
	}

	j := heap.Pop(&q.pq).(*job.Job)
	delete(q.jobsByID, j.ID)
	
	return j, true
}

func (q *Queue) Enqueue(j *job.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrClosed
	}

	heap.Push(&q.pq, j)
	q.jobsByID[j.ID] = j
	q.cond.Signal()
	return nil
}

// Depth reports the number of jobs currently waiting to be dequeued.
func (q *Queue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.pq)
}

func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	q.closed = true
	q.cond.Broadcast()
}

func (q *Queue) Cancel(id int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	j, ok := q.jobsByID[id]
	if !ok {
		return ErrJobNotFound
	}

	heap.Remove(&q.pq, j.Index)
	delete(q.jobsByID, id)
	return nil
}