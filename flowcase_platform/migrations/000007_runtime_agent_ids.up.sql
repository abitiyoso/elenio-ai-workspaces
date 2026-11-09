-- Phase 4: agent runs record the external runtime identifier the pack binding mapped the agent to.
ALTER TABLE agent_runs ADD COLUMN runtime_agent_id text NOT NULL DEFAULT '';
