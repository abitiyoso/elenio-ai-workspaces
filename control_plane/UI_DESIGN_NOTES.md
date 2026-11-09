# UI Design System — AI Workspace Control Plane

**Direction:** calm, professional enterprise interface, continuous with the latest KMS/Flowcase/Agent Studio prototypes. Not a generic AI chat page. Semantic application navigation first, useful data density and high-contrast foregrounds.

## Tokens

| Token | Value | Usage |
|---|---|---|
| Navy | `#101B33` | Sidebar and primary brand |
| Deep cobalt | `#2864EC` | Primary controls and active state |
| Canvas | `#F4F6FB` | Application background |
| Surface | `#FFFFFF` | Cards, tables, forms |
| Ink | `#1B2B43` | Main readable text |
| Muted | `#7F8FA7` | Supporting labels (not crucial data) |
| Edge | `#E4EAF4` | Dividers and subtle borders |
| Success | `#138667` | Published / allowed / active |
| Warning | `#A87523` | Draft / unconfigured / pending |
| Danger | `#BC5166` | Disabled / blocked / denied |

**Layout:** 258px fixed sidebar, sticky topbar, 12-column bento for Home, three-column launcher cards, dense but legible admin tables, responsive drawer on mobile. Restrained topbar translucent white only; don't render important text on blurry glass.

## Views and interaction behaviors

1. **Home:** role-scoped app launcher, workspace metrics, recent demo audit, trust model. Switching actor/workspace is illustrative, no real SSO.
2. **App Catalog:** Available vs All registered, with locked/coming-soon states. Launch opens a *preview dialog* because real app URLs are not connected. Never claim actual access to KMS/Flowcase/Agent Studio.
3. **People & Teams:** user directory mock, role editor with role checkboxes. Admin-only UI simulation; production must support scoped Workspace Admin separately with more precise checks.
4. **Workspaces:** membership-filtered switcher and workspace cards, demo metadata creation.
5. **Access Control:** choose subject, workspace and app, explain allow/deny code using role intersection and app publishing status. This is only a preview of the eventual backend decision engine.
6. **Mini-App Registry:** create draft app key, fixed same-origin route, manage status; cannot set arbitrary upstream. Publishing is a local demo status change, **not real service deployment**.
7. **Identity & Integrations:** OIDC BFF diagram, IdP and three core adapters with honest configuration-needed labels.
8. **Audit Log:** local activity preview (production must use append-only server-side audit).

## Production conversion

- React/TS pages/components, route loader and authenticated `/me`; no localStorage for token/identity/entitlement authority.
- Backend must supply verified catalog and access decisions, never accept local browser user selection as identity.
- Add controlled error/loading/expired session states, keyboard-operable menus, accessible focus and WCAG review.
- Adopt same-origin mounting by default; CSP and trust review before enabling cross-origin or iframe.
