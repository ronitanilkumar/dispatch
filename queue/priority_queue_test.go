package queue

import (
	"encoding/json"
	"testing"

	"github.com/ronitanilkumar/dispatch/job"
)

// drainIDs dequeues every remaining job and returns their IDs in dequeue order.
func drainIDs(t *testing.T, q *Queue) []int64 {
	t.Helper()

	q.Close() // so the final Dequeue returns false instead of blocking

	var got []int64
	for {
		j, ok := q.Dequeue()
		if !ok {
			return got
		}
		got = append(got, j.ID)
	}
}

// checkIndexes verifies every job's cached Index matches its real slot, which is
// the invariant Cancel depends on to locate a job in the heap.
func checkIndexes(t *testing.T, q *Queue) {
	t.Helper()

	for slot, j := range q.pq {
		if j.Index != slot {
			t.Errorf("job %d sits at slot %d but has Index %d", j.ID, slot, j.Index)
		}
	}
}

func enqueueN(t *testing.T, q *Queue, priorities ...job.Priority) []*job.Job {
	t.Helper()

	jobs := make([]*job.Job, 0, len(priorities))
	for _, p := range priorities {
		j := job.NewJob(json.RawMessage(`{}`), p, "https://example.com")
		if err := q.Enqueue(j); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		jobs = append(jobs, j)
	}
	return jobs
}

// Canceling a job in the middle of the heap must not disturb the ordering of the
// rest. This is the case where Remove swaps the tail into the hole and sifts,
// exercising the Index bookkeeping in Swap.
func TestCancelMiddleKeepsPriorityOrder(t *testing.T) {
	q := NewQueue()
	jobs := enqueueN(t, q, job.Low, job.High, job.Normal, job.High, job.Low)

	// Cancel one of the two High jobs; it is not at the tail of the heap.
	target := jobs[1]
	if target.Index == len(q.pq)-1 {
		t.Fatalf("test precondition: target is at the tail, wanted a middle element")
	}
	if err := q.Cancel(target.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	checkIndexes(t, q)

	want := []int64{jobs[3].ID, jobs[2].ID, jobs[0].ID, jobs[4].ID}
	got := drainIDs(t, q)

	if len(got) != len(want) {
		t.Fatalf("dequeued %d jobs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got job %d, want %d", i, got[i], want[i])
		}
	}
}

// The i == n path in heap.Remove skips the swap and sift entirely, so it needs
// its own coverage.
func TestCancelLastIndexKeepsPriorityOrder(t *testing.T) {
	q := NewQueue()
	jobs := enqueueN(t, q, job.High, job.Normal, job.Low, job.Low)

	target := q.pq[len(q.pq)-1]
	if err := q.Cancel(target.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	checkIndexes(t, q)

	if _, still := q.jobsByID[target.ID]; still {
		t.Errorf("canceled job %d still present in jobsByID", target.ID)
	}

	for _, id := range drainIDs(t, q) {
		if id == target.ID {
			t.Fatalf("canceled job %d was dequeued", target.ID)
		}
	}

	_ = jobs
}

// Canceling every job one at a time keeps the heap consistent throughout, and
// leaves both the heap and the ID index empty.
func TestCancelAllLeavesQueueEmpty(t *testing.T) {
	q := NewQueue()
	jobs := enqueueN(t, q, job.Normal, job.High, job.Low, job.High, job.Normal, job.Low)

	for _, j := range jobs {
		if err := q.Cancel(j.ID); err != nil {
			t.Fatalf("Cancel(%d): %v", j.ID, err)
		}
		checkIndexes(t, q)
	}

	if len(q.pq) != 0 {
		t.Errorf("heap has %d jobs left, want 0", len(q.pq))
	}
	if len(q.jobsByID) != 0 {
		t.Errorf("jobsByID has %d entries left, want 0", len(q.jobsByID))
	}
}

func TestCancelUnknownIDReturnsErrJobNotFound(t *testing.T) {
	q := NewQueue()
	enqueueN(t, q, job.High)

	if err := q.Cancel(999999); err != ErrJobNotFound {
		t.Errorf("Cancel(unknown) = %v, want ErrJobNotFound", err)
	}
}

// A dequeued job must no longer be cancelable, since Dequeue drops it from the
// ID index.
func TestCancelAfterDequeueReturnsErrJobNotFound(t *testing.T) {
	q := NewQueue()
	enqueueN(t, q, job.High, job.Low)

	j, ok := q.Dequeue()
	if !ok {
		t.Fatal("Dequeue returned no job")
	}

	if err := q.Cancel(j.ID); err != ErrJobNotFound {
		t.Errorf("Cancel(dequeued) = %v, want ErrJobNotFound", err)
	}
}
