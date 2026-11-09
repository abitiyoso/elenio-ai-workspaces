# Operations runbook

## Health and metrics
- `GET /healthz` liveness; `GET /readyz` → 503 if the database is down or migrations are pending; `idp: degraded`
  does not fail readiness (logins fail closed, sessions continue).
- Metrics (`METRICS_ADDRESS`): `cp_http_requests_total{route,code}`, `cp_http_request_duration_seconds`,
  `cp_auth_events_total{event,outcome}`, `cp_authz_decisions_total{effect,reason}`, `cp_active_sessions`,
  `cp_idp_up`, `cp_app_upstream_healthy{app}`, `cp_app_proxy_requests_total{app,outcome}`, `cp_rate_limited_total{bucket}`.
- Logs: JSON on stdout, one `http_request` line per request with `request_id` (also returned as `X-Request-ID`
  and stored as `correlation_id` in audit). Query strings are never logged; credential-like keys are redacted.

## Suggested alerts
| Alert | Expression (PromQL) | Action |
|---|---|---|
| IdP down | `cp_idp_up == 0` for 5m | new logins paused; check IdP / network |
| Login failures spike | `rate(cp_auth_events_total{event="callback",outcome!="success"}[10m]) > 0.2` | inspect `auth.login_failed` audit reason codes |
| Authz deny spike | `rate(cp_authz_decisions_total{effect="deny"}[10m])` 5× baseline | possible probing or broken grants |
| Upstream unhealthy | `cp_app_upstream_healthy == 0` for 5m | notify owning team |
| 5xx | `rate(cp_http_requests_total{code="5xx"}[5m]) > 0` | check logs by `request_id` |
| Rate limiting | `rate(cp_rate_limited_total[5m]) > 1` | abuse or mis-sized limits |

## Incident actions
- **Disable a user:** People → Suspend (or `POST /api/v1/admin/users/{id}/suspend`). Sessions are revoked in the
  same transaction; downstream tokens expire within `SERVICE_TOKEN_TTL` (5 min). Also disable the user at the IdP.
- **Kill all sessions of a user:** `POST /api/v1/admin/users/{id}/sessions/revoke`.
- **Kill every session (key compromise):** `UPDATE sessions SET revoked_at = now(), revoke_reason = 'incident' WHERE revoked_at IS NULL;`
- **Emergency app disable:** Registry → Disable (effective on the next request: catalog, launch and `/apps/<key>/*`).
  If the BFF itself is suspect, also set the app's upstream to `"mode":"pending"` and restart.

## Key rotation
- **Service token signing key:** generate a new key (`control-plane-api gen-signing-key`), put the current public
  JWKS (`curl /.well-known/jwks.json`) into `SERVICE_TOKEN_PREVIOUS_JWKS_FILE`, swap the signing key file, restart.
  Remove the previous JWKS after `SERVICE_TOKEN_TTL` + downstream JWKS cache TTL.
- **CSRF key:** rotate the file and restart; open tabs re-fetch `/me` and get a new token after one 403.
- **Token encryption key:** rotating makes stored refresh tokens unreadable (logout then skips IdP revocation for
  existing sessions); rotate together with a forced session revocation if compromise is suspected.
- **OIDC client secret:** regenerate in the IdP, replace `oidc_client_secret`, restart (re-render the realm file on the VM).

## Backups and restore
- `deploy/backup-vm.sh` dumps `control_plane` and `keycloak` (gzip, 0700 directory, keeps 14 runs). Schedule it
  daily via cron on the VM and copy off-host.
- Restore into a disposable database first, run `control-plane-api migrate`, check `/readyz`, then switch.
- Audit events are append-only (trigger); retention/archival is a policy decision (OPEN_DECISIONS).

## Maintenance
A background loop every minute probes the IdP, updates gauges and purges auth flows older than 1 h and sessions
7 days past absolute expiry. The outbox dispatcher delivers propagation events (currently to the log sink) with
exponential backoff and `SKIP LOCKED`, safe across replicas.
