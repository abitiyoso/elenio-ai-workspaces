// Package migrations embeds UCW's PostgreSQL schema migrations (applied in file-name order at startup).
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
