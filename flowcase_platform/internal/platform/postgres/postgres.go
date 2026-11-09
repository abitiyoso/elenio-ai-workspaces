// Package postgres owns the connection pool, schema migrations and SQLSTATE mapping.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLSTATEs raised by Flowcase triggers.
const (
	CodeImmutable  = "FC001" // published version / pinned case / audit row change attempt
	CodePinInvalid = "FC002" // case started from a non-published or mismatching version
)

func Open(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.ConnConfig.RuntimeParams["application_name"] = "flowcase"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	return pool, nil
}

// HasCode reports whether err is a PostgreSQL error with the given SQLSTATE.
func HasCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
