// Command migrate applies the schema and seed migrations to DATABASE_URL.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/himanshu60/sutra-mini/internal/config"
	"github.com/himanshu60/sutra-mini/internal/db"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pool, err := db.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	slog.Info("migrations up to date", "applied_now", applied)
	return nil
}
