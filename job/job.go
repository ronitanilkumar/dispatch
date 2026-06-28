package job

import (
	"time"
	"encoding/json"
)

type Priority int
type Status int

const (
	High Priority = iota
	Normal
	Low
)
const (
	Pending Status = iota
	InFlight
	Succeeded
	Failed
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