package packs

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/elenio/flowcase/internal/workflows/catalog"
)

var tenantIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var businessRefPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,119}$`)
var pinnedVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Configure extends the existing operator-owned PACKS_CONFIG boundary, never workflow JSON.
// Registrations neither grant membership/delegation nor approve Agent releases or KMS access.
func (b *Bindings) Configure(base *catalog.Static) (catalog.Catalog, error) {
	if err := b.Apply(base); err != nil {
		return nil, err
	}
	if b == nil {
		return base, nil
	}
	if len(b.Tenants) > 128 {
		return nil, fmt.Errorf("PACKS_CONFIG: too many tenant catalogs")
	}
	additions := make(map[string]catalog.Snapshot, len(b.Tenants))
	for tenant, snapshot := range b.Tenants {
		if !tenantIDPattern.MatchString(tenant) || tenant == "00000000-0000-0000-0000-000000000000" {
			return nil, fmt.Errorf("PACKS_CONFIG: tenant catalog key must be a canonical source tenant UUID")
		}
		if err := validateTenantCatalog(base, &snapshot); err != nil {
			return nil, fmt.Errorf("PACKS_CONFIG tenants.%s: %w", tenant, err)
		}
		additions[tenant] = snapshot
	}
	configured, err := catalog.WithTenants(base, additions)
	if err != nil {
		return nil, fmt.Errorf("PACKS_CONFIG: %w", err)
	}
	return configured, nil
}

func validateTenantCatalog(base catalog.Catalog, s *catalog.Snapshot) error {
	s.Agents, s.Policies = slices.Clone(s.Agents), slices.Clone(s.Policies)
	if len(s.Agents)+len(s.Policies)+len(s.AssigneeRoles)+len(s.ArtifactTypes) > 512 || len(s.Connectors) != 0 {
		return fmt.Errorf("catalog exceeds bounds or attempts to register an unsupported connector")
	}
	validName := func(name string) bool { return strings.TrimSpace(name) == name && len(name) > 0 && len(name) <= 160 }
	for i := range s.Agents {
		a := &s.Agents[i]
		if !businessRefPattern.MatchString(a.Ref) || !validName(a.Name) || !runtimeIDPattern.MatchString(a.RuntimeAgentID) || strings.Contains(a.RuntimeAgentID, "://") ||
			len(a.SkillVersion) > 40 || (!pinnedVersionPattern.MatchString(a.SkillVersion) && !tenantIDPattern.MatchString(a.SkillVersion)) || a.InputContract != "flowcase.case.v1" || a.Pack != "" {
			return fmt.Errorf("agent registration requires a valid ref/name, explicit runtime ID, immutable version and flowcase.case.v1 contract; pack is server-owned")
		}
		a.Pack = "tenant"
	}
	for i := range s.Policies {
		p := &s.Policies[i]
		if !businessRefPattern.MatchString(p.Ref) || !validName(p.Name) || len(p.Description) > 2000 || p.Pack != "" {
			return fmt.Errorf("invalid policy ref/name/description or caller-supplied pack")
		}
		if len(p.SoD) != 0 && (len(p.SoD) != len(catalog.SoDRules) || !slices.Contains(p.SoD, catalog.SoDNotCaseOwner) || !slices.Contains(p.SoD, catalog.SoDNotTaskSubmitter)) {
			return fmt.Errorf("tenant policies must retain independent owner and submitter separation")
		}
		p.Pack, p.Provisional = "tenant", true
		if r := p.Ratification; r != nil {
			if _, err := time.Parse("2006-01-02", r.Date); err != nil || len(strings.TrimSpace(r.Reference)) < 3 || len(r.Reference) > 256 || !validName(r.By) {
				return fmt.Errorf("policy ratification needs a bounded decision reference, date and approver")
			}
			p.Provisional = false
		}
	}
	for _, refs := range []struct {
		values []string
		exists func(string) bool
	}{{s.AssigneeRoles, base.HasAssigneeRole}, {s.ArtifactTypes, base.HasArtifactType}} {
		seen := map[string]bool{}
		for _, ref := range refs.values {
			if !businessRefPattern.MatchString(ref) || seen[ref] || refs.exists(ref) {
				return fmt.Errorf("invalid, duplicate or shared role/artifact reference")
			}
			seen[ref] = true
		}
	}
	return nil
}
