# Auth boundary

## Inbound (browser → Control Plane → UCW)
- The browser holds no token. The Control Plane proxies `/apps/case-workspace/*` same-origin, strips the prefix and
  injects `Authorization: Bearer <ES256 JWT, aud=case-workspace>`.
- Every `/api/` request (any path, any method) is authenticated before routing (`internal/httpapi`):
  exactly one `Authorization: Bearer` header, ≤ 8 KiB; `alg` must be `ES256` (no `none`, no HS*/RS*); `kid` looked up
  in the CP JWKS (`UCW_CP_JWKS_URL`, cached 10 min, unknown kids refetch at most every 30 s, duplicate kids distrusted);
  exact `iss` (`UCW_CP_ISSUER`); `aud` contains `case-workspace`; `exp` and `iat` required, `nbf` honoured, skew 30 s;
  `exp − iat ≤ 15 min`; `principal_type = human`; `tenant_id = UCW_TENANT_KEY` (acme-demo); no `act` claim (exchanged
  tokens are not accepted inbound); `sub` must be a UUID. Failure → 401 `unauthenticated`; JWKS unreachable → 503.
- Identity/tenant/workspace come only from the verified claims. `X-User-ID`, `X-Tenant-ID`, `X-Role`, Tailscale headers,
  query strings and bodies are never read for identity. `scope` is shown as `platform_roles` and never grants anything.
- The verifier passes the tokencheck negative suite (unit test `internal/auth/verifier_test.go`): no token, forged
  identity headers, wrong audience (flowcase/kms/agent-studio), wrong issuer, expired, nbf in future, lifetime > 15 min,
  foreign key with the same kid, alg none, HS256 confusion, other tenant, service principal type.
- UCW has no user directory: any human CP user of the tenant reaches the UCW shell; what they can see is decided by
  Flowcase. tokencheck's "subject not provisioned" check does not apply (`-outsider-check=false`).

## Outbound to Flowcase (as the user)
- UCW POSTs the incoming token to `{UCW_CP_INTERNAL_URL}/api/v1/token-exchange` with HTTP Basic
  `case-workspace:<UCW_CP_CLIENT_SECRET_FILE>` and `{"subject_token", "audience": "flowcase"}`; the answer
  `{access_token, expires_in}` is cached per (user id, `sid`, audience) until 30 s before expiry.
- Exchange 401/403 (session ended) → 401 `session_inactive` for Flowcase-backed endpoints; `/me/context` still
  answers 200 with `flowcase.available=false` and a partial error. Exchange/network failure → 503
  `flowcase_unavailable` (or a partial error on composite reads). Flowcase rejecting the exchanged token → 403
  `flowcase_access_denied`.
- Flowcase applies its own authorization (case visibility, `case_owner` for creation, owner-only draft review,
  approvals with SoD). UCW checks a draft belongs to the case before forwarding and never fabricates a decision.
- A cached exchanged token stays usable until it expires (≤ its CP lifetime) even if the CP session ends meanwhile.

## Outbound to Agent Studio (service token + delegated user)
- UCW calls Agent Studio with its own opaque service token (`UCW_AGENT_STUDIO_TOKEN_FILE`), client `case-workspace`,
  permissions `run.invoke, run.read, run.cancel` only (Agent Studio stores only its SHA-256). A service client can
  read only runs it started.
- `delegated_user` = the caller's CP user id (token `sub`) — the same value Flowcase sends for workflow agents and the
  `tenant_principals.principal_id` KMS expects in `X-KMS-Delegated-User`. Agent id from the pack's `intake.agent_key`,
  `skill_version ""` (active release), idempotency key `ucw-intake-<session>:<turn>`, correlation id `intake:<session>`.
- Results are accepted only if the run's `agent_id` equals the agent the turn started, the output matches
  `output_checksum` (canonical JSON SHA-256) and is ≤ 256 KiB.

## What the browser never sees
CP tokens (inbound or exchanged), the token-exchange client secret, the Agent Studio service token, upstream URLs,
database credentials, raw upstream error bodies beyond Flowcase's own `{code, message, details}`.
Logs record method, route pattern, status, duration and request id — never tokens, headers, query strings or bodies;
audit metadata holds ids, counts and codes only (no message text or drafts).

## Other controls
`X-Request-ID` accepted (safe charset) or generated, propagated to Flowcase; no CORS headers; API responses
`Cache-Control: no-store`, `CSP default-src 'none'`; bodies ≤ 256 KiB, strict JSON (unknown fields → 400);
timeouts Flowcase 10 s, Agent Studio 15 s, exchange 5 s, JWKS 5 s; CSRF is handled by the CP proxy (same-origin,
cookie session → injected bearer).
