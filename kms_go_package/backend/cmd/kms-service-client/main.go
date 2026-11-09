package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	connectionURL := os.Getenv("DATABASE_URL")
	if connectionURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	db, err := platform.OpenDatabase(context.Background(), connectionURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database initialization failed")
		os.Exit(1)
	}
	defer db.Close()

	switch os.Args[1] {
	case "create":
		create(db.SQL())
	case "revoke":
		revoke(db.SQL())
	default:
		usage()
	}
}

type createOptions struct {
	TenantID  string
	ActorID   string
	Name      string
	ExpiresAt time.Time
	Grants    []auth.ResourceGrant
	Scopes    []string
}

func parseCreateArgs(args []string) (createOptions, error) {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	tenant := fs.String("tenant-id", "", "tenant UUID")
	actor := fs.String("actor-id", "", "operator principal UUID")
	name := fs.String("name", "", "service client name")
	expires := fs.String("expires-at", "", "RFC3339 expiry timestamp")
	delegate := fs.Bool("delegate", false, "also grant "+auth.ScopeRetrievalDelegate+": act for a tenant principal named in the "+auth.DelegatedUserHeader+" header")
	var rawGrants stringList
	fs.Var(&rawGrants, "grant", "collection UUID or collection UUID:case UUID; repeatable")
	if err := fs.Parse(args); err != nil {
		return createOptions{}, err
	}
	if fs.NArg() != 0 {
		return createOptions{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	expiresAt, err := time.Parse(time.RFC3339, *expires)
	if err != nil {
		return createOptions{}, fmt.Errorf("expires-at must be RFC3339")
	}
	grants, err := parseGrants(rawGrants)
	if err != nil {
		return createOptions{}, err
	}
	return createOptions{TenantID: *tenant, ActorID: *actor, Name: *name, ExpiresAt: expiresAt, Grants: grants, Scopes: auth.ServiceClientScopes(*delegate)}, nil
}

func create(db *sql.DB) {
	opts, err := parseCreateArgs(os.Args[2:])
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(2)
	}
	token, clientID, err := auth.ProvisionServiceClient(context.Background(), db, opts.TenantID, opts.ActorID, opts.Name, opts.ExpiresAt, opts.Grants, opts.Scopes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "service client creation failed")
		os.Exit(1)
	}
	fmt.Printf("client_id=%s\naudience=%s\nscopes=%s\nexpires_at=%s\ntoken=%s\n", clientID, auth.RetrievalAudience, strings.Join(opts.Scopes, ","), opts.ExpiresAt.UTC().Format(time.RFC3339), token)
}

func parseGrants(rawGrants []string) ([]auth.ResourceGrant, error) {
	grants := make([]auth.ResourceGrant, 0, len(rawGrants))
	for _, raw := range rawGrants {
		parts := strings.Split(raw, ":")
		if len(parts) == 1 {
			grants = append(grants, auth.ResourceGrant{CollectionID: parts[0]})
		} else if len(parts) == 2 {
			grants = append(grants, auth.ResourceGrant{CollectionID: parts[0], CaseID: parts[1]})
		} else {
			return nil, fmt.Errorf("grant must be collection UUID or collection UUID:case UUID")
		}
	}
	return grants, nil
}

func revoke(db *sql.DB) {
	fs := flag.NewFlagSet("revoke", flag.ExitOnError)
	tenant := fs.String("tenant-id", "", "tenant UUID")
	actor := fs.String("actor-id", "", "operator principal UUID")
	client := fs.String("client-id", "", "service client UUID")
	_ = fs.Parse(os.Args[2:])
	if err := auth.RevokeServiceClient(context.Background(), db, *tenant, *actor, *client); err != nil {
		fmt.Fprintln(os.Stderr, "service client revocation failed")
		os.Exit(1)
	}
	fmt.Println("status=revoked")
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: kms-service-client create --tenant-id UUID --actor-id UUID --name NAME --expires-at RFC3339 --grant COLLECTION[:CASE] [...] [--delegate]")
	fmt.Fprintln(os.Stderr, "       kms-service-client revoke --tenant-id UUID --actor-id UUID --client-id UUID")
	os.Exit(2)
}
