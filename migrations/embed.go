// Package migrations embeds the SQL files so every binary and test applies the same schema.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
