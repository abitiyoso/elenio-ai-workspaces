import { useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { browser, loginUrl } from "../api/browser";
import { Icon } from "../components/Icons";

const RETRY = "The sign-in response could not be verified, so no session was created. Please try again.";
// Codes emitted by the BFF callback (internal/http/auth.go, internal/identity/oidc).
const ERROR_TEXT: Record<string, string> = {
  idp_error: "Sign-in was cancelled or denied at the identity provider.",
  invalid_request: RETRY,
  state_invalid: "The sign-in attempt expired, was already used, or was started in another browser. Please try again.",
  wrong_issuer: RETRY,
  wrong_audience: RETRY,
  nonce_mismatch: RETRY,
  id_token_expired: RETRY,
  id_token_invalid: RETRY,
  missing_id_token: RETRY,
  invalid_subject: RETRY,
  code_rejected: "The identity provider rejected the sign-in code. Please try again.",
  login_failed: RETRY,
  idp_unavailable: "The identity provider is unavailable right now. Try again in a few minutes.",
  not_provisioned: "Your identity is valid, but you have not been added to this organisation yet. Contact an administrator.",
  subject_disabled: "Your account is suspended. Contact an administrator.",
};

export function SignedOutPage() {
  const [params] = useSearchParams();
  const code = params.get("error");
  const known = code ? ERROR_TEXT[code] : undefined;

  useEffect(() => {
    document.title = "Signed out · AI Workspace Control Plane";
  }, []);

  return (
    <main className="signedout">
      <div className="signedcard">
        <div className="brandmark" aria-hidden="true">
          <Icon name="layers" size="lg" />
        </div>
        <h1>{code ? "Sign-in did not complete" : "You are signed out"}</h1>
        {code ? (
          <div className="notice warning" role="alert">
            {known ?? "Something went wrong while signing you in. Please try again."}
            <div className="errmeta">
              <span>
                code <code>{code.slice(0, 64)}</code>
              </span>
            </div>
          </div>
        ) : (
          <p>Your control plane session has ended. Sign in with your organisation account to continue.</p>
        )}
        <button type="button" className="btn primary wide" onClick={() => browser.assign(loginUrl("/"))}>
          Sign in
        </button>
        <p className="tiny muted">Authentication happens at your identity provider. This site never asks for your password.</p>
      </div>
    </main>
  );
}
