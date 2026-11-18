import type { WorkflowDefinition } from "../api/workflows";

// Synthetic design-review data. No executable bindings, provider rates or customer information.
export const vendorPayment: WorkflowDefinition = {
  schema_version: "1.0",
  key: "vendor-payment",
  name: "Vendor payment assurance",
  version: 1,
  status: "draft",
  description:
    "Validate evidence, resolve exceptions and keep payment authority with Finance.",
  input_schema: {
    type: "object",
    properties: { vendor_matched: { type: "boolean" } },
  },
  policy_refs: ["fixture.finance-review"],
  metadata: {
    fixture: true,
    domain: "Finance",
    provenance: "Elenio Phase 1 synthetic design example",
  },
  nodes: [
    { id: "start", type: "start", name: "Invoice received", next: ["extract"] },
    {
      id: "extract",
      type: "agent_task",
      name: "Document intelligence",
      config: {
        agent_ref: "fixture.document-intelligence@1",
        output_artifact_type: "invoice",
      },
      next: ["split"],
    },
    {
      id: "split",
      type: "parallel_split",
      name: "Verify in parallel",
      next: ["document-check", "vendor-lookup"],
    },
    {
      id: "document-check",
      type: "service_task",
      name: "Check invoice & PO",
      config: { connector_ref: "fixture.erp", operation: "match" },
      next: ["join"],
    },
    {
      id: "vendor-lookup",
      type: "service_task",
      name: "Look up vendor",
      config: { connector_ref: "fixture.erp", operation: "lookup" },
      next: ["join"],
    },
    {
      id: "join",
      type: "parallel_join",
      name: "Collect evidence",
      config: { join: "all", split_ref: "split" },
      next: ["match"],
    },
    {
      id: "match",
      type: "condition",
      name: "Vendor matched?",
      config: {
        expression: { operator: "eq", field: "vendor_matched", value: true },
        branches: { true: "review", false: "exception" },
      },
      next: ["review", "exception"],
    },
    {
      id: "exception",
      type: "agent_task",
      name: "Exception analyst",
      config: {
        agent_ref: "fixture.exception-analyst@1",
        output_artifact_type: "exception",
      },
      next: ["review"],
    },
    {
      id: "review",
      type: "human_approval",
      name: "Finance approval",
      config: { policy_ref: "fixture.finance-review", return_to: "extract" },
      next: ["post"],
    },
    {
      id: "post",
      type: "service_task",
      name: "Post approved payment",
      config: { connector_ref: "fixture.erp", operation: "post" },
      next: ["end"],
    },
    { id: "end", type: "end", name: "Reconciled", next: [] },
  ],
};

export interface KnowledgeDocument {
  id: string;
  name: string;
  collection: string;
  status: "Published" | "In review" | "Indexing" | "Revoked";
  version: string;
  owner: string;
  updated: string;
  pages: number;
  content: string;
}
export const documents: KnowledgeDocument[] = [
  {
    id: "finance-policy",
    name: "Finance approval policy.pdf",
    collection: "Finance",
    status: "Published",
    version: "3",
    owner: "Finance steward",
    updated: "18 Nov 2026",
    pages: 12,
    content:
      "Section 4 · Payment authorization\n\nEvery payment requires an independent Finance approver. The preparer cannot approve their own submission. Unresolved purchase-order or vendor exceptions must return for review before any posting.\n\nThis synthetic policy illustrates the evidence experience. It is not a customer policy or published KMS source.",
  },
  {
    id: "vendor-guide",
    name: "Vendor verification handbook.pdf",
    collection: "Finance",
    status: "Published",
    version: "2",
    owner: "Finance steward",
    updated: "17 Nov 2026",
    pages: 8,
    content:
      "Section 2 · Vendor matching\n\nCompare the invoice vendor identifier against the approved vendor record. A mismatch routes to exception review; it must not be silently normalized. Capture the source document and record reference as evidence.\n\nSynthetic design-review content.",
  },
  {
    id: "invoice-sop",
    name: "Invoice processing SOP.pdf",
    collection: "Finance",
    status: "In review",
    version: "4",
    owner: "Operations steward",
    updated: "17 Nov 2026",
    pages: 18,
    content:
      "Draft procedure\n\nExtract invoice and purchase-order fields. Verify documents and vendor records in parallel. Join both results before evaluating the match. Draft material is not eligible as published evidence.\n\nSynthetic design-review content.",
  },
  {
    id: "exception-playbook",
    name: "Exception handling playbook.pdf",
    collection: "Finance",
    status: "Indexing",
    version: "1",
    owner: "Operations steward",
    updated: "18 Nov 2026",
    pages: 9,
    content:
      "Indexing fixture. Retrieval is unavailable until ingestion and governance gates complete.",
  },
  {
    id: "retention",
    name: "Data retention standard.pdf",
    collection: "Governance",
    status: "Published",
    version: "2",
    owner: "Security steward",
    updated: "16 Nov 2026",
    pages: 14,
    content:
      "Section 3 · Scoped access\n\nKnowledge access follows workspace and document policy. Revoked or expired evidence cannot be reused. Never copy customer knowledge into another workspace.\n\nSynthetic design-review content.",
  },
  {
    id: "legacy",
    name: "Legacy approval matrix.pdf",
    collection: "Governance",
    status: "Revoked",
    version: "1",
    owner: "Security steward",
    updated: "15 Nov 2026",
    pages: 5,
    content:
      "Revoked source. Preview metadata is provided for design review only. This source must not be eligible for retrieval.",
  },
  {
    id: "recruitment",
    name: "Recruitment review guidance.pdf",
    collection: "People",
    status: "In review",
    version: "1",
    owner: "People steward",
    updated: "17 Nov 2026",
    pages: 10,
    content:
      "Draft guidance\n\nAssess only approved job requirements and relevant evidence. A human reviews recommendations. Do not automatically reject candidates based solely on AI output.\n\nSynthetic design-review content.",
  },
  {
    id: "tender",
    name: "Solution evidence checklist.pdf",
    collection: "Engineering",
    status: "Published",
    version: "1",
    owner: "Engineering steward",
    updated: "16 Nov 2026",
    pages: 7,
    content:
      "Evidence checklist\n\nTrack requirement, source, version, owner and open assumption. Check readiness against actual published dependencies before release.\n\nSynthetic design-review content.",
  },
];
