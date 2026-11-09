// Package migrations embeds the ordered SQL migrations: NNNNNN_name.up.sql / NNNNNN_name.down.sql.
package migrations

import "embed"

// FS contains the schema migrations used by the Agent Studio database layer.
//
//go:embed *.sql
var FS embed.FS
