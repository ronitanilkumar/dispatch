package queue

import (
	"container/heap"
	"github.com/ronitanilkumar/dispatch/job"
	"sync"
)

type PriorityQueue []*job.Job

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
	mu		sync.Mutex
	cond 	*sync.Cond
	pq		PriorityQueue
}

func NewQueue() *Queue {
	q := &Queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *Queue) Dequeue() *job.Job {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.pq) == 0 {
		q.cond.Wait()
	}

	j := heap.Pop(&q.pq).(*job.Job)
	
	return j
}

func (q *Queue) Enqueue(j *job.Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	heap.Push(&q.pq, j)
	q.cond.Signal()
}