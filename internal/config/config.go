// Package config reads settings from the environment, with an optional .env file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"time"

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

// Env reads typed variables and remembers the first one that failed to parse,
// so a typo in .env stops the program instead of silently using a default.
type Env struct {
	err error
}

// Err returns the first parse error, if any.
func (e *Env) Err() error { return e.err }

// Float returns the variable as a float64, or def when it is unset.
func (e *Env) Float(key string, def float64) float64 {
	return get(e, key, def, func(s string) (float64, error) { return strconv.ParseFloat(s, 64) })
}

// Int returns the variable as an int, or def when it is unset.
func (e *Env) Int(key string, def int) int {
	return get(e, key, def, strconv.Atoi)
}

// Duration returns the variable as a duration such as "5s", or def when it is unset.
func (e *Env) Duration(key string, def time.Duration) time.Duration {
	return get(e, key, def, time.ParseDuration)
}

func get[T any](e *Env, key string, def T, parse func(string) (T, error)) T {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	x, err := parse(v)
	if err != nil {
		if e.err == nil {
			e.err = fmt.Errorf("invalid %s=%q: %w", key, v, err)
		}
		return def
	}
	return x
}
