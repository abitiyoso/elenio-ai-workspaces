package platform

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Database struct{ db *sql.DB }

func OpenDatabase(_ context.Context, connectionURL string) (*Database, error) {
	db, err := sql.Open("pgx", connectionURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &Database{db: db}, nil
}

func (d *Database) Ping(ctx context.Context) error {
	if d == nil || d.db == nil {
		return sql.ErrConnDone
	}
	return d.db.PingContext(ctx)
}

func (d *Database) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

func (d *Database) SQL() *sql.DB {
	if d == nil {
		return nil
	}
	return d.db
}
