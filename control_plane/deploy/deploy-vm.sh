#!/usr/bin/env bash
# Deploy the Control Plane (API + Keycloak + PostgreSQL) to the tailnet VM as its own Compose project.
# Usage: deploy/deploy-vm.sh [user@host]   (default kibi-vm@100.126.146.52)
# Builds locally (linux/amd64 binary + web/dist), ships a release folder, keeps private config and secrets
# in ~/control-plane/{.env,secrets,config} on the VM (generated on first deploy, never in the repository).
# A first deploy serves the tailnet origin http://$BIND_IP:5200. kibi-vm now runs behind the Cloudflare tunnel:
# ~/control-plane/.env has CP_BIND_IP=127.0.0.1, CP_PUBLIC_URL=https://orchestra.kibicode.my.id and
# KEYCLOAK_PUBLIC_URL=https://auth.kibicode.my.id (tunnel -> localhost:5200 / :5201); later deploys keep that .env.
set -euo pipefail

VM="${1:-kibi-vm@100.126.146.52}"
BIND_IP="${CP_BIND_IP:-100.126.146.52}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE="$(date -u +%Y%m%dT%H%M%SZ)"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "==> building release $RELEASE"
(cd "$ROOT/web" && npm ci --silent && npm run build >/dev/null)
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$STAGE/build/control-plane-api" ./cmd/control-plane-api)
cp -R "$ROOT/web/dist" "$STAGE/build/web"
cp "$ROOT/deploy/Dockerfile.runtime" "$ROOT/deploy/compose.vm.yaml" "$ROOT/deploy/dev-seed.json" "$ROOT/deploy/upstreams.vm.json" "$STAGE/"
cp -R "$ROOT/deploy/postgres-init" "$ROOT/deploy/keycloak" "$STAGE/"

echo "==> shipping to $VM"
ssh "$VM" "mkdir -p ~/control-plane/releases/$RELEASE"
rsync -a "$STAGE/" "$VM:control-plane/releases/$RELEASE/"

ssh "$VM" BIND_IP="$BIND_IP" RELEASE="$RELEASE" 'bash -s' <<'REMOTE'
set -euo pipefail
BASE="$HOME/control-plane"
REL="$BASE/releases/$RELEASE"
SECRETS="$BASE/secrets"
CONFIG="$BASE/config"
mkdir -p "$CONFIG"
umask 077
rand() { openssl rand -hex "$1"; }

if [ ! -f "$BASE/.env" ]; then
  echo "==> first deploy: generating private configuration and secrets"
  mkdir -p "$SECRETS" && chmod 700 "$SECRETS"
  PGPW="$(rand 24)"; KCDBPW="$(rand 24)"; KCADMIN="$(rand 18)"
  rand 32 > "$SECRETS/oidc_client_secret"
  rand 32 > "$SECRETS/csrf_hmac_key"
  rand 32 > "$SECRETS/token_encryption_key"
  printf 'postgres://control_plane:%s@127.0.0.1:55440/control_plane?sslmode=disable' "$PGPW" > "$SECRETS/database_url"
  printf '%s' "$KCADMIN" > "$SECRETS/keycloak_admin_password"
  cat > "$BASE/.env" <<ENV
POSTGRES_PASSWORD=$PGPW
KEYCLOAK_DB_PASSWORD=$KCDBPW
KEYCLOAK_ADMIN_PASSWORD=$KCADMIN
CP_BIND_IP=$BIND_IP
CP_PORT=5200
KEYCLOAK_PORT=5201
CP_DB_PORT=55440
CP_METRICS_PORT=9201
CP_PUBLIC_URL=http://$BIND_IP:5200
KEYCLOAK_PUBLIC_URL=http://$BIND_IP:5201
CP_SECRETS_DIR=$SECRETS
CP_CONFIG_DIR=$CONFIG
CP_UID=$(id -u)
CP_GID=$(id -g)
ENV
  chmod 600 "$BASE/.env"
fi
set -a; . "$BASE/.env"; set +a
# Trusted upstreams: first deploy copies the shipped file; later deploys only ADD app keys that are missing
# (existing entries, including operator edits, are never overwritten).
python3 - "$REL/upstreams.vm.json" "$CONFIG/upstreams.json" <<'PY'
import json, os, sys
shipped_path, live_path = sys.argv[1], sys.argv[2]
shipped = json.load(open(shipped_path))
live = json.load(open(live_path)) if os.path.exists(live_path) else {}
added = [k for k in shipped if k not in live]
for k in added:
    live[k] = shipped[k]
if added or not os.path.exists(live_path):
    tmp = live_path + ".tmp"
    with open(tmp, "w") as f:
        json.dump(live, f, indent=2)
        f.write("\n")
    os.replace(tmp, live_path)
    print("==> trusted upstreams added: " + (", ".join(added) or "(initial copy)"))
PY

# Delegated token exchange (ADR-004): one client secret per experience app, generated once. The plaintext stays in
# $SECRETS (0600) for that app's own deploy to copy; the CP only receives its SHA-256 via TOKEN_EXCHANGE_CLIENTS.
#   case-workspace -> ~/control-plane/secrets/token_exchange_case_workspace (read by deploy/case-workspace-cp)
if [ ! -s "$SECRETS/token_exchange_case_workspace" ]; then
  printf '%s' "$(rand 32)" > "$SECRETS/token_exchange_case_workspace"
fi
python3 - "$SECRETS/token_exchange_case_workspace" "$CONFIG/token_exchange_clients.json" <<'PY'
import hashlib, json, os, sys
secret = open(sys.argv[1]).read().strip()
assert len(secret) >= 32, "token exchange client secret too short"
clients = {"case-workspace": {"secret_sha256": hashlib.sha256(secret.encode()).hexdigest(), "audiences": ["flowcase", "kms"]}}
tmp = sys.argv[2] + ".tmp"
with open(tmp, "w") as f:
    json.dump(clients, f, indent=2)
    f.write("\n")
os.replace(tmp, sys.argv[2])
PY
python3 "$REL/keycloak/render-realm.py" "$REL/keycloak/realm-workspace.template.json" "$SECRETS/oidc_client_secret" \
  "$CP_PUBLIC_URL" none "$SECRETS/realm-workspace.json"
chmod 644 "$SECRETS/realm-workspace.json" # read by the keycloak container user (secrets dir itself stays 0700)
chmod 711 "$SECRETS"

# Signing key: generated once with the shipped binary (ES256, PKCS#8).
if [ ! -s "$SECRETS/service_token_signing_key" ]; then
  "$REL/build/control-plane-api" gen-signing-key > "$SECRETS/service_token_signing_key"
fi
chmod 600 "$SECRETS"/* ; chmod 644 "$SECRETS/realm-workspace.json"

cd "$REL"
export CP_RELEASE="$RELEASE"
docker compose --env-file "$BASE/.env" -f compose.vm.yaml up -d --build --remove-orphans </dev/null
ln -sfn "$REL" "$BASE/current"

echo "==> waiting for Keycloak"
for i in $(seq 1 60); do
  curl -sf "$KEYCLOAK_PUBLIC_URL/realms/workspace/.well-known/openid-configuration" >/dev/null && break
  sleep 5
done
if [ ! -f "$SECRETS/demo-users.txt" ]; then
  echo "==> setting demo user passwords (stored only in $SECRETS/demo-users.txt)"
  KC="docker compose --env-file $BASE/.env -f compose.vm.yaml exec -T keycloak /opt/keycloak/bin/kcadm.sh"
  $KC config credentials --server http://localhost:8080 --realm master --user kc-bootstrap-admin --password "$KEYCLOAK_ADMIN_PASSWORD" >/dev/null </dev/null
  : > "$SECRETS/demo-users.txt"
  for u in rafi nadia maya dimas sari demo; do
    pw="$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-16)"
    $KC set-password -r workspace --username "$u" --new-password "$pw" </dev/null
    echo "$u $pw" >> "$SECRETS/demo-users.txt"
  done
  chmod 600 "$SECRETS/demo-users.txt"
fi
docker compose --env-file "$BASE/.env" -f compose.vm.yaml restart api >/dev/null </dev/null

echo "==> smoke"
for i in $(seq 1 30); do curl -sf "$CP_PUBLIC_URL/readyz" >/dev/null && break; sleep 2; done
curl -s "$CP_PUBLIC_URL/readyz"; echo
curl -s -o /dev/null -w "login redirect: %{http_code}\n" "$CP_PUBLIC_URL/auth/login"
curl -s -o /dev/null -w "me without session: %{http_code}\n" "$CP_PUBLIC_URL/api/v1/me"
ls -1dt "$BASE"/releases/* | tail -n +6 | xargs -r rm -rf   # keep the last 5 releases
REMOTE
echo "==> deployed $RELEASE (public URL = CP_PUBLIC_URL in ~/control-plane/.env on the VM)"
