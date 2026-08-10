package job

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

type Priority int
type Status int

const (
	High Priority = iota
	Normal
	Low
	PriorityLimit
)
const (
	Pending Status = iota
	InFlight
	Succeeded
	Failed
)

type Job struct {
	ID        int64
	Priority  Priority
	CreatedAt time.Time
	URL       string
	Payload   json.RawMessage
	Status    Status
	Index     int
	Attempts  int
}

var nextID atomic.Int64

func NewJob(payload json.RawMessage, priority Priority, url string) *Job {
	return &Job{
		Payload:   payload,
		Priority:  priority,
		URL:       url,
		CreatedAt: time.Now(),
		Status:    Pending,
		Index:     -1,
		ID:        nextID.Add(1),
	}
}
