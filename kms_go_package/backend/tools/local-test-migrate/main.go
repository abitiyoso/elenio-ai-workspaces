// Local acceptance helper: reuses the production migration implementation on a disposable database only.
package main

import (
	"context"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
	"log"
	"net/url"
	"os"
	"time"
)

func main() {
	raw := os.Getenv("KMS_TEST_DATABASE_URL")
	u, err := url.Parse(raw)
	if err != nil || os.Getenv("ELENIO_LOCAL_E2E") != "1" || u.Host != "127.0.0.1:55448" || u.Path != "/kms_tests" {
		log.Fatal("requires the dedicated local E2E KMS database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := platform.OpenDatabase(ctx, raw)
	if err != nil {
		log.Fatal("open local database failed")
	}
	defer db.Close()
	if err := db.Migrate(ctx, "migrations"); err != nil {
		log.Fatal(err)
	}
	log.Print("local KMS test schema migrated")
}
