#!/usr/bin/env python3
"""Write the agent-studio-cp access map: login = Control Plane user id, tenant = this instance's tenant UUID.
Usage: render-access-map.py <directory.json> <tenant uuid> <output path>"""
import json, os, subprocess, sys

directory, tenant, out = json.load(open(sys.argv[1])), sys.argv[2], sys.argv[3]
rows = subprocess.run(["docker", "exec", "-i", "control-plane-postgres-1", "psql", "-U", "control_plane", "-d", "control_plane", "-Atq",
                       "-c", "SELECT idp_subject || '|' || id || '|' || display_name FROM users"], capture_output=True, text=True, check=True).stdout
cp = {s: (i, n) for s, i, n in (line.split("|", 2) for line in rows.splitlines())}
entries = []
for u in directory["users"]:
    if u["subject"] not in cp:
        print(f"skip {u['subject']}: not in Control Plane directory")
        continue
    uid, name = cp[u["subject"]]
    entries.append({"login": uid, "tenant_id": tenant, "permissions": u["permissions"]})
    print(f"granted {name}: {len(u['permissions'])} permissions")
fd = os.open(out + ".tmp", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w") as f:
    json.dump(entries, f, indent=2)
os.replace(out + ".tmp", out)
