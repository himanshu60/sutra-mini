// Command mockacct runs the misbehaving mock accounting API on MOCK_ADDR (default :8081).
package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/himanshu60/sutra-mini/internal/config"
	"github.com/himanshu60/sutra-mini/internal/mockacct"
)

func main() {
	if err := run(); err != nil {
		slog.Error("mock stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(); err != nil {
		return err
	}
	var env config.Env
	cfg := mockacct.Config{
		FailRate:     env.Float("MOCK_FAIL_RATE", 0.2),
		HangRate:     env.Float("MOCK_HANG_RATE", 0.1),
		HangDuration: env.Duration("MOCK_HANG_DURATION", 5*time.Second),
	}
	if err := env.Err(); err != nil {
		return err
	}
	if cfg.FailRate < 0 || cfg.HangRate < 0 || cfg.FailRate+cfg.HangRate > 1 {
		return errors.New("MOCK_FAIL_RATE and MOCK_HANG_RATE must be >= 0 and add up to at most 1")
	}

	addr := config.String("MOCK_ADDR", ":8081")
	slog.Info("mock accounting listening", "addr", addr,
		"fail_rate", cfg.FailRate, "hang_rate", cfg.HangRate, "hang", cfg.HangDuration)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mockacct.New(cfg).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return srv.ListenAndServe()
}
