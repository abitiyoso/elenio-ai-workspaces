package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/elenio/agent-studio/internal/repository/postgres"
)

// Store is the persistence the worker needs; *postgres.ModelRegistry implements it.
type Store interface {
	ClaimWork(ctx context.Context, kind, worker string, lease time.Duration) (postgres.WorkItem, bool, error)
	ExtendLease(ctx context.Context, kind, refID, worker string, lease time.Duration) (bool, error)
	CompleteWork(ctx context.Context, kind, refID, worker string) error
	RescheduleWork(ctx context.Context, kind, refID, worker string, delay time.Duration) error
	BeginRunExecution(ctx context.Context, tenantID, runID string) (postgres.RunExecution, error)
	AppendRunEvents(ctx context.Context, tenantID, runID string, events []postgres.RunEvent) error
	RecordEvidence(ctx context.Context, tenantID, runID string, refs []postgres.EvidenceRef) error
	FinishRun(ctx context.Context, tenantID, runID string, result postgres.RunResult) (postgres.Run, error)
	RequeueRun(ctx context.Context, tenantID, runID, errorCode, providerError string, delay time.Duration) error
	CancelRequested(ctx context.Context, tenantID, runID string) (bool, error)
	EvalCaseForRun(ctx context.Context, tenantID, runID string) (postgres.EvalCase, bool, error)
	RecordEvalOutcome(ctx context.Context, tenantID, runID string, passed bool, checks json.RawMessage) error
	GetOutboxEvent(ctx context.Context, tenantID, id string) (postgres.OutboxEvent, error)
	MarkOutbox(ctx context.Context, tenantID, id, status, lastError string) error
	PurgeRunEvents(ctx context.Context, tenantID string, olderThan time.Duration) (int64, error)
}

type Worker struct {
	Store       Store
	Executor    *Executor
	Dispatcher  *FlowcaseDispatcher
	ID          string
	Concurrency int
	Lease       time.Duration
	Retention   time.Duration
	Logger      *log.Logger

	purgeMu   sync.Mutex
	lastPurge map[string]time.Time
}

// Run processes runs and outbox deliveries until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if w.Lease <= 0 {
		w.Lease = 30 * time.Second
	}
	if w.Concurrency <= 0 {
		w.Concurrency = 2
	}
	w.lastPurge = map[string]time.Time{}
	var wg sync.WaitGroup
	for i := 0; i < w.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx, "run", w.processRun)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.loop(ctx, "outbox", w.processOutbox)
	}()
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context, kind string, process func(context.Context, postgres.WorkItem)) {
	for ctx.Err() == nil {
		item, ok, err := w.Store.ClaimWork(ctx, kind, w.ID, w.Lease)
		if err != nil && ctx.Err() == nil {
			w.logf("claim %s: %v", kind, err)
		}
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		process(ctx, item)
	}
}

func (w *Worker) logf(format string, args ...any) {
	if w.Logger != nil {
		w.Logger.Printf(format, args...)
	}
}

type bufferedSink struct {
	store    Store
	tenantID string
	runID    string
}

func (s bufferedSink) Emit(ctx context.Context, event postgres.RunEvent) {
	// Trace writes must not depend on the run context, which may be cancelled.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = s.store.AppendRunEvents(writeCtx, s.tenantID, s.runID, []postgres.RunEvent{event})
}

func (w *Worker) processRun(ctx context.Context, item postgres.WorkItem) {
	exec, err := w.Store.BeginRunExecution(ctx, item.TenantID, item.RefID)
	if errors.Is(err, postgres.ErrInvalidState) || errors.Is(err, postgres.ErrRunNotFound) {
		_ = w.Store.CompleteWork(ctx, "run", item.RefID, w.ID)
		return
	}
	if err != nil {
		w.logf("begin run %s: %v", item.RefID, err)
		_ = w.Store.RescheduleWork(ctx, "run", item.RefID, w.ID, 10*time.Second)
		return
	}
	persist := context.WithoutCancel(ctx)
	if exec.Run.ErrorCode != nil && (*exec.Run.ErrorCode == "pinned_config_changed" || *exec.Run.ErrorCode == "model_approval_revoked") {
		w.finish(persist, item, exec.Run, Outcome{Status: "failed", ErrorCode: *exec.Run.ErrorCode})
		return
	}
	if exec.Run.CancelRequested {
		w.finish(persist, item, exec.Run, Outcome{Status: "cancelled", ErrorCode: "cancelled"})
		return
	}
	if time.Now().After(exec.Run.DeadlineAt) {
		w.finish(persist, item, exec.Run, Outcome{Status: "failed", ErrorCode: "deadline_exceeded"})
		return
	}

	runCtx, cancel := context.WithDeadline(ctx, exec.Run.DeadlineAt)
	defer cancel()
	var cancelled, leaseLost bool
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(w.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if ok, err := w.Store.ExtendLease(persist, "run", item.RefID, w.ID, w.Lease); err == nil && !ok {
					mu.Lock()
					leaseLost = true
					mu.Unlock()
					cancel()
					return
				}
				if requested, err := w.Store.CancelRequested(persist, item.TenantID, item.RefID); err == nil && requested {
					mu.Lock()
					cancelled = true
					mu.Unlock()
					cancel()
					return
				}
			}
		}
	}()
	sink := bufferedSink{store: w.Store, tenantID: item.TenantID, runID: item.RefID}
	outcome, execErr := w.Executor.Execute(runCtx, exec, sink)
	close(done)
	mu.Lock()
	wasCancelled, lost := cancelled, leaseLost
	mu.Unlock()

	if err := w.Store.RecordEvidence(persist, item.TenantID, item.RefID, outcome.Evidence); err != nil {
		w.logf("record evidence %s: %v", item.RefID, err)
	}
	var providerErr *ProviderError
	if errors.As(execErr, &providerErr) {
		outcome.ProviderError = providerErr.Code
	}
	switch {
	case lost:
		// Another worker owns the run now; do not settle it twice.
		return
	case ctx.Err() != nil && !wasCancelled:
		// Worker shutdown: leave the run leased so it is replayed after expiry.
		return
	case wasCancelled:
		outcome.Status, outcome.ErrorCode = "cancelled", "cancelled"
	case execErr != nil && errors.Is(execErr, ErrRetryable) && exec.Run.Attempts < exec.Run.MaxAttempts:
		delay := time.Duration(1<<exec.Run.Attempts)*2*time.Second + time.Duration(rand.IntN(1000))*time.Millisecond
		if err := w.Store.RequeueRun(persist, item.TenantID, item.RefID, "provider_unavailable", outcome.ProviderError, delay); err != nil {
			w.logf("requeue run %s: %v", item.RefID, err)
		}
		return
	case execErr != nil && errors.Is(execErr, ErrRetryable):
		outcome.Status, outcome.ErrorCode = "failed", "provider_unavailable"
	case execErr != nil && errors.Is(execErr, context.DeadlineExceeded):
		outcome.Status, outcome.ErrorCode = "failed", "deadline_exceeded"
	case execErr != nil:
		outcome.Status, outcome.ErrorCode = "failed", "runtime_error"
	}
	w.finish(persist, item, exec.Run, outcome)
}

func (w *Worker) finish(ctx context.Context, item postgres.WorkItem, run postgres.Run, outcome Outcome) {
	validation, _ := json.Marshal(outcome.Validation)
	checksum := ""
	if len(outcome.Output) > 0 {
		checksum, _ = postgres.OutputChecksum(outcome.Output)
	}
	settled, err := w.Store.FinishRun(ctx, item.TenantID, item.RefID, postgres.RunResult{
		Status: outcome.Status, Output: outcome.Output, OutputChecksum: checksum, Validation: validation, ErrorCode: outcome.ErrorCode, ProviderError: outcome.ProviderError,
		TokensIn: outcome.TokensIn, TokensOut: outcome.TokensOut, ToolCalls: outcome.ToolCalls, CitedSources: outcome.Validation.CitedSources,
	})
	if err != nil && !errors.Is(err, postgres.ErrInvalidState) {
		w.logf("finish run %s: %v", item.RefID, err)
		_ = w.Store.RescheduleWork(ctx, "run", item.RefID, w.ID, 10*time.Second)
		return
	}
	if err == nil && settled.EvalRunID != nil {
		if evalCase, ok, err := w.Store.EvalCaseForRun(ctx, item.TenantID, item.RefID); err == nil && ok {
			passed, checks := ScoreCase(evalCase.Expectations, settled)
			encoded, _ := json.Marshal(checks)
			if err := w.Store.RecordEvalOutcome(ctx, item.TenantID, item.RefID, passed, encoded); err != nil {
				w.logf("record eval outcome %s: %v", item.RefID, err)
			}
		}
	}
	_ = w.Store.CompleteWork(ctx, "run", item.RefID, w.ID)
	w.maybePurge(ctx, item.TenantID)
}

// maybePurge enforces trace retention lazily, at most hourly per tenant.
func (w *Worker) maybePurge(ctx context.Context, tenantID string) {
	if w.Retention <= 0 {
		return
	}
	w.purgeMu.Lock()
	last := w.lastPurge[tenantID]
	if time.Since(last) < time.Hour {
		w.purgeMu.Unlock()
		return
	}
	w.lastPurge[tenantID] = time.Now()
	w.purgeMu.Unlock()
	if _, err := w.Store.PurgeRunEvents(ctx, tenantID, w.Retention); err != nil {
		w.logf("purge run events: %v", err)
	}
}

func (w *Worker) processOutbox(ctx context.Context, item postgres.WorkItem) {
	event, err := w.Store.GetOutboxEvent(ctx, item.TenantID, item.RefID)
	if err != nil || event.Status != "pending" {
		_ = w.Store.CompleteWork(ctx, "outbox", item.RefID, w.ID)
		return
	}
	if w.Dispatcher == nil || !w.Dispatcher.Configured() {
		_ = w.Store.MarkOutbox(ctx, item.TenantID, item.RefID, "pending", "flowcase_callback_not_configured")
		_ = w.Store.RescheduleWork(ctx, "outbox", item.RefID, w.ID, 5*time.Minute)
		return
	}
	outcome := w.Dispatcher.Deliver(ctx, event.Payload)
	switch {
	case outcome == "":
		_ = w.Store.MarkOutbox(ctx, item.TenantID, item.RefID, "delivered", "")
		_ = w.Store.CompleteWork(ctx, "outbox", item.RefID, w.ID)
	case event.Attempts+1 >= 10 || outcome == "flowcase_rejected":
		_ = w.Store.MarkOutbox(ctx, item.TenantID, item.RefID, "dead", outcome)
		_ = w.Store.CompleteWork(ctx, "outbox", item.RefID, w.ID)
	default:
		_ = w.Store.MarkOutbox(ctx, item.TenantID, item.RefID, "pending", outcome)
		delay := time.Duration(1<<min(event.Attempts, 8))*5*time.Second + time.Duration(rand.IntN(2000))*time.Millisecond
		_ = w.Store.RescheduleWork(ctx, "outbox", item.RefID, w.ID, delay)
	}
}
