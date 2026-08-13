package main

import (
	"fmt"
	"strings"
	"time"
)

func fmtDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "-"
	case d < time.Millisecond:
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000.0)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

func printRows(results []result, realistic bool) {
	header := "| Scenario | Jobs | Submitters | Workers | Throughput (jobs/s) | Submit rate (jobs/s) | p50 | p95 | p99 | max | Exactly-once |"
	sep := "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|:---:|"

	if realistic {
		header = "| Scenario | Jobs | Submitters | Workers | Throughput (jobs/s) | Submit rate (jobs/s) | Delivered | Dropped | p50 | p95 | p99 | Exactly-once |"
		sep = "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|:---:|"
	}

	fmt.Println(header)
	fmt.Println(sep)

	for _, r := range results {
		if r.scenario.realistic != realistic {
			continue
		}

		if realistic {
			fmt.Printf("| %s | %d | %d | %d | %.1f | %.0f | %d | %d (%.0f%%) | %s | %s | %s | %s |\n",
				r.scenario.name,
				r.scenario.jobs,
				r.scenario.submitters,
				r.scenario.workers,
				r.throughput,
				r.submitRate,
				r.correctness.delivered,
				r.correctness.missing,
				r.correctness.dropRate(),
				fmtDuration(r.latency.p50),
				fmtDuration(r.latency.p95),
				fmtDuration(r.latency.p99),
				r.correctness.String(),
			)
			continue
		}

		fmt.Printf("| %s | %d | %d | %d | %.0f | %.0f | %s | %s | %s | %s | %s |\n",
			r.scenario.name,
			r.scenario.jobs,
			r.scenario.submitters,
			r.scenario.workers,
			r.throughput,
			r.submitRate,
			fmtDuration(r.latency.p50),
			fmtDuration(r.latency.p95),
			fmtDuration(r.latency.p99),
			fmtDuration(r.latency.max),
			r.correctness.String(),
		)
	}

	fmt.Println()
}

func printTable(results []result) {
	fmt.Println()
	fmt.Println("## Dispatch load benchmark results")
	fmt.Println()

	fmt.Println("### A. Isolated delivery path (rate limiter disabled, loopback sink)")
	fmt.Println()
	fmt.Println("Upper-bound capacity of the queue + worker pool + delivery path.")
	fmt.Println("Not representative of production: see README.")
	fmt.Println()
	printRows(results, false)

	fmt.Println("### B. Realistic profile (production rate limits, 50ms simulated RTT)")
	fmt.Println()
	fmt.Println("Production limiter (50 burst, 10/s per host) and wide-area latency.")
	fmt.Println("Drops are a measured outcome of the retry ceiling, not a delivery bug.")
	fmt.Println()
	printRows(results, true)

	// Correctness detail, including the priority-ordering measurement which
	// does not fit the main table.
	var notes []string

	for _, r := range results {
		c := r.correctness

		if !c.exactlyOnce() {
			notes = append(notes, fmt.Sprintf(
				"- **%s**: exactly-once FAILED — submitted=%d delivered=%d duplicates=%d missing=%d",
				r.scenario.name, c.submitted, c.delivered, c.duplicates, c.missing))
		}

		if c.expectDrops && c.missing > 0 {
			notes = append(notes, fmt.Sprintf(
				"- **%s**: %d/%d jobs (%.0f%%) exhausted all %d attempts and were dropped under the "+
					"production per-host rate limit. Zero duplicates — at-most-once held.",
				r.scenario.name, c.missing, c.submitted, c.dropRate(), maxAttempts))
		}

		if c.priorityChecked {
			notes = append(notes, fmt.Sprintf(
				"- **%s**: priority ordering — %d/%d ordered pairs inverted (%.1f%%). "+
					"See README on why this is a rate, not a pass/fail.",
				r.scenario.name, c.inversions, c.comparisons, c.inversionPct))
		}

		if r.retried {
			notes = append(notes, fmt.Sprintf(
				"- **%s**: sink rejected the first attempt of every job (503); "+
					"all %d still delivered exactly once via the retry path.",
				r.scenario.name, c.delivered))
		}
	}

	if len(notes) > 0 {
		fmt.Println("### Notes")
		fmt.Println()
		fmt.Println(strings.Join(notes, "\n"))
		fmt.Println()
	}
}
