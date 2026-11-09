#!/usr/bin/env bash
# Deploy the Control-Plane-integrated Agent Studio instance (compose project `agent-studio-cp`) to the VM.
# Usage: deploy/agent-studio-cp/deploy.sh [user@host]   Requires the control-plane stack to be deployed first.
set -euo pipefail
VM="${1:-kibi-vm@100.126.146.52}"
HERE="$(cd "$(dirname "$0")" && pwd)"
AS_SRC="${AS_SRC:-$HERE/../../../ai_agent_studio}"
RELEASE="$(date -u +%Y%m%dT%H%M%SZ)"

echo "==> packaging Agent Studio source from $AS_SRC"
ssh "$VM" "mkdir -p ~/control-plane/agent-studio-cp/releases/$RELEASE/agent-studio-src"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$AS_SRC" -czf - --exclude='./apps/agent-studio-web/node_modules' --exclude='./apps/agent-studio-web/dist' \
  --exclude='./.env' --exclude='./.claude' --exclude='./.DS_Store' --exclude='*.png' --exclude='./kibi-vm@*' \
  --exclude='./deploy/access-map.json' --exclude='./previews' . \
  | ssh "$VM" "tar -xzf - -C ~/control-plane/agent-studio-cp/releases/$RELEASE/agent-studio-src"
rsync -a "$HERE/compose.yaml" "$HERE/directory.json" "$HERE/render-access-map.py" "$VM:control-plane/agent-studio-cp/releases/$RELEASE/"
rsync -a "$HERE/Dockerfile.cp" "$VM:control-plane/agent-studio-cp/releases/$RELEASE/agent-studio-src/Dockerfile.cp"

ssh "$VM" RELEASE="$RELEASE" 'bash -s' <<'REMOTE'
set -euo pipefail
BASE="$HOME/control-plane/agent-studio-cp"
REL="$BASE/releases/$RELEASE"
SECRETS="$BASE/secrets"
set -a; . "$HOME/control-plane/.env"; set +a      # CP_PUBLIC_URL
if [ ! -f "$BASE/.env" ]; then
  umask 077
  mkdir -p "$SECRETS" "$BASE/vault" && chmod 700 "$SECRETS" "$BASE/vault"
  openssl rand -hex 24 > "$SECRETS/db-owner-password"
  openssl rand -hex 24 > "$SECRETS/db-app-password"
  head -c 32 /dev/urandom > "$SECRETS/vault-master-key"
  printf 'CP_PUBLIC_URL=%s\nAS_TENANT_ID=%s\nAS_SECRETS_DIR=%s\nAS_VAULT_DIR=%s\nAS_UID=%s\nAS_GID=%s\n' \
    "$CP_PUBLIC_URL" "$(cat /proc/sys/kernel/random/uuid)" "$SECRETS" "$BASE/vault" "$(id -u)" "$(id -g)" > "$BASE/.env"
fi
# Same tenant UUID as Flowcase: Flowcase only accepts callbacks whose tenant_id matches its own case tenant.
FC_TENANT="$(docker exec flowcase-cp-postgres-1 psql -U flowcase -d flowcase -Atc "SELECT id FROM tenants WHERE external_ref = 'acme-demo'" </dev/null)"
if [ -n "$FC_TENANT" ]; then sed -i "s/^AS_TENANT_ID=.*/AS_TENANT_ID=$FC_TENANT/" "$BASE/.env"; fi
set -a; . "$BASE/.env"; set +a
umask 077
# Flowcase and Case Workspace -> Agent Studio service tokens (only their SHA-256 is stored here).
FC_TOKEN_FILE="$HOME/control-plane/flowcase-cp/secrets/agent-studio-service-token"
UCW_TOKEN_FILE="$HOME/control-plane/case-workspace-cp/secrets/agent-studio-service-token"
python3 - "$FC_TOKEN_FILE" "$UCW_TOKEN_FILE" "$AS_TENANT_ID" "$SECRETS/service-tokens.json" <<'PY'
import datetime, hashlib, json, os, sys
fc_token, ucw_token, tenant, out = sys.argv[1:5]
grants = []
expires = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=180)).strftime("%Y-%m-%dT%H:%M:%SZ")
for client_id, token_file in (("flowcase", fc_token), ("case-workspace", ucw_token)):
    if os.path.exists(token_file):
        digest = hashlib.sha256(open(token_file, "rb").read().strip()).hexdigest()
        grants.append({"client_id": client_id, "token_sha256": digest, "tenant_id": tenant,
                       "permissions": ["run.invoke", "run.read", "run.cancel"], "expires_at": expires})
json.dump(grants, open(out, "w"))
print(f"service tokens: {len(grants)}")
PY
# Callback secret shared with flowcase-cp (byte-identical, no trailing newline).
grep -E '^AGENT_CALLBACK_SECRET=' "$HOME/control-plane/flowcase-cp/.env" | cut -d= -f2- | tr -d '\n' > "$SECRETS/flowcase-callback-secret"
# KMS retrieval credential with delegation scope, granted only the seeded knowledge collection.
if [ ! -s "$SECRETS/kms-service-token" ]; then
  set -a; . "$HOME/control-plane/kms-cp/.env"; set +a
  CID="$(docker exec kms-cp-postgres-1 psql -U kms -d kms -Atc "SELECT id FROM collections WHERE name = 'Telkom Digital DPI & Costing'" </dev/null)"
  ACTOR="$(docker exec control-plane-postgres-1 psql -U control_plane -d control_plane -Atc "SELECT id FROM users WHERE idp_subject = '7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c03'" </dev/null)"
  if [ -n "$CID" ] && [ -n "$ACTOR" ]; then
    docker exec kms-cp-api-1 /kms-service-client create --tenant-id "$KMS_TENANT_ID" --actor-id "$ACTOR" --name agent-studio-retrieval \
      --expires-at "$(date -u -d '+180 days' +%Y-%m-%dT%H:%M:%SZ)" --grant "$CID" --delegate </dev/null | sed -n 's/^token=//p' | tr -d '\n' > "$SECRETS/kms-service-token"
    echo "kms service client created (delegate scope, collection ${CID:0:8})"
  fi
  [ -s "$SECRETS/kms-service-token" ] || echo "placeholder-kms-token-not-provisioned-0000000000" > "$SECRETS/kms-service-token"
fi
python3 "$REL/render-access-map.py" "$REL/directory.json" "$AS_TENANT_ID" "$SECRETS/access-map.json" </dev/null
chmod 600 "$SECRETS"/*
# The postgres image reads POSTGRES_PASSWORD_FILE after dropping to uid 70; the 0700 directory still protects it on the host.
chmod 644 "$SECRETS/db-owner-password"
cd "$REL"
export AS_RELEASE="$RELEASE"
DC="docker compose --env-file $BASE/.env -f compose.yaml"
$DC build </dev/null
$DC up -d --remove-orphans --force-recreate app </dev/null
ln -sfn "$REL" "$BASE/current"
for i in $(seq 1 40); do curl -sf http://127.0.0.1:8290/health/ready >/dev/null && break; sleep 3; done
echo "ready: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8290/health/ready)  ui: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8290/)"
ls -1dt "$BASE"/releases/* | tail -n +4 | xargs -r rm -rf
REMOTE
echo "==> agent-studio-cp $RELEASE deployed"
