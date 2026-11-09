// Package migrations embeds the ordered SQL migrations applied by the control-plane binary.
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
