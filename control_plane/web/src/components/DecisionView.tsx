import type { Decision, ReasonCode } from "../api/types";
import { fmtDateTime } from "../lib/format";
import { Icon } from "./Icons";
import { Pill } from "./ui";

export const REASON_TEXT: Record<ReasonCode, string> = {
  ROLE_GRANT: "Allowed by an active role binding that includes the required permission.",
  OVERRIDE_ALLOW: "Allowed by an explicit entitlement override in this workspace.",
  EXPLICIT_DENY: "Denied by an explicit entitlement override (deny wins over role grants).",
  NOT_MEMBER: "Denied: the user is not an active member of the workspace.",
  MEMBERSHIP_SUSPENDED: "Denied: the user's membership in this workspace is suspended.",
  APP_DISABLED: "Denied: the app is disabled in the registry.",
  APP_UNPUBLISHED: "Denied: the app has no published version.",
  MISSING_PERMISSION: "Denied: none of the user's roles grant the required permission.",
  SUBJECT_DISABLED: "Denied: the user account is suspended.",
  WRONG_AUDIENCE: "Denied: the token audience does not match this resource.",
  POLICY_ERROR: "Denied: the policy engine failed closed while evaluating this request.",
  WORKSPACE_NOT_ALLOWED: "Denied: the app is not on the allowlist for this workspace.",
};

export function DecisionView({ decision }: { decision: Decision }) {
  const allow = decision.effect === "allow";
  return (
    <div className={`decisionbox ${allow ? "allow" : "deny"}`} aria-live="polite">
      <div className="decisionhead">
        <span className={`decisionicon ${allow ? "ok" : "fail"}`} aria-hidden="true">
          <Icon name={allow ? "check" : "lock"} />
        </span>
        <div>
          <h3>
            {allow ? "Allowed" : "Denied"} · <code>{decision.reason_code}</code>
          </h3>
          <p className="decisiontext">{REASON_TEXT[decision.reason_code] ?? decision.reason_code}</p>
        </div>
        <Pill tone={allow ? "ok" : "fail"}>{decision.effect}</Pill>
      </div>
      <div className="decisiongrid">
        <div>
          <h4>Effective roles</h4>
          {decision.effective_roles.length ? (
            decision.effective_roles.map((r) => (
              <span key={r} className="rolechip">
                {r}
              </span>
            ))
          ) : (
            <span className="muted">none</span>
          )}
        </div>
        <div>
          <h4>Missing permissions</h4>
          {decision.missing_permissions.length ? (
            decision.missing_permissions.map((p) => (
              <span key={p} className="rolechip danger">
                {p}
              </span>
            ))
          ) : (
            <span className="muted">none</span>
          )}
        </div>
      </div>
      {decision.trace && decision.trace.length > 0 && (
        <>
          <h4>Evaluation trace</h4>
          <ol className="trace">
            {decision.trace.map((t, i) => (
              <li key={i} className={t.outcome}>
                <span className="tracebadge">{t.outcome}</span>
                <strong>{t.step}</strong>
                <span>{t.detail}</span>
              </li>
            ))}
          </ol>
        </>
      )}
      <dl className="decisionmeta">
        <div>
          <dt>Action</dt>
          <dd>
            <code>{decision.action}</code> on {decision.resource_type} <code>{decision.resource_id}</code>
          </dd>
        </div>
        <div>
          <dt>Policy</dt>
          <dd>{decision.policy_version}</dd>
        </div>
        <div>
          <dt>Decision id</dt>
          <dd>
            <code>{decision.decision_id}</code>
          </dd>
        </div>
        <div>
          <dt>Correlation</dt>
          <dd>
            <code>{decision.correlation_id}</code>
          </dd>
        </div>
        <div>
          <dt>Evaluated</dt>
          <dd>{fmtDateTime(decision.evaluated_at)}</dd>
        </div>
      </dl>
    </div>
  );
}
