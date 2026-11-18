# Safe Git bootstrap and delivery

User requests separate GitHub repository so team source and Codex work do not conflict. An independent private clone preserves Git history and avoids public fork visibility. It does not eliminate merge conflicts if upstream changes later; use deliberate sync branches. Proposed destination `abitiyoso/elenio-ai-workspaces` (not yet confirmed by remote). Source `https://github.com/kawakibireku/ai-workspace.git`. On 2026-11-18 source was public, login `abitiyoso`, proposed destination lookup 404. Current Codex auth may differ. Never assume 404 proves absence.

## Bootstrap
1. Locate extracted kit, confirm filesystem location and no dirty repo being overwritten. `gh auth status`, `gh api user --jq .login`, `gh repo view kawakibireku/ai-workspace --json nameWithOwner,defaultBranch,visibility`. Verify permission to copy, license/attribution and distribution constraints including dependency licenses, source history and any customer-specific assets. Public does not mean permissively licensed. Scan entire to-be-pushed history (e.g. local gitleaks if available, plus tracked configuration and sensitive-data review). Do not print discovered secrets, report paths/redacted findings only. If rights or sensitive history are unclear, pause remote publication and do safe local work; never silently strip/rewrite upstream history. Do not include source credentials or customer records in references. Verify destination ownership and visibility using gh, list remote refs. Use the approved exact destination or proposed name when owner matches and empty/new.
2. Example shell commands (adapt to environment; run only after checks, fail fast on errors; no token in URL). If an existing local clone is present, inspect `git status`, remotes and ancestry first rather than cloning over it.

```sh
cd /path/to/parent-directory
# Fresh directory only
 git clone --origin upstream https://github.com/kawakibireku/ai-workspace.git elenio-ai-workspaces
 cd elenio-ai-workspaces
 git remote set-url --push upstream DISABLED
 git switch main
 git rev-parse HEAD
 git remote -v
 git status --short
 # Only after approved copy/secret/license checks and verified account:
 # Create an EMPTY independent PRIVATE repo if confirmed not existing:
 gh repo create abitiyoso/elenio-ai-workspaces --private
 git remote add origin https://github.com/abitiyoso/elenio-ai-workspaces.git
 gh repo view abitiyoso/elenio-ai-workspaces --json nameWithOwner,visibility,defaultBranch
 git ls-remote origin
 # Empty origin only, or explicitly verified safe fast forward:
 git push -u origin main
 git switch -c feat/elenio-platform-foundation
```
If remote exists, do not create again. Verify it is private and owned by target. Fetch `origin`, inspect `git merge-base`, log left/right and `git merge-base --is-ancestor` before choosing a safe fast-forward/baseline. If target contains unrelated history, do not merge --allow-unrelated-histories, overwrite or force push; ask for alternate repo or reconciliation. Do not push every upstream branch/tag indiscriminately. Ensure default branch main and protect it if authorized/available (PR required, checks, no force push); inability to set protection is not permission to bypass PR review. Record source SHA, owner baseline and exact remote URLs. Never write to upstream.

3. Copy kit as namespace `docs/elenio-handoff/` inside checkout (preserve pre-existing docs and root AGENTS, merge instructions rather than replace). Include only safe references; if sensitive materials or rights unclear, leave local and note paths. Check `.gitignore`, exclude `.env`, credentials, DB dumps, caches, node_modules, generated binaries unless required and approved. Avoid committing local fixtures with real customer data. Consider history size and reference images before push; safe original brand assets allowed in private repo. Record provenance of original PRD and prototype. Baseline test existing before edit; commit docs separately. Create narrowly scoped branches as needed.

## Daily delivery
`git status --short`, `git diff --check`, relevant lint/typecheck/test/build, stage specific files, commit with meaningful message, verify `git remote get-url origin` and `git branch --show-current`, `git push -u origin <feature-branch>`. Verify `git rev-parse HEAD` equals feature branch remote SHA (`git ls-remote origin refs/heads/<branch>`). Open a PR in owner's repo only, base main or agreed integration branch; report PR URL, commit SHA, test evidence and blockers. No automatic merge. If gh unavailable, use Git push with verified credentials and give PR instructions; no claim of push when auth fails. Never push from detached HEAD or unrelated repository.

## Upstream synchronization (only when requested)
Fetch upstream; compare baseline SHA and owner changes. Start `sync/upstream-YYYYMMDD` from owner integration/main, review diffs, merge/rebase only on new sync branch per agreed strategy, manually reconcile conflicts with module ownership; test, push to origin, PR. Do not auto-sync on every task. No upstream commits or PRs. Track upstream SHA imported and decisions. Do not discard Codex or team changes.
