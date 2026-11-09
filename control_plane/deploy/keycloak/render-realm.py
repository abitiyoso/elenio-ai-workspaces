#!/usr/bin/env python3
"""Render the Keycloak realm import from the template. The client secret is read from a file and written
only to the rendered output (kept in the private secrets directory, never in the repository)."""
import json, sys

template, secret_file, public_base_url, ssl_required, out = sys.argv[1:6]
secret = open(secret_file).read().strip()
text = open(template).read()
for k, v in {"__CLIENT_SECRET__": secret, "__PUBLIC_BASE_URL__": public_base_url.rstrip("/"), "__SSL_REQUIRED__": ssl_required}.items():
    text = text.replace(k, v)
json.loads(text)
with open(out, "w") as f:
    f.write(text)
