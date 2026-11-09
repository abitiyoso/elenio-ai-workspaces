#!/usr/bin/env python3
"""Provision Control Plane subjects into the flowcase-cp directory (idempotent).

Flowcase's trusted_jwt directory matches users by (issuer, subject) where issuer is the Control Plane origin and
subject is the Control Plane user id (the `sub` of the audience-bound token). Roles come only from
directory.json. Usage: provision.py <directory.json> <control-plane-origin>
"""
import json, subprocess, sys

directory, issuer = json.load(open(sys.argv[1])), sys.argv[2]

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

stmts = ["BEGIN;", f"SELECT id FROM tenants WHERE external_ref = {lit(directory['tenant_key'])};"]
tenant = f"(SELECT id FROM tenants WHERE external_ref = {lit(directory['tenant_key'])})"
for u in directory["users"]:
    if u["subject"] not in cp:
        print(f"skip {u['subject']}: not in Control Plane directory")
        continue
    uid, name = cp[u["subject"]]
    user = f"(SELECT id FROM users WHERE issuer = {lit(issuer)} AND subject = {lit(uid)})"
    stmts += [
        f"INSERT INTO users (issuer, subject, display_name) VALUES ({lit(issuer)}, {lit(uid)}, {lit(name)}) "
        f"ON CONFLICT (issuer, subject) DO UPDATE SET display_name = EXCLUDED.display_name;",
        f"INSERT INTO memberships (tenant_id, user_id) VALUES ({tenant}, {user}) ON CONFLICT DO NOTHING;",
    ]
    for role in u["roles"]:
        stmts.append(f"INSERT INTO role_assignments (tenant_id, user_id, role) VALUES ({tenant}, {user}, {lit(role)}) ON CONFLICT DO NOTHING;")
    if u.get("copy_domain_roles_from"):
        stmts.append(f"INSERT INTO domain_role_assignments (tenant_id, user_id, role) SELECT s.tenant_id, {user}, s.role FROM domain_role_assignments s "
                     f"WHERE s.user_id = {lit(u['copy_domain_roles_from'])}::uuid AND s.revoked_at IS NULL AND s.case_id IS NULL AND NOT EXISTS ("
                     f"SELECT 1 FROM domain_role_assignments d WHERE d.user_id = {user} AND d.role = s.role AND d.revoked_at IS NULL AND d.case_id IS NULL);")
    if u.get("copy_delegations_from"):
        stmts.append(f"INSERT INTO policy_delegations (tenant_id, policy_ref, user_id) SELECT s.tenant_id, s.policy_ref, {user} FROM policy_delegations s "
                     f"WHERE s.user_id = {lit(u['copy_delegations_from'])}::uuid AND s.revoked_at IS NULL AND NOT EXISTS ("
                     f"SELECT 1 FROM policy_delegations d WHERE d.user_id = {user} AND d.policy_ref = s.policy_ref AND d.revoked_at IS NULL);")
    print(f"provisioned {name} ({', '.join(u['roles'])})")
stmts.append("COMMIT;")
if not psql("flowcase-cp-postgres-1", "flowcase", "flowcase", "\n".join(stmts)).strip():
    sys.exit("tenant not found in flowcase-cp")
