// Package sqlcheck verifies every static SQL statement in the backend against a migrated PostgreSQL schema.
// Unit tests cannot catch parameter type inference or column errors; PREPARE does, without executing anything.
//
// Run with: KMS_TEST_DATABASE_URL=postgres://... go test ./internal/sqlcheck/
package sqlcheck

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

var queryMethods = map[string]int{"ExecContext": 1, "QueryContext": 1, "QueryRowContext": 1, "PrepareContext": 1}

type statement struct {
	position string
	sql      string
}

func TestEveryStaticStatementPreparesAgainstMigratedSchema(t *testing.T) {
	url := os.Getenv("KMS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KMS_TEST_DATABASE_URL is not set; point it at a migrated (disposable or read-only) KMS database")
	}
	statements := collectStatements(t, "..", "../../cmd")
	if len(statements) < 50 {
		t.Fatalf("found only %d statements; the source walk is probably broken", len(statements))
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, stmt := range statements {
		if _, err := conn.Prepare(ctx, "", stmt.sql); err != nil {
			t.Errorf("%s: %v", stmt.position, err)
		}
	}
	t.Logf("prepared %d statements", len(statements))
}

func TestCollectorEvaluatesConstantConcatenation(t *testing.T) {
	dir := t.TempDir()
	source := "package demo\nconst tail = ` FROM t WHERE id=$1`\nfunc f(db interface{ ExecContext(...any) }) { db.ExecContext(nil, `DELETE`+tail, 1); db.ExecContext(nil, dynamic(), 1) }\nfunc dynamic() string { return \"\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	statements := collectStatements(t, dir)
	if len(statements) != 1 || statements[0].sql != "DELETE FROM t WHERE id=$1" {
		t.Fatalf("statements=%+v", statements)
	}
}

func collectStatements(t *testing.T, roots ...string) []statement {
	t.Helper()
	packages := map[string][]*ast.File{}
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			packages[filepath.Dir(path)] = append(packages[filepath.Dir(path)], file)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var statements []statement
	for _, files := range packages {
		consts := map[string]ast.Expr{}
		for _, file := range files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					value := spec.(*ast.ValueSpec)
					for i, name := range value.Names {
						if i < len(value.Values) {
							consts[name.Name] = value.Values[i]
						}
					}
				}
			}
		}
		for _, file := range files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				index, ok := queryMethods[selector.Sel.Name]
				if !ok || len(call.Args) <= index {
					return true
				}
				if sql, ok := evalString(call.Args[index], consts); ok {
					statements = append(statements, statement{position: fset.Position(call.Pos()).String(), sql: sql})
				}
				return true
			})
		}
	}
	sort.Slice(statements, func(i, j int) bool { return statements[i].position < statements[j].position })
	return statements
}

// evalString resolves string literals, same-package constants and their concatenations;
// dynamically built queries are skipped.
func evalString(expr ast.Expr, consts map[string]ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		return text, err == nil
	case *ast.Ident:
		if bound, ok := consts[value.Name]; ok {
			return evalString(bound, consts)
		}
	case *ast.ParenExpr:
		return evalString(value.X, consts)
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, ok := evalString(value.X, consts)
		if !ok {
			return "", false
		}
		right, ok := evalString(value.Y, consts)
		return left + right, ok
	}
	return "", false
}
