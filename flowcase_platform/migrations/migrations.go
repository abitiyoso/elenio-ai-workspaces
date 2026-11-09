// Package migrations embeds the ordered SQL migrations: NNNNNN_name.up.sql / NNNNNN_name.down.sql.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
