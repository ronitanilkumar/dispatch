package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/ronitanilkumar/dispatch/job"
	"github.com/ronitanilkumar/dispatch/queue"
)

const maxByteSize int64 = 1024 * 1024

type Handler struct {
	qRef *queue.Queue
}

func NewHandler(q *queue.Queue) *Handler {
	return &Handler{
		qRef: q,
	}
}

type SubmitJobRequest struct {
	Payload  json.RawMessage `json:"payload"`
	Priority *job.Priority   `json:"priority"`
	URL      string          `json:"url"`
}

type SubmitJobResponse struct {
	ID int64 `json:"id"`
}

func (h *Handler) SubmitJobHandler(w http.ResponseWriter, r *http.Request) {
	var req SubmitJobRequest

	r.Body = http.MaxBytesReader(w, r.Body, maxByteSize)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		var maxBytesError *http.MaxBytesError
		var syntaxError *json.SyntaxError
		var typeError *json.UnmarshalTypeError

		switch {
		case errors.Is(err, io.EOF):
			http.Error(w, "request body cannot be empty", http.StatusBadRequest)
			return

		case errors.As(err, &maxBytesError):
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return

		case errors.As(err, &syntaxError):
			msg := fmt.Sprintf("request body contains malformed JSON near byte %d", int(syntaxError.Offset))
			http.Error(w, msg, http.StatusBadRequest)
			return

		case errors.As(err, &typeError):
			var msg string
			switch typeError.Field {
			case "priority":
				msg = "priority must be an integer"
			case "url":
				msg = "url must be a string"
			case "payload":
				msg = "payload must be valid JSON"
			default:
				msg = "request body contains a field with an invalid type"
			}
			http.Error(w, msg, http.StatusBadRequest)
			return

		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), "\"")
			msg := fmt.Sprintf("field %s is not recognized by this endpoint", fieldName)
			http.Error(w, msg, http.StatusBadRequest)
			return

		default:
			log.Printf("unexpected error decoding submit job request: %v", err)
			http.Error(w, "request body is invalid", http.StatusBadRequest)
			return
		}
	}

	var extra any

	err2 := decoder.Decode(&extra)

	var maxBytesError2 *http.MaxBytesError

	if errors.Is(err2, io.EOF) {
		// Nothing to be done, continue natural execution of program
	} else if errors.As(err2, &maxBytesError2) {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	} else if err2 != nil {
		http.Error(w, "request body is invalid", http.StatusBadRequest)
		return
	} else {
		http.Error(w, "request body must contain exactly one JSON object", http.StatusBadRequest)
		return
	}

	if validationErr := validateSubmitJobRequest(req); validationErr != nil {
		http.Error(w, validationErr.Error(), http.StatusBadRequest)
		return
	}

	priority := job.Low
	if req.Priority != nil {
		priority = *req.Priority
	}

	newJob := job.NewJob(
		req.Payload,
		priority,
		strings.TrimSpace(req.URL),
	)

	queueErr := h.qRef.Enqueue(newJob)

	if queueErr != nil {
		if errors.Is(queueErr, queue.ErrClosed) {
			http.Error(w, "service unavailable: queue is closed", http.StatusServiceUnavailable)
			return
		}
		
		log.Printf("ERROR: queue push failed: %v", queueErr)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	response := SubmitJobResponse{
		ID: newJob.ID,
	}

	if encodeErr := json.NewEncoder(w).Encode(response); encodeErr != nil {
		log.Printf("ERROR: failed to encode success response: %v", encodeErr)
	}
}

func validateSubmitJobRequest(req SubmitJobRequest) error {
	// URL Validation
	requestURL := strings.TrimSpace(req.URL)

	if requestURL == "" {
		return fmt.Errorf("url must not be empty")
	}

	jobURL, err := url.ParseRequestURI(requestURL)

	if err != nil {
		return errors.New("url must be a valid absolute http(s) URL")
	}

	if jobURL.Host == "" {
		return errors.New("url must include a host")
	}

	if jobURL.Scheme != "http" && jobURL.Scheme != "https" {
		return errors.New("url scheme must be http or https")
	}

	// Priority Validation
	if req.Priority != nil && (*req.Priority < job.High || *req.Priority >= job.PriorityLimit) {
		return errors.New("job priority must be 0 (High), 1 (Normal), or 2 (Low)")
	}

	// Payload Validation
	trimmedPayload := bytes.TrimSpace(req.Payload)
	isEqual := bytes.Equal(trimmedPayload, []byte("null"))
	if len(trimmedPayload) == 0 || isEqual {
		return errors.New("payload must not be empty, null, or nil")
	}

	return nil
}
