// Package config reads settings from the environment, with an optional .env file.
package config

import (
	"errors"
	"io/fs"

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
