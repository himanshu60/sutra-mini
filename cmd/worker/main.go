// Command worker pushes pending vouchers to the accounting API at ACCOUNTING_URL.
// Run as many copies as you like; they coordinate through the database.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/himanshu60/sutra-mini/internal/config"
	"github.com/himanshu60/sutra-mini/internal/db"
	"github.com/himanshu60/sutra-mini/internal/store"
	"github.com/himanshu60/sutra-mini/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(); err != nil {
		return err
	}
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	var env config.Env
	cfg := worker.Config{
		ID:           fmt.Sprintf("%s-%d", host, os.Getpid()),
		Concurrency:  env.Int("WORKER_CONCURRENCY", 4),
		PollInterval: time.Second,
		Lease:        env.Duration("WORKER_LEASE", 15*time.Second),
		MaxAttempts:  env.Int("WORKER_MAX_ATTEMPTS", 5),
		BackoffBase:  2 * time.Second,
		BackoffMax:   time.Minute,
	}
	httpTimeout := env.Duration("WORKER_HTTP_TIMEOUT", 8*time.Second)
	if err := env.Err(); err != nil {
		return err
	}
	if cfg.Concurrency < 1 || cfg.MaxAttempts < 1 {
		return errors.New("WORKER_CONCURRENCY and WORKER_MAX_ATTEMPTS must be at least 1")
	}
	if cfg.Lease <= httpTimeout {
		return errors.New("WORKER_LEASE must be longer than WORKER_HTTP_TIMEOUT, or a slow push could be claimed by a second worker")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // a second Ctrl+C now kills the process instead of waiting for in-flight pushes
	}()

	pool, err := db.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	accountingURL := config.String("ACCOUNTING_URL", "http://localhost:8081")
	slog.Info("worker started", "id", cfg.ID, "accounting_url", accountingURL,
		"concurrency", cfg.Concurrency, "lease", cfg.Lease, "http_timeout", httpTimeout, "max_attempts", cfg.MaxAttempts)
	worker.New(store.New(pool), worker.NewClient(accountingURL, httpTimeout), cfg).Run(ctx)
	slog.Info("worker stopped cleanly", "id", cfg.ID)
	return nil
}
