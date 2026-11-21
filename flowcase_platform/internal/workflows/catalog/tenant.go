package catalog

// TenantCatalog keeps deployment-owned additions out of every other tenant's view.
// Snapshot and unscoped lookups expose only the original shared catalog.
type TenantCatalog struct {
	Catalog
	tenants map[string]Catalog
}

func WithTenants(base Catalog, additions map[string]Snapshot) (Catalog, error) {
	if len(additions) == 0 {
		return base, nil
	}
	out := &TenantCatalog{Catalog: base, tenants: map[string]Catalog{}}
	for tenant, snapshot := range additions {
		merged := NewStatic()
		if err := merged.Merge(base.Snapshot()); err != nil {
			return nil, err
		}
		if err := merged.Merge(snapshot); err != nil {
			return nil, err
		}
		out.tenants[tenant] = merged
	}
	return out, nil
}

func (c *TenantCatalog) ForTenant(tenant string) Catalog {
	if scoped, ok := c.tenants[tenant]; ok {
		return scoped
	}
	return c.Catalog
}

// ForTenant accepts only a tenant resolved by the source directory or stored case.
func ForTenant(c Catalog, tenant string) Catalog {
	if scoped, ok := c.(interface{ ForTenant(string) Catalog }); ok {
		return scoped.ForTenant(tenant)
	}
	return c
}
