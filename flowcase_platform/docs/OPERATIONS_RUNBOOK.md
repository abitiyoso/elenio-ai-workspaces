# Operations Runbook

## Components

| Process | Role | Probes | Metrics |
|---|---|---|---|
| `api` | REST API, auth, registry, case commands | `GET /healthz`, `GET /readyz` (postgres, schema, host JWKS) | `METRICS_ADDRESS` listener (internal only) `/` |
| `worker` | executes steps, polls agents, fires timers, reconciles evidence (1/min), dispatches outbox | `WORKER_HEALTH_ADDRESS` `/healthz`, `/readyz` (a tick succeeded within 3 intervals) | same listener `/metrics` |
| `migrate` | `up`, `down N`, `status`, `seed-dev` (development only) | — | — |
| PostgreSQL | system of record | `pg_isready` | provider metrics |

Run at least two workers for availability; they coordinate through `FOR UPDATE SKIP LOCKED` leases, not a leader.

## Key metrics and alerts (suggested thresholds)

| Metric | Alert |
|---|---|
| `flowcase_oldest_ready_execution_seconds` | > 60 s for 5 min — workers down, stuck or overloaded |
| `flowcase_node_executions{status="failed"}` | increase — a case failed after retries; inspect `last_error` |
| `flowcase_outbox_events{status="dead"}` | > 0 — an integration consumer rejected events 8 times |
| `flowcase_agent_runs{status="started"}` | growing for > agent timeout — runtime not answering |
| `flowcase_approvals{status="rereview"}` | informational — changes re-opened reviews |
| `flowcase_stale_artifact_versions` | informational — revisions needed before gates pass |
| `flowcase_http_requests_total{code="5xx"}` | rate > 1% |
| `flowcase_worker_errors_total` | increase — DB connectivity or schema drift |

## Routine operations

- **Deploy:** run `migrate up` (advisory-locked, safe to run from several places), then roll the API, then the
  workers. Readiness fails if the schema is behind the binary, so traffic only reaches compatible instances.
- **Publish a workflow version:** in the UI by a human `workflow_publisher`; running cases stay on their version.
- **Change pack bindings** (agent runtime ids, skill versions, per-policy SoD, ratifications): edit the mounted
  `PACKS_CONFIG` file and restart API and worker. Invalid files fail startup with a `PACKS_CONFIG …` message.
  Only set a `ratification` with the real decision reference; otherwise the policy stays provisional.
- **Pause a case:** case owner or tenant admin in the UI (or `POST /cases/{id}/pause`). Paused cases are skipped.

## Incident playbooks

**Queue lag rising.** Check worker `/readyz` and logs (`worker tick failed`). Typical causes: DB unavailable,
schema behind (`migrate status`), or a slow external adapter. Scale workers horizontally.

**A case failed.** Open the case; the timeline shows the failed step with `last_error`. Fix the cause (adapter,
data), then start a new case or (future) use a supervised retry. Do not edit rows to "un-fail" a case.

**Dead-lettered outbox events.** Inspect `SELECT id, topic, last_error FROM outbox_events WHERE status = 'dead'`.
After fixing the consumer, requeue explicitly and record why:
```sql
UPDATE outbox_events SET status = 'pending', attempts = 0, available_at = now(), last_error = NULL
WHERE status = 'dead' AND id = '<event id>';
```

**Agent runtime outage.** Runs stay `started`; the engine polls until the agent timeout (default 1 h), then
retries with a fresh run and finally fails the case. Callbacks arriving after a run was settled are ignored.
If the outage is long, pause affected cases.

**KMS evidence withdrawn.** Expected behaviour: the citing artifact becomes stale, downstream artifacts too, and
affected approvals are invalidated with re-reviews. Gates refuse approval until revised and re-reviewed.

**Suspected credential leak (callback secret).** Rotate `AGENT_CALLBACK_SECRET` in the secret manager and in the
AI runtime, restart the API. Old signatures stop verifying immediately; replays are already de-duplicated.

**Authentication failures spike.** Check host JWKS reachability (`/readyz` `host_jwks`), clock skew, and the
issuer/audience configuration. Never switch to `dev_mock` outside development — the process refuses to start.

## Data protection

- Logs never contain tokens or request bodies; sensitive attribute keys are redacted.
- Audit events, approval decisions, condition evaluations and published versions are append-only at the database level.
- Connector parameters are stored redacted; agent inputs are not stored (only their hash).

See `BACKUP_RECOVERY.md` for backups and `DEPLOYMENT.md` for the production checklist.
