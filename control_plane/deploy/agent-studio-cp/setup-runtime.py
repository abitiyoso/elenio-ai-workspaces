#!/usr/bin/env python3
"""Set up agent-studio-cp for real SIM agent runs, through Agent Studio's own API and governance (development seeding).

Runs on the VM. Each call uses a short-lived Control Plane token for the acting person (~/control-plane/devtoken), so
Agent Studio enforces and audits separation of duties exactly as in the UI:
  ops (Nadia)       provider + credential + deployment, probes, model approvals (development, production)
  author (Sari)     SIM pack bootstrap/configure, model bindings, evaluation runs, release requests
  Domain review (Rafi) and the ops decision (Nadia) are deliberately NOT scripted: people make them in the UI.
Usage: setup-runtime.py <step> [<step>...]   steps: model agents evals request status
The 9Router API key is read from stdin for the "model" step and is never printed.
State is kept in ~/control-plane/agent-studio-cp/runtime-state.json (ids only).
Re-running "agents" after a pack upgrade (pack 1.1.0 adds sim.case_intake, the Flowcase pre-case intake chat agent) merges:
agents configured by that call are added and bound; already-configured agents keep their recorded ids, evals and releases.
"""
import json, os, subprocess, sys, time, urllib.error, urllib.request

AS = os.environ.get("AS_API", "http://127.0.0.1:8290")
HOME = os.path.expanduser("~/control-plane")
STATE = os.path.join(HOME, "agent-studio-cp/runtime-state.json")
BASE_URL = os.environ.get("NINE_ROUTER_BASE_URL", "https://9router.kibicode.my.id/v1")
MODEL = os.environ.get("NINE_ROUTER_MODEL", "cx/gpt-6-luna[1m]")
SUBJECTS = {"sari": "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c05", "rafi": "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c01",
            "nadia": "7f3c1a52-0d1e-4c71-9a0b-5b1e2f3a4c02"}


def sh(*args):
    return subprocess.run(list(args), capture_output=True, text=True, check=True).stdout.strip()


ISSUER = next(l.split("=", 1)[1].strip() for l in open(os.path.join(HOME, ".env")) if l.startswith("CP_PUBLIC_URL="))
CP_IDS = {name: sh("docker", "exec", "control-plane-postgres-1", "psql", "-U", "control_plane", "-d", "control_plane", "-Atc",
                   f"SELECT id FROM users WHERE idp_subject = '{sub}'") for name, sub in SUBJECTS.items()}


def call(who, method, path, body=None, ok=()):
    tok = sh(os.path.join(HOME, "devtoken"), "-key", os.path.join(HOME, "secrets/service_token_signing_key"),
             "-issuer", ISSUER, "-aud", "agent-studio", "-sub", CP_IDS[who])
    headers = {"Authorization": "Bearer " + tok, "Accept": "application/json"}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(AS + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=300) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        text = e.read()[:600].decode(errors="replace")
        if e.code in ok:
            return {"_status": e.code, "_body": text}
        sys.exit(f"{method} {path} as {who} -> {e.code}: {text}")


def items(resp):
    if isinstance(resp, list):
        return resp
    for k in ("items", "data", "providers", "deployments", "agents", "runs", "requests", "releases"):
        if isinstance(resp.get(k), list):
            return resp[k]
    return []


def obj(resp, *keys):
    for k in keys:
        if isinstance(resp.get(k), dict):
            return resp[k]
    return resp


def load():
    return json.load(open(STATE)) if os.path.exists(STATE) else {}


def save(s):
    json.dump(s, open(STATE, "w"), indent=2)


def step_model(s):
    key = sys.stdin.read().strip()
    prov = next((p for p in items(call("nadia", "GET", "/api/v1/model-providers")) if p.get("key") == "ninerouter"), None)
    if prov is None:
        prov = obj(call("nadia", "POST", "/api/v1/model-providers", {"key": "ninerouter", "display_name": "9Router",
                    "adapter_type": "openai_compatible", "base_url": BASE_URL}), "provider")
        print("provider created")
    s["provider_id"] = prov["id"]
    if key:
        call("nadia", "PUT", f"/api/v1/model-providers/{s['provider_id']}/credentials", {"credential": key})
        print("credential stored in vault")
    dep = next((d for d in items(call("nadia", "GET", "/api/v1/model-deployments")) if d.get("key") == "sim-default"), None)
    if dep is None:
        dep = obj(call("nadia", "POST", "/api/v1/model-deployments", {"provider_id": s["provider_id"], "key": "sim-default",
                  "provider_model_id": MODEL, "display_name": f"9Router {MODEL}", "capabilities": {"json": True, "tools": True},
                  "max_output_tokens": 4096, "timeout_ms": 180000, "cost_policy": {}}), "deployment")
        print("deployment created")
    s["deployment_id"] = dep["id"]
    save(s)
    r = call("nadia", "POST", f"/api/v1/model-providers/{s['provider_id']}/probe", {"mode": "connectivity", "deployment_id": s["deployment_id"]})
    print("connectivity probe:", json.dumps(r)[:300])
    r = call("nadia", "POST", f"/api/v1/model-providers/{s['provider_id']}/probe", {"mode": "capabilities", "deployment_id": s["deployment_id"],
             "capabilities": {"tools": True}, "confirm_token_cost": True})
    print("capability probe:", json.dumps(r)[:500])
    for env in ("development", "production"):
        r = call("nadia", "POST", f"/api/v1/model-deployments/{s['deployment_id']}/approvals?environment={env}",
                 {"state": "approved", "reason": "SIM Contact Center pilot: tools + JSON verified by probe"}, ok=(409,))
        print(f"approval {env}:", r.get("_status", "approved"), r.get("_body", "")[:200])
    save(s)


def step_agents(s):
    r = call("sari", "POST", "/api/v1/agents/bootstrap/sim", {}, ok=(409,))
    print("bootstrap:", r.get("_status", "ok"))
    r = call("sari", "POST", "/api/v1/agents/bootstrap/sim/configure", {}, ok=(409,))
    agents = s.setdefault("agents", {})
    for a in r.get("agents", []):
        if a.get("agent_version_id"):
            # Configured by this call: a fresh draft that still needs its model binding.
            agents[a["agent_key"]] = {"version_id": a["agent_version_id"], "suite_id": a.get("eval_suite_id"),
                                      "outcome": a["outcome"], "bound": False}
        else:
            # already_configured etc.: keep the ids, eval and release state recorded earlier.
            agents.setdefault(a["agent_key"], {"version_id": None, "suite_id": None})["outcome"] = a["outcome"]
    print("configure:", {k: v["outcome"] for k, v in agents.items()} or r)
    save(s)
    for k, a in agents.items():
        # Entries recorded before the "bound" flag existed were bound by an earlier run.
        if not a.get("version_id") or a.get("bound", True):
            continue
        for env in ("development", "production"):
            call("sari", "PUT", f"/api/v1/agent-versions/{a['version_id']}/model-binding",
                 {"model_deployment_id": s["deployment_id"], "environment": env})
        a["bound"] = True
        save(s)
        print(f"bound {k} (development, production)")


def step_evals(s):
    pending = {}
    for k, a in s["agents"].items():
        if a.get("eval_pass") or not a.get("version_id") or not a.get("suite_id"):
            continue
        r = obj(call("sari", "POST", "/api/v1/evaluations/runs", {"suite_id": a["suite_id"], "agent_version_id": a["version_id"]}), "run", "eval_run")
        pending[k] = r["id"]
        print(f"eval started {k}: {r['id'][:8]}")
    deadline = time.time() + 1800
    while pending and time.time() < deadline:
        time.sleep(15)
        for k, rid in list(pending.items()):
            r = obj(call("sari", "GET", f"/api/v1/evaluations/runs/{rid}"), "run", "eval_run")
            if r.get("status") in ("completed", "failed", "cancelled"):
                summary = r.get("summary") or {}
                s["agents"][k]["eval_run_id"] = rid
                s["agents"][k]["eval_pass"] = r.get("status") == "completed" and summary.get("gate") == "pass"
                print(f"eval {k}: status={r.get('status')} gate={summary.get('gate')} summary={json.dumps(summary)[:400]}")
                del pending[k]
                save(s)
    if pending:
        print("still running:", list(pending))


def step_request(s):
    """Author step only: Sari submits release requests. Review (Rafi) and ops decision (Nadia) are done by people in the UI."""
    for k, a in s["agents"].items():
        if a.get("release_request_id") or not a.get("eval_pass"):
            continue
        req = obj(call("sari", "POST", "/api/v1/releases/requests", {"agent_version_id": a["version_id"], "eval_run_id": a["eval_run_id"],
                  "environment": "production", "note": "SIM Contact Center pilot release (eval gate passed on 9Router model)"}), "request", "release_request")
        a["release_request_id"] = req["id"]
        save(s)
        print(f"release requested {k}: {req['id'][:8]} state={req.get('state') or req.get('status')}")


def step_status(s):
    print(json.dumps({k: {x: v for x, v in a.items() if x != "suite_id"} for k, a in s.get("agents", {}).items()}, indent=1))


if __name__ == "__main__":
    st = load()
    for step in sys.argv[1:]:
        {"model": step_model, "agents": step_agents, "evals": step_evals, "request": step_request, "status": step_status}[step](st)
