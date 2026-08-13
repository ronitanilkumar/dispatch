package main

import (
	"testing"
	"time"
)

// Deliberately inverted: high-priority job submitted first, delivered last.
func TestPriorityOrderingDetectsInversion(t *testing.T) {
	base := time.Now()
	subs := []submission{
		{jobID: 1, priority: 0, submittedAt: base},                       // High, first
		{jobID: 2, priority: 2, submittedAt: base.Add(1 * time.Millisecond)}, // Low, second
	}
	// Low delivered first (Seq 0), High second (Seq 1) => 1 inversion.
	recs := []receipt{
		{JobID: 2, Seq: 0},
		{JobID: 1, Seq: 1},
	}
	inv, cmp := checkPriorityOrdering(subs, recs)
	if cmp != 1 {
		t.Fatalf("expected 1 comparison, got %d", cmp)
	}
	if inv != 1 {
		t.Fatalf("expected 1 inversion, got %d", inv)
	}
}

// Correct ordering: high-priority submitted first and delivered first.
func TestPriorityOrderingAcceptsCorrectOrder(t *testing.T) {
	base := time.Now()
	subs := []submission{
		{jobID: 1, priority: 0, submittedAt: base},
		{jobID: 2, priority: 2, submittedAt: base.Add(1 * time.Millisecond)},
	}
	recs := []receipt{
		{JobID: 1, Seq: 0},
		{JobID: 2, Seq: 1},
	}
	inv, cmp := checkPriorityOrdering(subs, recs)
	if cmp != 1 || inv != 0 {
		t.Fatalf("expected 1 comparison / 0 inversions, got %d/%d", cmp, inv)
	}
}

func TestExactlyOnceDetectsDupesAndMissing(t *testing.T) {
	ids := []int64{1, 2, 3}
	recs := []receipt{{JobID: 1}, {JobID: 1}} // 1 twice, 2 and 3 missing
	c := checkExactlyOnce(ids, recs)
	if c.duplicates != 1 {
		t.Fatalf("expected 1 duplicate, got %d", c.duplicates)
	}
	if c.missing != 2 {
		t.Fatalf("expected 2 missing, got %d", c.missing)
	}
	if c.exactlyOnce() {
		t.Fatal("expected exactlyOnce() to be false")
	}
}
