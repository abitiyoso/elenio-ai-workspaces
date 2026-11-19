# 9Router connectivity — 2026-11-19

The owner supplied and authorized `https://9router.elenio.id/v1`, model
`cx/gpt-6-luna`, OpenAI-compatible Chat Completions with Bearer authentication.
The real endpoint and the existing Agent Studio Eino executor both passed a
synthetic, tool-free diagnostic. This is connectivity evidence, not approval of
a model deployment, agent release or production execution.

## Credential handling

Only `NINE_ROUTER_API_KEY` was extracted from the owner's specified Prospecto
environment file. No other source variables were imported. The source was not
modified. The credential is stored in `ai_agent_studio/.env.local`, excluded by
`.env.*`, with inherited Windows ACLs disabled and one current-user access rule.
No key was printed, passed as a command argument, sent to a browser or committed.
An exact-value check found it absent from tracked files and the frontend build.

`scripts/Test-NineRouter.ps1` imports only that named variable from the local
server file into the diagnostic's process environment and restores the caller's
environment afterwards. The Go command removes the variable from its environment
after reading it, clears its credential buffer, and emits only sanitized metadata
or fixed error codes. The helper's credential resolver is limited to one exact
diagnostic provider scope; it is not installed in the API or worker.

## Reproduce

From the repository root, with the ignored server credential file provisioned:

```powershell
./scripts/Test-NineRouter.ps1 -GoExe /absolute/path/to/go.exe -Runtime
```

This makes billable synthetic requests: model catalog, a bounded JSON capability
probe, and one bounded Eino agent completion. Without `-Runtime`, only the existing
catalog/capability probe runs. The endpoint/model are deliberately fixed to the
owner-authorized destination. Do not paste a key into a frontend `.env` file.

The command uses the existing `NewOpenAICompatibleProbe`, public-DNS-pinned HTTPS
egress policy and `runtime.Executor`; no direct alternate provider integration was
introduced. Redirects/proxies and private-address egress remain disallowed. Input
contains only a synthetic diagnostic purpose; there are no tools, customer
documents or external mutations. Provider output bodies are not logged.

## Observed result

- Catalog and JSON capability: observed at the specified endpoint/model.
- Existing Eino executor: `succeeded`, schema valid, zero tool calls.
- Runtime usage: 299 input tokens / 9 output tokens. Probe usage is separate and
  is not included in those runtime counts. No monetary cost is inferred without
  an approved rate card.
- Adapter/runtime Go suites and two diagnostic credential boundary tests pass.
- Machine-readable safe record: [nine-router-verification.json](evidence/nine-router-verification.json).

## Remaining application integration

The key is staged in a server-only environment file and consumed by the test
launcher. It has **not** been imported into a running Agent Studio API/worker
vault or approved model registry. That service's FileVault still requires POSIX
0700 permissions and cannot start on this Windows host; that protection was not
bypassed or replaced. Its existing supported deployment environment must be used
for the vault, provider registration, capability evidence and approved deployment.

The general Architect blueprint authoring API also remains unimplemented. The
Architect screen captures and refines a local requirement, and explicitly states
that it does not call a model or change workflows. The prior blanket message
“Model not configured” was removed: provider connectivity is now proven, while
workspace authoring integration remains incomplete. Do not claim a model-backed
Architect → workflow → evaluation → release pilot from this diagnostic.
