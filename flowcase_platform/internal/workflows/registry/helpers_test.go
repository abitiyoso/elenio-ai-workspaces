package registry_test

import "github.com/elenio/flowcase/internal/audit"

func auditFilter() audit.Filter { return audit.Filter{Limit: 100} }
