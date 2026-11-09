#!/usr/bin/env bash
# Deploy the Control-Plane-integrated KMS instance (compose project `kms-cp`) to the VM.
# Usage: deploy/kms-cp/deploy.sh [user@host]   Requires the control-plane stack to be deployed first.
set -euo pipefail
VM="${1:-kibi-vm@100.126.146.52}"
HERE="$(cd "$(dirname "$0")" && pwd)"
KMS_SRC="${KMS_SRC:-$HERE/../../../kms_go_package}"
RELEASE="$(date -u +%Y%m%dT%H%M%SZ)"

echo "==> packaging KMS source from $KMS_SRC"
ssh "$VM" "mkdir -p ~/control-plane/kms-cp/releases/$RELEASE/kms-src"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$KMS_SRC" -czf - --exclude='./frontend/node_modules' --exclude='./frontend/dist' \
  --exclude='./backend/.env' --exclude='./.env' --exclude='./.claude' --exclude='./.DS_Store' --exclude='*.png' . \
  | ssh "$VM" "tar -xzf - -C ~/control-plane/kms-cp/releases/$RELEASE/kms-src"
rsync -a "$HERE/compose.yaml" "$HERE/directory.json" "$HERE/provision.py" "$VM:control-plane/kms-cp/releases/$RELEASE/"
rsync -a "$HERE/web.Dockerfile" "$VM:control-plane/kms-cp/releases/$RELEASE/kms-src/frontend/Dockerfile.cp"
rsync -a "$HERE/nginx.cp.conf" "$VM:control-plane/kms-cp/releases/$RELEASE/kms-src/frontend/nginx.cp.conf"

ssh "$VM" RELEASE="$RELEASE" 'bash -s' <<'REMOTE'
set -euo pipefail
BASE="$HOME/control-plane/kms-cp"
REL="$BASE/releases/$RELEASE"
set -a; . "$HOME/control-plane/.env"; set +a      # CP_PUBLIC_URL
if [ ! -f "$BASE/.env" ]; then
  umask 077
  # The shared Docling service key is reused from the existing KMS deployment without printing it.
  DOCLING_KEY="$(grep -E '^DOCLING_API_KEY=' "$HOME/kms-phase1/app/.env" 2>/dev/null | head -1 | cut -d= -f2- || true)"
  printf 'POSTGRES_PASSWORD=%s\nS3_SECRET_KEY=%s\nKMS_TENANT_ID=%s\nCP_PUBLIC_URL=%s\nDOCLING_API_KEY=%s\n' \
    "$(openssl rand -hex 24)" "$(openssl rand -hex 24)" "$(cat /proc/sys/kernel/random/uuid)" "$CP_PUBLIC_URL" "$DOCLING_KEY" > "$BASE/.env"
fi
set -a; . "$BASE/.env"; set +a
cd "$REL"
export KMS_RELEASE="$RELEASE"
DC="docker compose --env-file $BASE/.env -f compose.yaml"
$DC build </dev/null
$DC up -d --remove-orphans </dev/null
for i in $(seq 1 60); do curl -sf http://127.0.0.1:8380/healthz >/dev/null && break; sleep 3; done
python3 provision.py directory.json "$KMS_TENANT_ID" </dev/null
ln -sfn "$REL" "$BASE/current"
echo "api healthz: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8380/healthz)  readyz: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:8380/readyz)  web: $(curl -s -o /dev/null -w %{http_code} http://127.0.0.1:5380/)"
ls -1dt "$BASE"/releases/* | tail -n +4 | xargs -r rm -rf
REMOTE
echo "==> kms-cp $RELEASE deployed"
