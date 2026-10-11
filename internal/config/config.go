// Package config reads settings from the environment, with an optional .env file.
package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/joho/godotenv"
)

// LoadDotEnv loads .env from the working directory if it exists.
// Variables already set in the environment win over the file.
func LoadDotEnv() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// String returns the variable's value, or def when it is unset or empty.
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
