package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/ronitanilkumar/dispatch/api"
	"github.com/ronitanilkumar/dispatch/delivery"
	"github.com/ronitanilkumar/dispatch/queue"
	"github.com/ronitanilkumar/dispatch/worker"
)

const numWorkers = 16
const deliveryTimeout = 5 * time.Second
const shutdownTimeout = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	q := queue.NewQueue()
	client := delivery.NewClient(deliveryTimeout)
	pool := worker.NewPool(q, client, numWorkers)
	handler := api.NewHandler(q)

	pool.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", handler.SubmitJobHandler)
	mux.HandleFunc("DELETE /jobs/{id}", handler.CancelJobHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	serverErrCh := make(chan error, 1)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-serverErrCh:
		log.Printf("server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}

	pool.Stop()
}
