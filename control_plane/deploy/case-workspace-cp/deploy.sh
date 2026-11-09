#!/usr/bin/env bash
# Deploy the Universal Case Workspace (compose project `case-workspace-cp`) to the VM.
# Usage: deploy/case-workspace-cp/deploy.sh [user@host]
# Requires: the control-plane stack (with the case-workspace token-exchange client secret in
# ~/control-plane/secrets/token_exchange_case_workspace), flowcase-cp and agent-studio-cp already deployed;
# kms-cp's .env provides the docling-serve API key for intake document uploads.
set -euo pipefail
VM="${1:-kibi-vm@100.126.146.52}"
HERE="$(cd "$(dirname "$0")" && pwd)"
UCW_SRC="${UCW_SRC:-$HERE/../../../universal_case_workspace}"
RELEASE="$(date -u +%Y%m%dT%H%M%SZ)"

echo "==> packaging case workspace source from $UCW_SRC"
ssh "$VM" "mkdir -p ~/control-plane/case-workspace-cp/releases/$RELEASE/case-workspace-src"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$UCW_SRC" -czf - --exclude='./web/node_modules' --exclude='./web/dist' --exclude='./.env' --exclude='./.claude' \
  --exclude='./.DS_Store' --exclude='*.png' --exclude='./previews' --exclude='./prototype_source' --exclude='./Universal_Case_Workspace_Prototype.html' . \
  | ssh "$VM" "tar -xzf - -C ~/control-plane/case-workspace-cp/releases/$RELEASE/case-workspace-src"
rsync -a "$HERE/compose.yaml" "$VM:control-plane/case-workspace-cp/releases/$RELEASE/"
rsync -a "$HERE/Dockerfile" "$VM:control-plane/case-workspace-cp/releases/$RELEASE/case-workspace-src/Dockerfile.cp"

ssh "$VM" RELEASE="$RELEASE" 'bash -s' <<'REMOTE'
set -euo pipefail
BASE="$HOME/control-plane/case-workspace-cp"
REL="$BASE/releases/$RELEASE"
SECRETS="$BASE/secrets"
AS_BASE="$HOME/control-plane/agent-studio-cp"
# Read only what UCW needs: exporting the whole CP .env would leak its POSTGRES_PASSWORD into compose, which
# prefers shell variables over --env-file (that once initialized UCW's database with the CP's password).
cpenv() { sed -n "s/^$1=//p" "$HOME/control-plane/.env"; }
CP_PUBLIC_URL="$(cpenv CP_PUBLIC_URL)"
CP_INTERNAL_URL="http://$(cpenv CP_BIND_IP):$(cpenv CP_PORT)"   # token exchange on the host network
umask 077
mkdir -p "$SECRETS" && chmod 700 "$SECRETS"
if [ ! -f "$BASE/.env" ]; then
  printf 'POSTGRES_PASSWORD=%s\nCP_PUBLIC_URL=%s\nCP_INTERNAL_URL=%s\nUCW_SECRETS_DIR=%s\n' \
    "$(openssl rand -hex 24)" "$CP_PUBLIC_URL" "$CP_INTERNAL_URL" "$SECRETS" > "$BASE/.env"
fi

# Control Plane token-exchange client secret (created by the Control Plane deploy; never printed).
CP_SECRET="$HOME/control-plane/secrets/token_exchange_case_workspace"
if [ ! -s "$CP_SECRET" ]; then
  echo "missing $CP_SECRET: deploy the Control Plane with the case-workspace token-exchange client first" >&2
  exit 1
fi
cp "$CP_SECRET" "$SECRETS/token-exchange-client-secret"

# UCW -> Agent Studio service token. Agent Studio stores only its SHA-256 (client "case-workspace",
# run.invoke/read/cancel); agent-studio-cp/deploy.sh keeps the grant on later Agent Studio deploys.
[ -s "$SECRETS/agent-studio-service-token" ] || openssl rand -hex 32 | tr -d '\n' > "$SECRETS/agent-studio-service-token"
# docling-serve API key, shared with kms-cp (never printed). Without one, PDF/DOCX uploads fail and TXT/MD still work.
DOCLING_KEY="$(grep -E '^DOCLING_API_KEY=' "$HOME/control-plane/kms-cp/.env" 2>/dev/null | head -1 | cut -d= -f2- || true)"
if [ -n "$DOCLING_KEY" ]; then
  printf '%s' "$DOCLING_KEY" > "$SECRETS/docling-api-key"
else
  echo "WARNING: no DOCLING_API_KEY in kms-cp/.env; intake PDF/DOCX uploads will fail" >&2
  printf 'unset' > "$SECRETS/docling-api-key"
fi
unset DOCLING_KEY
# The distroless image runs as uid 65532; the 0700 directory keeps the files private on the host.
chmod 644 "$SECRETS/token-exchange-client-secret" "$SECRETS/agent-studio-service-token" "$SECRETS/docling-api-key"

if [ -f "$AS_BASE/.env" ] && [ -f "$AS_BASE/secrets/service-tokens.json" ]; then
  AS_TENANT_ID="$(grep -E '^AS_TENANT_ID=' "$AS_BASE/.env" | cut -d= -f2-)"
  CHANGED="$(python3 - "$SECRETS/agent-studio-service-token" "$AS_TENANT_ID" "$AS_BASE/secrets/service-tokens.json" <<'PY'
import datetime, hashlib, json, os, sys
token_file, tenant, path = sys.argv[1:4]
digest = hashlib.sha256(open(token_file, "rb").read().strip()).hexdigest()
grants = json.load(open(path))
current = [g for g in grants if g.get("client_id") == "case-workspace"]
if current and current[0].get("token_sha256") == digest and current[0].get("tenant_id") == tenant:
    print("unchanged")
    sys.exit(0)
expires = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=180)).strftime("%Y-%m-%dT%H:%M:%SZ")
grants = [g for g in grants if g.get("client_id") != "case-workspace"]
grants.append({"client_id": "case-workspace", "token_sha256": digest, "tenant_id": tenant,
               "permissions": ["run.invoke", "run.read", "run.cancel"], "expires_at": expires})
tmp = path + ".tmp"
with open(tmp, "w") as f:
    json.dump(grants, f)
os.chmod(tmp, 0o600)
os.replace(tmp, path)
print("changed")
PY
)"
  if [ "$CHANGED" = "changed" ]; then
    echo "==> registered the case-workspace service client with Agent Studio; restarting agent-studio-cp app"
    docker restart agent-studio-cp-app-1 </dev/null >/dev/null
  fi
else
  echo "WARNING: agent-studio-cp not deployed; intake chat answers 503 until Agent Studio knows the case-workspace client" >&2
fi

cd "$REL"
export UCW_RELEASE="$RELEASE"
DC="docker compose --env-file $BASE/.env -f compose.yaml"
$DC build </dev/null
$DC up -d postgres </dev/null
$DC up -d --remove-orphans --force-recreate app </dev/null
ln -sfn "$REL" "$BASE/current"
for i in $(seq 1 40); do curl -sf http://127.0.0.1:8480/readyz >/dev/null && break; sleep 2; done
echo "readyz: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8480/readyz)  healthz: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8480/healthz)  spa: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8480/)"
ls -1dt "$BASE"/releases/* | tail -n +4 | xargs -r rm -rf
REMOTE
echo "==> case-workspace-cp $RELEASE deployed"
