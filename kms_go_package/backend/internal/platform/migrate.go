package platform

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (d *Database) Migrate(ctx context.Context, directory string) error {
	if d == nil || d.db == nil {
		return sql.ErrConnDone
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var files []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		version, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
		if err != nil || version <= 0 {
			return fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		files = append(files, migrationFile{version: version, path: filepath.Join(directory, entry.Name())})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, file := range files {
		if file.version == 1 {
			var exists bool
			if err := d.db.QueryRowContext(ctx, `SELECT to_regclass('public.tenants') IS NOT NULL`).Scan(&exists); err != nil {
				return fmt.Errorf("inspect foundation schema: %w", err)
			}
			if exists {
				if _, err := d.db.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (1) ON CONFLICT DO NOTHING`); err != nil {
					return fmt.Errorf("baseline foundation migration: %w", err)
				}
				continue
			}
		}
		var applied bool
		if err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, file.version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", file.version, err)
		}
		if applied {
			continue
		}
		contents, err := os.ReadFile(file.path)
		if err != nil {
			return fmt.Errorf("read migration %d: %w", file.version, err)
		}
		statements := splitSQLStatements(string(contents))
		tx, err := d.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", file.version, err)
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(763193421, $1)`, int32(file.version)); err == nil {
			var appliedAfterLock bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, file.version).Scan(&appliedAfterLock)
			if err == nil && !appliedAfterLock {
				for _, statement := range statements {
					if _, err = tx.ExecContext(ctx, statement); err != nil {
						break
					}
				}
			} else if err == nil {
				_ = tx.Rollback()
				continue
			}
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, file.version)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", file.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", file.version, err)
		}
	}
	return nil
}

type migrationFile struct {
	version int64
	path    string
}

func splitSQLStatements(source string) []string {
	var statements []string
	start := 0
	var quote byte
	lineComment, blockComment := false, false
	var dollarQuote string
	for i := 0; i < len(source); i++ {
		if lineComment {
			if source[i] == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if i+1 < len(source) && source[i] == '*' && source[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if dollarQuote != "" {
			if strings.HasPrefix(source[i:], dollarQuote) {
				i += len(dollarQuote) - 1
				dollarQuote = ""
			}
			continue
		}
		if quote != 0 {
			if source[i] == quote {
				if i+1 < len(source) && source[i+1] == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		switch {
		case i+1 < len(source) && source[i] == '-' && source[i+1] == '-':
			lineComment = true
			i++
		case i+1 < len(source) && source[i] == '/' && source[i+1] == '*':
			blockComment = true
			i++
		case source[i] == '\'' || source[i] == '"':
			quote = source[i]
		case source[i] == '$':
			if end := strings.IndexByte(source[i+1:], '$'); end >= 0 {
				tag := source[i : i+end+2]
				if strings.Trim(tag[1:len(tag)-1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
					dollarQuote = tag
					i += len(tag) - 1
				}
			}
		case source[i] == ';':
			statement := strings.TrimSpace(source[start:i])
			if statement != "" {
				statements = append(statements, statement)
			}
			start = i + 1
		}
	}
	if statement := strings.TrimSpace(source[start:]); statement != "" {
		statements = append(statements, statement)
	}
	return statements
}
