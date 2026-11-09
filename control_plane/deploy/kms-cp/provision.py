#!/usr/bin/env python3
"""Provision Control Plane subjects as KMS tenant principals in kms-cp (idempotent).

KMS's control_plane_jwt verifier accepts a token only for an active row in tenant_principals keyed by
(mapped tenant uuid, Control Plane user id). Roles come only from directory.json.
Usage: provision.py <directory.json> <kms tenant uuid>
"""
import json, subprocess, sys

directory, tenant = json.load(open(sys.argv[1])), sys.argv[2]

def psql(container, user, db, sql):
    out = subprocess.run(["docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", db, "-Atq"],
                         input=sql, capture_output=True, text=True)
    if out.returncode != 0:
        sys.exit(out.stderr)
    return out.stdout

def lit(s):
    return "'" + str(s).replace("'", "''") + "'"

cp = {}
for line in psql("control-plane-postgres-1", "control_plane", "control_plane",
                 "SELECT idp_subject || '|' || id || '|' || display_name FROM users;").splitlines():
    sub, uid, name = line.split("|", 2)
    cp[sub] = (uid, name)

stmts = ["BEGIN;", f"INSERT INTO tenants (id, name) VALUES ({lit(tenant)}, 'Example Enterprise') ON CONFLICT (id) DO NOTHING;"]
for u in directory["users"]:
    if u["subject"] not in cp:
        print(f"skip {u['subject']}: not in Control Plane directory")
        continue
    uid, name = cp[u["subject"]]
    roles = "ARRAY[" + ",".join(lit(r) for r in u["roles"]) + "]::text[]" if u["roles"] else "'{}'::text[]"
    stmts.append(f"INSERT INTO tenant_principals (tenant_id, principal_id, display_name, status, roles) "
                 f"VALUES ({lit(tenant)}, {lit(uid)}, {lit(name)}, 'active', {roles}) "
                 f"ON CONFLICT (tenant_id, principal_id) DO UPDATE SET display_name = EXCLUDED.display_name, roles = EXCLUDED.roles;")
    print(f"provisioned {name} (roles: {', '.join(u['roles']) or 'none'})")
stmts.append("COMMIT;")
psql("kms-cp-postgres-1", "kms", "kms", "\n".join(stmts))
