package postgres_test

import (
	"io/fs"

	"github.com/elenio/flowcase/migrations"
)

func migrationsFS() fs.FS { return migrations.FS }
