// Command devtoken prints a short-lived Control Plane delegation token for one user and audience.
//
// DEVELOPMENT OPERATOR TOOL: it signs with the Control Plane's service-token key, so it can act as any user
// towards a mini-app. It exists to seed demo data through the apps' real APIs (so every action is still
// authorised and audited by the app under that user). Run it only where the key already lives (the VM).
//
//	devtoken -key <signing key file> -issuer http://cp.example -aud kms -sub <cp user id> [-workspace sim-b2b]
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/elenio/control-plane/internal/integrations"
)

func main() {
	keyFile := flag.String("key", "", "ES256 signing key file")
	issuer := flag.String("issuer", "", "Control Plane origin")
	aud := flag.String("aud", "", "audience (kms, flowcase, agent-studio)")
	sub := flag.String("sub", "", "Control Plane user id")
	tenant := flag.String("tenant", "acme-demo", "tenant key")
	workspace := flag.String("workspace", "sim-b2b", "workspace key")
	flag.Parse()
	if *keyFile == "" || *issuer == "" || *aud == "" || *sub == "" {
		fmt.Fprintln(os.Stderr, "devtoken: -key, -issuer, -aud and -sub are required")
		os.Exit(2)
	}
	pemKey, err := os.ReadFile(*keyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "devtoken:", err)
		os.Exit(2)
	}
	is, err := integrations.NewIssuer(*issuer, 5*time.Minute, pemKey, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "devtoken:", err)
		os.Exit(2)
	}
	tok, _, err := is.Mint(*aud, integrations.Subject{UserID: *sub, TenantKey: *tenant, WorkspaceKey: *workspace, PrincipalType: "human",
		SessionID: "devtoken", Scopes: []string{"app:open"}, PolicyVersion: "devtoken", CorrelationID: "devtoken", AuthTime: time.Now()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "devtoken:", err)
		os.Exit(2)
	}
	fmt.Print(tok)
}
