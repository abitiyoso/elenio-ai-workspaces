package integrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/elenio/flowcase/internal/integrations/agent"
	"github.com/elenio/flowcase/internal/integrations/kms"
)

func TestDisabledAdaptersRefuseWork(t *testing.T) {
	ctx := context.Background()
	if _, err := kms.NewDisabled().Search(ctx, kms.Scope{TenantID: "t"}, kms.SearchRequest{Query: "x"}); !errors.Is(err, kms.ErrDisabled) {
		t.Fatalf("disabled KMS must refuse search, got %v", err)
	}
	if _, err := agent.NewDisabled().StartRun(ctx, agent.StartRunRequest{IdempotencyKey: "k", AgentID: "a"}); !errors.Is(err, agent.ErrDisabled) {
		t.Fatalf("disabled agent runtime must refuse runs, got %v", err)
	}
}

func TestStubsAnnounceThemselves(t *testing.T) {
	ctx := context.Background()
	if h := kms.NewDevelopmentStub().Health(ctx); h.Status != "stub" {
		t.Fatalf("KMS stub must report stub status, got %+v", h)
	}
	if h := agent.NewDevelopmentStub("").Health(ctx); h.Status != "stub" {
		t.Fatalf("agent stub must report stub status, got %+v", h)
	}
}

func TestKMSStubStaysWithinCallerTenant(t *testing.T) {
	scope := kms.Scope{TenantID: "tenant-a", UserID: "u"}
	res, err := kms.NewDevelopmentStub().Search(context.Background(), scope, kms.SearchRequest{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Citations {
		if err := kms.CheckTenant(scope, c.Source); err != nil {
			t.Fatalf("stub returned cross-tenant source: %+v", c.Source)
		}
	}
	if err := kms.CheckTenant(scope, kms.Source{TenantID: "tenant-b"}); !errors.Is(err, kms.ErrForbidden) {
		t.Fatalf("cross-tenant source must be rejected, got %v", err)
	}
}

func TestAgentStubNeverAcceptsCallbacks(t *testing.T) {
	_, err := agent.NewDevelopmentStub("").VerifyResult(context.Background(), agent.CallbackEnvelope{Provider: "stub", Body: []byte(`{"status":"succeeded"}`)})
	if !errors.Is(err, agent.ErrInvalidCallback) {
		t.Fatalf("unsigned callbacks must be rejected, got %v", err)
	}
}
