# Agent Studio web

React, TypeScript, and Vite interface for the Agent Studio Phase 1 registry.

Run `npm install` and `npm run dev` from this directory. The Vite development
server proxies `/api` to `http://127.0.0.1:8190`. The host application must
provide the authenticated session and mount the Go registry handlers.

Set `AGENT_STUDIO_WEB_BASE_PATH` (for example `/apps/agent-studio/`, default
`/`) at build time to serve the UI under a reverse-proxy prefix; asset URLs and
API calls are then prefixed with it.

The interface reads real tenant-scoped provider, model, agent, and skill data.
Skills Studio supports draft skill and agent-version authoring and reads
existing bindings. Tool Library reads the global catalog and existing tenant
bindings; tool definitions are not editable here. Publishing and capability
binding controls remain disabled until the SME/AI Ops approval and delegation
policy is defined.

The interface has no browser demo fixtures, credential inputs, connection
probes, approval toggles, or local storage. Until the host session and API are
available, the app explains the unavailable state and does not invent tenant
records.
