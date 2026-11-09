// Package connector invokes allowlisted service operations for service_task steps. Workflows name a
// registered connector and operation; they can never supply URLs, queries or code.
package connector

import (
	"context"
	"errors"
	"sync"
)

var ErrUnavailable = errors.New("connector is not available in this runtime")

// Call carries an idempotency key; implementations must apply the side effect at most once per key.
type Call struct {
	TenantID, CaseID, Connector, Operation, IdempotencyKey string
	Params                                                 map[string]any
}

type Connector interface {
	Invoke(ctx context.Context, c Call) (map[string]any, error)
}

type Registry interface {
	Get(ref string) (Connector, bool)
}

type Static map[string]Connector

func (s Static) Get(ref string) (Connector, bool) { c, ok := s[ref]; return c, ok }

// Stub is a development connector that records invocations and replays the first result for a
// repeated idempotency key, demonstrating the at-most-once side-effect contract.
type Stub struct {
	mu      sync.Mutex
	results map[string]map[string]any
	Applied int // number of distinct side effects applied
}

func NewStub() *Stub { return &Stub{results: map[string]map[string]any{}} }

func (s *Stub) Invoke(_ context.Context, c Call) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.results[c.IdempotencyKey]; ok {
		return r, nil
	}
	s.Applied++
	r := map[string]any{"stub": true, "connector": c.Connector, "operation": c.Operation, "accepted": true}
	s.results[c.IdempotencyKey] = r
	return r, nil
}
