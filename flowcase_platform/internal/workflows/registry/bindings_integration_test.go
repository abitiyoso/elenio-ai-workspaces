package registry_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/registry"
)

func TestPublishedBindingEvidenceTracksCatalogDriftWithoutChangingWorkflow(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	if _, err := e.svc.SeedPacks(e.ctx, a, e.ps); err != nil {
		t.Fatal(err)
	}
	first, err := e.svc.ObserveBindings(e.ctx, a, "sim-tender", 4)
	if err != nil || len(first.Bindings) < 12 || len(first.ContentHash) != 64 {
		t.Fatalf("binding observation: %+v %v", first, err)
	}
	again, err := e.svc.ObserveBindings(e.ctx, a, "sim-tender", 4)
	if err != nil || again.ContentHash != first.ContentHash {
		t.Fatal("observation time changed binding digest", err)
	}
	cat := e.svc.Catalog.(*catalog.Static)
	agent, _ := cat.Agent("sim.intake")
	agent.InputContract = "flowcase.case.v1"
	agent.RuntimeAgentID = "customer.intake"
	cat.SetAgent(agent)
	changed, err := e.svc.ObserveBindings(e.ctx, a, "sim-tender", 4)
	if err != nil || changed.DefinitionHash != first.DefinitionHash || changed.ContentHash == first.ContentHash {
		t.Fatal("catalog drift was not distinguished from immutable workflow", err)
	}
	found := false
	for _, b := range changed.Bindings {
		if b.Ref == "sim.intake" {
			found = b.Registered && b.InputContract == "flowcase.case.v1" && b.RuntimeAgentID == "customer.intake"
		}
	}
	if !found {
		t.Fatal("trusted binding not observed")
	}
	raw, _ := json.Marshal(changed)
	for _, private := range []string{`"instructions"`, `"config"`, `"params"`, `"input_schema"`} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("definition payload leaked: %s", private)
		}
	}
	if _, err = e.svc.ObserveBindings(e.ctx, a, "sim-tender", 5); err == nil {
		t.Fatal("draft presented as published binding evidence")
	}
	foreign := e.member("other-tenant", "other-reader", auth.KindHuman, auth.RoleWorkspaceMember)
	if _, err = e.svc.ObserveBindings(e.ctx, foreign, "sim-tender", 4); !errors.Is(err, registry.ErrNotFound) {
		t.Fatal("cross-tenant binding observation", err)
	}
	if _, err = e.svc.ObserveBindings(e.ctx, auth.Identity{}, "sim-tender", 4); !errors.Is(err, registry.ErrForbidden) {
		t.Fatal("unauthenticated binding observation", err)
	}
}
