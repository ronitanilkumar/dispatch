package job

import (
	"time"
	"encoding/json"
)

type Priority int
type Status int

const (
	high Priority = iota
	normal
	low
)
const (
	pending Status = iota
	inFlight
	succeeded
	failed
)

type Job struct {
	Id		 	int64
	Priority 	Priority
	CreatedAt 	time.Time
	URL 		string
	Payload		json.RawMessage
	Status 		Status
	Index 		int
}