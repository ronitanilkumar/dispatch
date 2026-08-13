package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/ronitanilkumar/dispatch/monitor"
)

// DashboardHandler serves read-only observability endpoints for the dashboard.
// It is separate from Handler so the write path (job submission) has no
// dependency on monitoring state.
type DashboardHandler struct {
	mon   *monitor.Monitor
	depth func() int
}

func NewDashboardHandler(mon *monitor.Monitor, depth func() int) *DashboardHandler {
	return &DashboardHandler{
		mon:   mon,
		depth: depth,
	}
}

type QueueDepthResponse struct {
	Depth   int                   `json:"depth"`
	History []monitor.QueueSample `json:"history"`
}

type WorkersResponse struct {
	Total int `json:"total"`
	Busy  int `json:"busy"`
	Idle  int `json:"idle"`
}

// StatsResponse is the single aggregate payload the dashboard polls. Serving
// everything in one response keeps the client's view internally consistent:
// separate endpoints could interleave with worker updates and show a queue
// depth that disagrees with the job list.
type StatsResponse struct {
	Queue   QueueDepthResponse  `json:"queue"`
	Workers WorkersResponse     `json:"workers"`
	Jobs    []monitor.JobState  `json:"jobs"`
	Events  []monitor.Event     `json:"events"`
	Totals  map[string]int      `json:"totals"`
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("ERROR: failed to encode dashboard response: %v", err)
	}
}

func (h *DashboardHandler) QueueDepthHandler(w http.ResponseWriter, r *http.Request) {
	_, _, samples, _, _ := h.mon.Snapshot()

	writeJSON(w, QueueDepthResponse{
		Depth:   h.depth(),
		History: samples,
	})
}

func (h *DashboardHandler) JobsHandler(w http.ResponseWriter, r *http.Request) {
	jobs, _, _, _, _ := h.mon.Snapshot()
	writeJSON(w, jobs)
}

func (h *DashboardHandler) WorkersHandler(w http.ResponseWriter, r *http.Request) {
	_, _, _, busy, total := h.mon.Snapshot()

	writeJSON(w, WorkersResponse{
		Total: total,
		Busy:  busy,
		Idle:  total - busy,
	})
}

func (h *DashboardHandler) StatsHandler(w http.ResponseWriter, r *http.Request) {
	jobs, events, samples, busy, total := h.mon.Snapshot()

	totals := map[string]int{
		"pending":   0,
		"in-flight": 0,
		"retrying":  0,
		"succeeded": 0,
		"failed":    0,
	}

	for _, j := range jobs {
		totals[j.Status]++
	}

	writeJSON(w, StatsResponse{
		Queue: QueueDepthResponse{
			Depth:   h.depth(),
			History: samples,
		},
		Workers: WorkersResponse{
			Total: total,
			Busy:  busy,
			Idle:  total - busy,
		},
		Jobs:   jobs,
		Events: events,
		Totals: totals,
	})
}
