# General infrastructure knowledge (kms-cp seed)

Vendor-neutral reference pages for the SIM agents (CCTV/VMS sizing and architecture, reading a KAK/RKS,
infrastructure WBS and cost components). Source of truth is the Markdown here; the PDFs KMS ingests are rendered
from it (Markdown -> HTML -> headless browser print) and are not committed.

Seeding goes through KMS governance: the steward uploads and submits each version for review, a reviewer
approves it in the KMS UI (no self-approval), and a re-run publishes the approved versions:

    seed-knowledge.py <pdf dir> <steward cp id> --reviewer <reviewer cp id> --docs manifest.json
