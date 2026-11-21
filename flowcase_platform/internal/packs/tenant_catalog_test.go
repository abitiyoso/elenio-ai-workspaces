package packs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elenio/flowcase/internal/workflows/catalog"
)

const tenantA = "00000000-0000-4000-8000-000000000041"
const tenantB = "00000000-0000-4000-8000-000000000042"

func businessCatalog(runtime string) catalog.Snapshot {
	return catalog.Snapshot{
		Agents:        []catalog.Agent{{Ref: "business.extract", Name: "Business extraction", RuntimeAgentID: runtime, SkillVersion: "1.2.0", InputContract: "flowcase.case.v1"}},
		Policies:      []catalog.Policy{{Ref: "business.approve", Name: "Independent approval"}},
		AssigneeRoles: []string{"business-reviewer"}, ArtifactTypes: []string{"business.draft"},
	}
}

func TestTenantCatalogConfigurationIsScopedAndRepeatable(t *testing.T) {
	ps, _ := Load()
	base, _ := Catalog(ps)
	b := &Bindings{Tenants: map[string]catalog.Snapshot{tenantA: businessCatalog("finance.extract"), tenantB: businessCatalog("access.analyze")}}
	for range 2 {
		configured, err := b.Configure(base)
		if err != nil {
			t.Fatal(err)
		}
		for tenant, expected := range map[string]string{tenantA: "finance.extract", tenantB: "access.analyze"} {
			scoped := catalog.ForTenant(configured, tenant)
			a, ok := scoped.Agent("business.extract")
			if !ok || a.RuntimeAgentID != expected || a.InputContract != "flowcase.case.v1" || a.Pack != "tenant" {
				t.Fatalf("wrong tenant binding: %+v", a)
			}
			p, ok := scoped.Policy("business.approve")
			if !ok || !p.Provisional || p.Ratification != nil || len(p.SoD) != 2 {
				t.Fatalf("invented business signoff: %+v", p)
			}
			if !scoped.HasArtifactType("business.draft") || !scoped.HasAssigneeRole("business-reviewer") {
				t.Fatal("business contracts missing")
			}
			if _, ok := scoped.Agent("sim.intake"); !ok {
				t.Fatal("existing shared pack lost")
			}
		}
		for _, scoped := range []catalog.Catalog{base, configured, catalog.ForTenant(configured, ""), catalog.ForTenant(configured, "unknown")} {
			if _, ok := scoped.Agent("business.extract"); ok || scoped.HasAssigneeRole("business-reviewer") || scoped.HasArtifactType("business.draft") {
				t.Fatal("tenant catalog leaked into shared/unknown tenant")
			}
		}
	}
	if b.Tenants[tenantA].Agents[0].Pack != "" {
		t.Fatal("configuration mutated its input")
	}
}

func TestTenantCatalogRejectsUnsafeOrAmbiguousRegistrations(t *testing.T) {
	for name, mutate := range map[string]func(*catalog.Snapshot){
		"unpinned":               func(s *catalog.Snapshot) { s.Agents[0].SkillVersion = "unpinned" },
		"invalid version":        func(s *catalog.Snapshot) { s.Agents[0].SkillVersion = "1.2" },
		"unknown contract":       func(s *catalog.Snapshot) { s.Agents[0].InputContract = "sim.v1" },
		"runtime URL":            func(s *catalog.Snapshot) { s.Agents[0].RuntimeAgentID = "https://example.com?token=x" },
		"no runtime":             func(s *catalog.Snapshot) { s.Agents[0].RuntimeAgentID = "" },
		"pack spoof":             func(s *catalog.Snapshot) { s.Agents[0].Pack = "sim-tender" },
		"shared agent override":  func(s *catalog.Snapshot) { s.Agents[0].Ref = "sim.intake" },
		"duplicate agent":        func(s *catalog.Snapshot) { s.Agents = append(s.Agents, s.Agents[0]) },
		"shared policy override": func(s *catalog.Snapshot) { s.Policies[0].Ref = "poc.budget" },
		"duplicate policy":       func(s *catalog.Snapshot) { s.Policies = append(s.Policies, s.Policies[0]) },
		"relaxed separation":     func(s *catalog.Snapshot) { s.Policies[0].SoD = []string{catalog.SoDNotCaseOwner} },
		"vague ratification": func(s *catalog.Snapshot) {
			s.Policies[0].Ratification = &catalog.Ratification{Reference: "ok", Date: "soon", By: "someone"}
		},
		"shared role":        func(s *catalog.Snapshot) { s.AssigneeRoles = []string{"poc-owner"} },
		"duplicate artifact": func(s *catalog.Snapshot) { s.ArtifactTypes = append(s.ArtifactTypes, s.ArtifactTypes[0]) },
		"unsupported connector": func(s *catalog.Snapshot) {
			s.Connectors = []catalog.Connector{{Ref: "arbitrary.http", Name: "Not implemented"}}
		},
		"bounds": func(s *catalog.Snapshot) { s.ArtifactTypes = make([]string, 513) },
	} {
		t.Run(name, func(t *testing.T) {
			ps, _ := Load()
			base, _ := Catalog(ps)
			s := businessCatalog("finance.extract")
			mutate(&s)
			_, err := (&Bindings{Tenants: map[string]catalog.Snapshot{tenantA: s}}).Configure(base)
			if err == nil || !strings.Contains(err.Error(), "PACKS_CONFIG") {
				t.Fatalf("unsafe catalog accepted: %v", err)
			}
			if _, ok := base.Agent("business.extract"); ok {
				t.Fatal("failed config mutated shared catalog")
			}
		})
	}
	for _, tenant := range []string{"acme-demo", "", strings.ToUpper(tenantA[:35]) + "A", "00000000-0000-0000-0000-000000000000"} {
		ps, _ := Load()
		base, _ := Catalog(ps)
		if _, err := (&Bindings{Tenants: map[string]catalog.Snapshot{tenant: businessCatalog("finance.extract")}}).Configure(base); err == nil {
			t.Fatalf("invalid tenant key accepted: %q", tenant)
		}
	}
}

func TestTenantCatalogFileIsStrictAndRequiresCitedRatification(t *testing.T) {
	ps, _ := Load()
	base, _ := Catalog(ps)
	s := businessCatalog("finance.extract")
	s.Policies[0].Provisional = false
	b := &Bindings{Tenants: map[string]catalog.Snapshot{tenantA: s}}
	configured, err := b.Configure(base)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := catalog.ForTenant(configured, tenantA).Policy("business.approve")
	if !p.Provisional {
		t.Fatal("false flag replaced a cited business decision")
	}
	b.Tenants[tenantA].Policies[0].Ratification = &catalog.Ratification{Reference: "Explicit test decision fixture", Date: "2026-11-20", By: "Fixture Finance committee"}
	path := filepath.Join(t.TempDir(), "packs.json")
	raw, _ := json.Marshal(b)
	for _, suffix := range []string{"", ` {"tenant_catalogs":{}}`} {
		if err := os.WriteFile(path, append(append([]byte{}, raw...), []byte(suffix)...), 0o600); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadBindings(path)
		if suffix != "" {
			if err == nil {
				t.Fatal("trailing JSON accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		configured, err := loaded.Configure(base)
		if err != nil {
			t.Fatal(err)
		}
		p, _ := catalog.ForTenant(configured, tenantA).Policy("business.approve")
		if p.Provisional || p.Ratification == nil || p.Ratification.Reference != "Explicit test decision fixture" {
			t.Fatal("explicit decision citation lost")
		}
	}
	if err := os.WriteFile(path, []byte(`{"tenant_catalogs":{"`+tenantA+`":{"endpoint":"https://unapproved.example"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBindings(path); err == nil {
		t.Fatal("unknown tenant catalog fields accepted")
	}
}

func TestTenantCatalogConfigurationBounds(t *testing.T) {
	ps, _ := Load()
	base, _ := Catalog(ps)
	b := &Bindings{Tenants: map[string]catalog.Snapshot{}}
	for i := 1; i <= 129; i++ {
		b.Tenants[fmt.Sprintf("00000000-0000-4000-8000-%012d", i)] = catalog.Snapshot{}
	}
	if _, err := b.Configure(base); err == nil {
		t.Fatal("unbounded tenant map accepted")
	}
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", (4<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBindings(path); err == nil {
		t.Fatal("oversized config file accepted")
	}
}
