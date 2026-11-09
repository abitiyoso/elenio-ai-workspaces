#!/usr/bin/env bash
# Deploy the Control-Plane-integrated Flowcase instance (compose project `flowcase-cp`) to the VM.
# Usage: deploy/flowcase-cp/deploy.sh [user@host]   Requires the control-plane stack to be deployed first.
set -euo pipefail
VM="${1:-kibi-vm@100.126.146.52}"
HERE="$(cd "$(dirname "$0")" && pwd)"
FLOWCASE_SRC="${FLOWCASE_SRC:-$HERE/../../../flowcase_platform}"
RELEASE="$(date -u +%Y%m%dT%H%M%SZ)"

echo "==> packaging flowcase source from $FLOWCASE_SRC"
ssh "$VM" "mkdir -p ~/control-plane/flowcase-cp/releases/$RELEASE/flowcase-src"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$FLOWCASE_SRC" -czf - --exclude='./web/node_modules' --exclude='./web/dist' --exclude='./.env' --exclude='./.claude' \
  --exclude='*.png' --exclude='./.DS_Store' . \
  | ssh "$VM" "tar -xzf - -C ~/control-plane/flowcase-cp/releases/$RELEASE/flowcase-src"
rsync -a "$HERE/compose.yaml" "$HERE/directory.json" "$HERE/provision.py" "$HERE/nginx.cp.conf" "$VM:control-plane/flowcase-cp/releases/$RELEASE/"

ssh "$VM" RELEASE="$RELEASE" 'bash -s' <<'REMOTE'
set -euo pipefail
BASE="$HOME/control-plane/flowcase-cp"
REL="$BASE/releases/$RELEASE"
# Only CP_PUBLIC_URL: exporting the whole CP .env would override this app's POSTGRES_PASSWORD in compose.
CP_PUBLIC_URL="$(sed -n 's/^CP_PUBLIC_URL=//p' "$HOME/control-plane/.env")"
if [ ! -f "$BASE/.env" ]; then
  umask 077
  printf 'POSTGRES_PASSWORD=%s\nAGENT_CALLBACK_SECRET=%s\nCP_PUBLIC_URL=%s\n' "$(openssl rand -hex 24)" "$(openssl rand -hex 32)" "$CP_PUBLIC_URL" > "$BASE/.env"
fi
# Service token Flowcase uses towards Agent Studio (Agent Studio stores only its SHA-256; see agent-studio-cp/deploy.sh).
mkdir -p "$BASE/secrets" && chmod 700 "$BASE/secrets"
[ -s "$BASE/secrets/agent-studio-service-token" ] || (umask 077; openssl rand -hex 32 | tr -d '\n' > "$BASE/secrets/agent-studio-service-token")
grep -q '^FC_SECRETS_DIR=' "$BASE/.env" || echo "FC_SECRETS_DIR=$BASE/secrets" >> "$BASE/.env"
# The distroless image runs as uid 65532; the 0700 directory keeps the file private on the host.
chmod 644 "$BASE/secrets/agent-studio-service-token"
cd "$REL"
export FC_RELEASE="$RELEASE"
DC="docker compose --env-file $BASE/.env -f compose.yaml"
$DC build </dev/null
$DC up -d postgres </dev/null   # recreated with the loopback port when its definition changes
$DC run --rm migrate </dev/null
if [ ! -f "$BASE/.packs-bootstrapped" ]; then
  echo "==> first deploy: importing SIM/PoC packs, then binding the tenant to the Control Plane tenant key"
  $DC --profile bootstrap run --rm bootstrap-packs </dev/null
  docker exec -i flowcase-cp-postgres-1 psql -v ON_ERROR_STOP=1 -U flowcase -d flowcase -q </dev/null -c \
    "UPDATE tenants SET external_ref = 'acme-demo', name = 'Example Enterprise' WHERE id = '00000000-0000-4000-8000-000000000001'; UPDATE users SET status = 'disabled' WHERE issuer = 'flowcase-dev-mock';"
  touch "$BASE/.packs-bootstrapped"
fi
$DC up -d --remove-orphans --force-recreate api worker web </dev/null
python3 provision.py directory.json "$CP_PUBLIC_URL" </dev/null
ln -sfn "$REL" "$BASE/current"
for i in $(seq 1 30); do curl -sf http://127.0.0.1:8280/readyz >/dev/null && break; sleep 2; done
echo "api readyz: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8280/readyz)  web: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:5280/)"
ls -1dt "$BASE"/releases/* | tail -n +4 | xargs -r rm -rf
REMOTE
echo "==> flowcase-cp $RELEASE deployed"
