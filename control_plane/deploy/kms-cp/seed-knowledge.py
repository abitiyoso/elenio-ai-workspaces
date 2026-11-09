#!/usr/bin/env python3
"""Seed the Contact Center / DPI catalog knowledge into kms-cp through KMS's real API and governance flow.

Development seeding, run on the VM. Each call uses a short-lived Control Plane token for the acting user
(minted by ~/control-plane/devtoken), so KMS authorises and audits every step under that person:
  steward (Maya)  creates the collection, adds the reviewer, uploads PDFs, submits each version for review
  reviewer (Sari) approves each review (KMS refuses self-approval)
  steward (Maya)  publishes each approved version
Idempotent: reuses the collection by name and skips documents whose title already exists.

Without --approve-as, the reviewer step is left to a person: the script uploads and submits for review, publishes
only versions a reviewer already approved in KMS, and lists the rest as awaiting review (re-run after approving).

Usage: seed-knowledge.py <pdf dir> <steward cp id> [--approve-as <reviewer cp id>] [--reviewer <cp id>] [--docs manifest.json]
  manifest.json: [{"file": "x.pdf", "title": "..."}]; default: the DPI/costing catalog below.
"""
import json, os, subprocess, sys, time, urllib.error, urllib.request, uuid

KMS = os.environ.get("KMS_API", "http://127.0.0.1:8380")
HOME = os.path.expanduser("~/control-plane")
COLLECTION = "Telkom Digital DPI & Costing"
DOCS = [  # (pdf name, title)
    ("telkom-digital-dpi-usecases.pdf", "Telkom Digital DPI — Katalog Use Case"),
    ("telkom-digital-dpi-capabilities.pdf", "Telkom Digital DPI — Katalog Kapabilitas"),
    ("telkom-digital-dpi-solutions.pdf", "Telkom Digital DPI — Katalog Solusi"),
    ("telkom-digital-costing-costing-guide.pdf", "Panduan Perhitungan Costing Project Solusi Digital"),
    ("telkom-digital-costing-standard-project.pdf", "Standard Project — WBS dan Estimasi Mandays"),
    ("telkom-digital-costing-standard-costing.pdf", "Standard Costing — Komponen Biaya dan Status Data"),
    ("telkom-digital-costing-standard-costing-dummy.pdf", "Standard Costing — Data Dummy POC"),
]


def env():
    vals = {}
    for line in open(os.path.join(HOME, ".env")):
        if "=" in line and not line.startswith("#"):
            k, v = line.strip().split("=", 1)
            vals[k] = v
    return vals


ISSUER = env()["CP_PUBLIC_URL"]


def token(sub):
    return subprocess.run([os.path.join(HOME, "devtoken"), "-key", os.path.join(HOME, "secrets/service_token_signing_key"),
                           "-issuer", ISSUER, "-aud", "kms", "-sub", sub], capture_output=True, text=True, check=True).stdout


def call(sub, method, path, body=None, files=None, ok=()):
    headers = {"Authorization": "Bearer " + token(sub), "Accept": "application/json"}
    data = None
    if files is not None:
        boundary = uuid.uuid4().hex
        parts = []
        for name, value in body.items():
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
        fname, content = files
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{fname}"\r\nContent-Type: application/pdf\r\n\r\n'.encode()
                     + content + b"\r\n")
        parts.append(f"--{boundary}--\r\n".encode())
        data = b"".join(parts)
        headers["Content-Type"] = f"multipart/form-data; boundary={boundary}"
    elif body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(KMS + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        if e.code in ok:
            return {"_status": e.code}
        sys.exit(f"{method} {path} as {sub[:8]} -> {e.code}: {e.read()[:400].decode(errors='replace')}")


def main():
    args = sys.argv[1:]
    opts = {}
    for flag in ("--approve-as", "--reviewer", "--docs"):
        if flag in args:
            i = args.index(flag)
            opts[flag] = args[i + 1]
            del args[i:i + 2]
    pdf_dir, steward = args[:2]
    approver = opts.get("--approve-as")
    reviewer = approver or opts.get("--reviewer")
    docs = [(d["file"], d["title"]) for d in json.load(open(opts["--docs"]))] if "--docs" in opts else DOCS
    cols = call(steward, "GET", "/api/v1/collections").get("collections", [])
    col = next((c for c in cols if c["name"] == COLLECTION), None)
    if col is None:
        col = call(steward, "POST", "/api/v1/collections", {"name": COLLECTION, "classification": "internal",
              "description": "Katalog use case, kapabilitas, solusi Telkom Digital DPI dan panduan costing (sumber: elenio-knowledge-hub docs/knowledge)."})["collection"]
        print(f"created collection {col['id']}")
    cid = col["id"]
    if reviewer:
        res = call(steward, "POST", f"/api/v1/collections/{cid}/members", {"principal_id": reviewer, "role": "reviewer"}, ok=(409,))
        print("reviewer already a member" if res.get("_status") == 409 else "added reviewer")

    existing = {d["title"]: d for d in call(steward, "GET", "/api/v1/documents").get("documents", [])
                if d.get("collection_id", cid) == cid}
    pending = []
    for fname, title in docs:
        if title in existing:
            print(f"exists: {title}")
            pending.append(existing[title]["id"])
            continue
        content = open(os.path.join(pdf_dir, fname), "rb").read()
        res = call(steward, "POST", "/api/v1/documents", {"collection_id": cid, "title": title}, files=(fname, content))
        print(f"uploaded: {title} (job {res['job']['id'][:8]})")
        pending.append(res["document"]["id"])

    # Wait for Docling parsing + indexing on the current version of every document.
    deadline = time.time() + 900
    versions = {}
    while time.time() < deadline:
        done = True
        for doc in pending:
            vs = call(steward, "GET", f"/api/v1/documents/{doc}/versions")["versions"]
            cur = next(v for v in vs if v["is_current"])
            versions[doc] = cur
            if cur["parser_status"] == "failed" or cur["index_status"] == "failed":
                sys.exit(f"ingestion failed for {doc}: {cur}")
            if cur["parser_status"] != "success" or cur["index_status"] != "indexed":
                done = False
        if done:
            break
        time.sleep(10)
    else:
        sys.exit("timed out waiting for ingestion")
    print("all versions parsed and indexed")

    for doc, v in versions.items():
        if v["publication_status"] == "published":
            continue
        if v["review_status"] in ("", "none", "not_submitted", "draft", None):
            call(steward, "POST", f"/api/v1/documents/{doc}/submit-review", {"version_id": v["id"], "note": "Seed: pengetahuan untuk agent SIM"})
    if approver:
        for r in call(approver, "GET", "/api/v1/reviews").get("reviews", []):
            if r["status"] in ("pending", "submitted", "open") and r["document_id"] in versions:
                call(approver, "POST", f"/api/v1/reviews/{r['id']}/approve", {"rationale": "Isi sesuai sumber katalog elenio-knowledge-hub; disetujui untuk retrieval."})
                print(f"approved: {r['document_title']}")
    waiting = 0
    for doc in pending:
        vs = call(steward, "GET", f"/api/v1/documents/{doc}/versions")["versions"]
        cur = next(v for v in vs if v["is_current"])
        if cur["publication_status"] != "published":
            if cur["review_status"] != "approved":
                waiting += 1
                print(f"awaiting review: {doc[:8]} (review_status={cur['review_status']})")
                continue
            call(steward, "POST", f"/api/v1/documents/{doc}/versions/{cur['id']}/publish", {"rationale": "Publikasi pengetahuan untuk agent SIM."})
        print(f"published: {cur['id'][:8]} trust={cur['trust_status']}")
    if waiting:
        print(f"{waiting} document(s) await a reviewer's approval in KMS; re-run this script afterwards to publish them")
    print(f"collection_id={cid}")


if __name__ == "__main__":
    main()
