#!/usr/bin/env bash
# Live contract checks for every Control-Plane-integrated mini-app on the VM (tests/contract/tokencheck).
# Usage: deploy/contract-check.sh [flowcase|kms|agent-studio|case-workspace ...]   (default: all deployed)
# case-workspace = UCW (instance case-workspace-cp).
set -euo pipefail
VM="${VM:-kibi-vm@100.126.146.52}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APPS="${*:-flowcase kms agent-studio case-workspace}"
BIN="$(mktemp -d)/tokencheck"
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$BIN" ./tests/contract/tokencheck)
scp -q "$BIN" "$VM:control-plane/tokencheck"
ssh "$VM" "APPS='$APPS' bash -s" <<'REMOTE'
set -uo pipefail
id() { docker exec control-plane-postgres-1 psql -U control_plane -d control_plane -Atc "SELECT id FROM users WHERE idp_subject = '7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c0$1'"; }
RAFI=$(id 1); NADIA=$(id 2); MAYA=$(id 3); DIMAS=$(id 4); SARI=$(id 5)
set -a; . ~/control-plane/.env; set +a
TC=(~/control-plane/tokencheck -key ~/control-plane/secrets/service_token_signing_key -issuer "$CP_PUBLIC_URL")
status=0
for app in $APPS; do
  case "$app" in
    flowcase)
      for t in http://127.0.0.1:5280 http://127.0.0.1:8280; do
        echo "=== flowcase via $t"
        "${TC[@]}" -target "$t" -aud flowcase -member "$MAYA" -privileged "$RAFI" -outsider "$NADIA" -check-principal-type=false \
          -expect 'member reads workflows|GET|/api/v1/workflows|member|ok' \
          -expect 'member cannot create workflow (Flowcase role)|POST|/api/v1/workflows|member|403' \
          -expect 'designer passes Flowcase authz|POST|/api/v1/workflow-validations|privileged|notdenied|{"definition":{}}' || status=1
      done ;;
    kms)
      for t in http://127.0.0.1:5380 http://127.0.0.1:8380; do
        echo "=== kms via $t"
        "${TC[@]}" -target "$t" -aud kms -member "$SARI" -privileged "$MAYA" -outsider "$NADIA" \
          -expect 'member lists collections|GET|/api/v1/collections|member|ok' \
          -expect 'member cannot create collection (KMS role)|POST|/api/v1/collections|member|403|{"name":"contract-denied"}' \
          -expect 'CP platform admin is not KMS admin|POST|/api/v1/collections|outsider|401|{"name":"contract-denied"}' \
          -expect 'steward passes KMS authz|POST|/api/v1/collections|privileged|notdenied|{"name":""}' || status=1
      done ;;
    agent-studio)
      echo "=== agent-studio via http://127.0.0.1:8290"
      "${TC[@]}" -target http://127.0.0.1:8290 -aud agent-studio -member "$RAFI" -privileged "$SARI" -outsider "$DIMAS" \
        -expect 'read-only member lists agents|GET|/api/v1/agents|member|ok' \
        -expect 'read-only member cannot create agent|POST|/api/v1/agents|member|403|{"name":"contract-denied"}' \
        -expect 'author passes Agent Studio authz|POST|/api/v1/agents|privileged|notdenied|{}' \
        -expect 'author cannot make the ops release decision|POST|/api/v1/releases/requests/00000000-0000-4000-8000-000000000000/ops-decision|privileged|403|{"approve":true,"reason":"x"}' \
        -expect 'domain reviewer cannot make the ops decision|POST|/api/v1/releases/requests/00000000-0000-4000-8000-000000000000/ops-decision|member|403|{"approve":true,"reason":"x"}' || status=1 ;;
    case-workspace)
      # UCW has no user directory of its own (the CP decides app access), so the outsider check is skipped.
      # The probe must answer 2xx for any verified token even when the CP token exchange fails (it then reports
      # flowcase.available=false + partial_errors). Contract tokens carry session "contract-check", which has
      # no live CP session, so the exchange refuses them and no Flowcase data may be served.
      echo "=== case-workspace via http://127.0.0.1:8480"
      "${TC[@]}" -target http://127.0.0.1:8480 -aud case-workspace -probe /api/v1/workspace/me/context -member "$MAYA" -outsider-check=false \
        -expect 'session-less member token cannot reach Flowcase data via the BFF|GET|/api/v1/workspace/cases|member|401' || status=1 ;;
  esac
done
exit $status
REMOTE
