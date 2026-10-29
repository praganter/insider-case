// Package migrations embeds the SQL files so the seed binary can apply them.
package migrations

import "embed"

// Files are applied in name order; every statement is idempotent.
//
//go:embed *.sql
var Files embed.FS
