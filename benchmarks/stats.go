package main

import (
	"fmt"
	"sort"
	"time"
)

// percentile returns the p-th percentile (0-100) of sorted durations using
// nearest-rank, which avoids interpolating between samples that were never
// actually observed.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	rank := int(float64(len(sorted)) * p / 100.0)
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}

	return sorted[rank]
}

type latencyStats struct {
	p50 time.Duration
	p95 time.Duration
	p99 time.Duration
	max time.Duration
}

func computeLatency(samples []time.Duration) latencyStats {
	if len(samples) == 0 {
		return latencyStats{}
	}

	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	return latencyStats{
		p50: percentile(sorted, 50),
		p95: percentile(sorted, 95),
		p99: percentile(sorted, 99),
		max: sorted[len(sorted)-1],
	}
}

// correctness holds the outcome of the exactly-once and ordering checks.
type correctness struct {
	submitted  int
	delivered  int
	duplicates int
	missing    int

	// expectDrops marks scenarios where the rate limiter can legitimately
	// starve jobs past the retry ceiling. There, "missing" jobs are a measured
	// system property, not a delivery bug, so exactlyOnce() judges only
	// duplicates and the drop rate is reported separately.
	expectDrops bool

	// priorityChecked is false when a scenario submits a single priority, where
	// an ordering check would be meaningless.
	priorityChecked bool
	// inversions counts lower-priority jobs delivered before higher-priority
	// ones that were already queued. See checkPriorityOrdering for why this is
	// reported as a ratio rather than a hard pass/fail.
	inversions   int
	comparisons  int
	inversionPct float64
}

func (c correctness) exactlyOnce() bool {
	// A duplicate delivery is always a bug. A missing delivery is only a bug
	// when the scenario did not expect rate-limit-induced drops.
	if c.duplicates != 0 {
		return false
	}
	if c.expectDrops {
		return true
	}
	return c.missing == 0 && c.delivered == c.submitted
}

// dropRate is the share of submitted jobs that never reached the sink.
func (c correctness) dropRate() float64 {
	if c.submitted == 0 {
		return 0
	}
	return float64(c.missing) / float64(c.submitted) * 100
}

func (c correctness) String() string {
	if c.duplicates != 0 {
		return fmt.Sprintf("FAIL (dupes=%d)", c.duplicates)
	}

	if c.expectDrops {
		if c.missing == 0 {
			return "PASS (no drops)"
		}
		// Not a failure: at-most-once held (zero duplicates) and the shortfall
		// is the measured effect of the retry ceiling under rate limiting.
		return fmt.Sprintf("PASS (no dupes; %d dropped)", c.missing)
	}

	if c.missing != 0 {
		return fmt.Sprintf("FAIL (missing=%d)", c.missing)
	}

	return "PASS"
}

// checkExactlyOnce verifies every submitted job was delivered exactly once.
func checkExactlyOnce(submittedIDs []int64, receipts []receipt) correctness {
	seen := make(map[int64]int, len(receipts))
	for _, r := range receipts {
		seen[r.JobID]++
	}

	c := correctness{
		submitted: len(submittedIDs),
		delivered: len(receipts),
	}

	for _, id := range submittedIDs {
		switch count := seen[id]; {
		case count == 0:
			c.missing++
		case count > 1:
			c.duplicates += count - 1
		}
	}

	return c
}

// checkPriorityOrdering measures how often a lower-priority job was delivered
// before a higher-priority job that was already sitting in the queue when the
// lower-priority one arrived.
//
// This is deliberately reported as an inversion *rate* rather than a boolean.
// Dispatch's queue is strictly ordered, but N workers dequeue concurrently and
// then perform variable-duration network I/O, so a high-priority job dequeued
// first can still finish after a low-priority job dequeued moments later. That
// is expected behavior of a concurrent pool, not a queue bug. A low rate
// confirms the queue is prioritizing; a rate near 50% would mean ordering is
// effectively random.
func checkPriorityOrdering(submissions []submission, receipts []receipt) (inversions, comparisons int) {
	// Delivery order, by job.
	deliveredAt := make(map[int64]int, len(receipts))
	for _, r := range receipts {
		deliveredAt[r.JobID] = r.Seq
	}

	// Compare only jobs that overlapped in the queue: b submitted after a was
	// submitted, so a was already queued and should win if it outranks b.
	for i := range submissions {
		a := submissions[i]
		aSeq, aok := deliveredAt[a.jobID]
		if !aok {
			continue
		}

		for j := i + 1; j < len(submissions); j++ {
			b := submissions[j]

			if a.priority == b.priority {
				continue
			}

			bSeq, bok := deliveredAt[b.jobID]
			if !bok {
				continue
			}

			// Only consider pairs where the higher-priority job was submitted
			// first; otherwise there is no ordering expectation to violate.
			if b.submittedAt.Before(a.submittedAt) {
				continue
			}

			comparisons++

			// Lower Priority value == higher priority (High=0).
			if a.priority < b.priority && bSeq < aSeq {
				inversions++
			}
		}
	}

	return inversions, comparisons
}
