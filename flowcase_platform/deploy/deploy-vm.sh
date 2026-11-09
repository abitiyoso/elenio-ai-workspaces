#!/usr/bin/env bash
# Ship the working tree to the VM and (re)start the tailnet-only stack.
#   deploy/deploy-vm.sh [user@host]      default kibi-vm@100.126.146.52
# The VM keeps its own .env (generated on first deploy, never copied from this machine).
set -euo pipefail
HOST="${1:-kibi-vm@100.126.146.52}"
BIND_IP="${FLOWCASE_BIND_IP:-${HOST#*@}}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "packaging $ROOT"
tar -C "$ROOT" -czf - \
  --exclude='./web/node_modules' --exclude='./web/dist' --exclude='./.env' --exclude='./.claude' \
  --exclude='./.DS_Store' --exclude='*.png' . \
  | ssh -o BatchMode=yes "$HOST" 'mkdir -p ~/flowcase && cat > ~/flowcase/release.tgz'

ssh -o BatchMode=yes "$HOST" BIND_IP="$BIND_IP" 'bash -s' <<'REMOTE'
set -euo pipefail
cd ~/flowcase
rm -rf app.new && mkdir app.new && tar -xzf release.tgz -C app.new
if [ -f app/.env ]; then cp app/.env app.new/.env; fi
rm -rf app && mv app.new app
cd app
if [ ! -f .env ]; then
  umask 077
  printf 'POSTGRES_PASSWORD=%s\nFLOWCASE_BIND_IP=%s\nFLOWCASE_WEB_PORT=5180\nFLOWCASE_API_PORT=8180\n' "$(openssl rand -hex 24)" "$BIND_IP" > .env
  echo "generated ~/flowcase/app/.env"
fi
if ! grep -q '^AGENT_CALLBACK_SECRET=' .env; then
  printf 'AGENT_CALLBACK_SECRET=%s\n' "$(openssl rand -hex 32)" >> .env
  echo "added AGENT_CALLBACK_SECRET to ~/flowcase/app/.env"
fi
docker compose -f docker-compose.vm.yml build
# App services share one image tag built by `migrate`; compose would not notice the tag moved, so
# recreate them explicitly. Postgres (a dependency, not listed) keeps running.
docker compose -f docker-compose.vm.yml up -d --remove-orphans --force-recreate migrate seed-dev api worker web
docker compose -f docker-compose.vm.yml ps
REMOTE
