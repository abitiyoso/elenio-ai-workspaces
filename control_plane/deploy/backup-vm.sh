#!/usr/bin/env bash
# Logical backup of the Control Plane and Keycloak databases on the VM (run on the VM).
# Restore: gunzip -c <file> | docker exec -i control-plane-postgres-1 psql -U control_plane -d <db>
set -euo pipefail
OUT="${1:-$HOME/control-plane/backups}"
mkdir -p "$OUT" && chmod 700 "$OUT"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
for db in control_plane keycloak; do
  docker exec control-plane-postgres-1 pg_dump -U control_plane --clean --if-exists "$db" | gzip > "$OUT/$db-$TS.sql.gz"
done
ls -1t "$OUT"/*.sql.gz | tail -n +29 | xargs -r rm -f   # keep 14 runs
echo "backups written to $OUT"
