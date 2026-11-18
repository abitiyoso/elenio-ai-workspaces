# Phase 1 dependency provenance

Observed package metadata in the changed frontend lockfile:

| Package | Installed | Declared license | Notice |
|---|---|---|---|
| @xyflow/react | 12.12.1 | MIT | [React Flow](licenses/React-Flow.txt) |
| elkjs | 0.12.1 | EPL-2.0 OR GPL-3.0-or-later | [ELK.js](licenses/ELK.js.txt) |
| lucide-react | 1.75.0 | ISC | [Lucide](licenses/Lucide.txt) |
| @radix-ui/react-dialog | 1.3.1 | MIT | [Radix](licenses/Radix-Dialog.txt) |

ELK distributed notice retains its upstream EPL text. Its source is available at https://github.com/kieler/elkjs (use the version corresponding to the lockfile). No library source was modified. Keep these notices and package license/source availability requirements with any later distributed runtime/browser bundle; complete delivery SBOM and distribution review belong to the packaging phase. React and existing service dependency notices remain in their packages/lockfiles. No paid/proprietary UI dependency or benchmark product assets/code are included.
