# UAT Report

Generated 2026-10-30 from an actual run of `go test -count=1 -p 1 -v ./...` with
`FLOWCASE_REQUIRE_DB=1` against PostgreSQL 16 (local Docker). Test results: 232 passed, 0 failed or skipped.
UAT IDs refer to `WORKFLOW_CASE_PLATFORM_TECHNICAL_PRD.md` §12.

| UAT | Scenario | Result | Evidence (test → result) |
|---|---|---|---|
| UAT-01 | Reuse across domains (same engine, no workflow-key branches) | **pass** | `TestSamplePacksAreValid` PASS<br>`TestPoCRunsWithGatedApprovalAndSoD` PASS<br>`TestSIMRunsEndToEndWithPermissionScopedEvidence` PASS |
| UAT-02 | Definition publication; running case stays pinned | **pass** | `TestCasePinnedToV1AfterV2Publish` PASS<br>`TestRunningCaseStaysOnPinnedVersion` PASS<br>`TestPublishedVersionsCannotBeEdited` PASS |
| UAT-03 | Parallel barrier fires once despite duplicate callbacks | **pass** | `TestJoinFiresOnceWithDuplicateAndOutOfOrderCallbacks` PASS |
| UAT-04 | Human gate; agent cannot self-approve | **pass** | `TestPoCRunsWithGatedApprovalAndSoD` PASS<br>`TestDatabaseRefusesServicePrincipalDecisions` PASS<br>`TestAgentOutputCannotApproveOrChangeCaseData` PASS |
| UAT-05 | Selective rework after a requirement change | **pass** | `TestChangeInvalidationBlocksG5AndReopensOnlyAffectedReviews` PASS |
| UAT-06 | KMS ACL: unauthorized evidence never reaches agents or artifacts | **pass** | `TestSIMRunsEndToEndWithPermissionScopedEvidence` PASS<br>`TestWithdrawnEvidenceIsNeverAcceptedAsProof` PASS |
| UAT-07 | Replay/recovery: idempotent side effects, recovered state | **pass** | `TestServiceSideEffectAppliedOnceAcrossCrash` PASS<br>`TestCrashedWorkerLeaseIsReclaimedOnce` PASS<br>`TestRetriesWithBackoffThenFailsCase` PASS |
| UAT-08 | Unknown approval policy blocks publication | **pass** | `TestDefectsAreRejected/unknown_policy_(UAT-08)` PASS<br>`TestDraftLifecycleWithConcurrencyAndValidation` PASS |
| UAT-09 | Mini-app wrapper: verified token works; forged/expired/other-tenant refused | **pass** | `TestTrustedHostTokenEndToEnd` PASS<br>`TestTrustedAdapterRejectsInvalidTokens` PASS<br>`TestPermissionMatrix` PASS |
| UAT-10 | SIM: G0 → Intake → editable register → G1 unlocks parallel analysis; outputs await review | **pass** | `TestUAT10SIMGateOrder` PASS<br>`TestChangeInvalidationBlocksG5AndReopensOnlyAffectedReviews` PASS |
| UAT-11 | Structural validation blocks publication | **pass** | `TestDefectsAreRejected` PASS<br>`TestMalformedDocuments` PASS |
| UAT-12 | Durable timer survives restart, fires once, never approves | **pass** | `TestTimerSurvivesRestartAndFiresOnce` PASS |

## Manual / live runs (not automated tests)

| Run | Result |
|---|---|
| PoC case in the browser (owner → condition → approver persona → completion) | pass (Phase 2) |
| SIM v2 to G5, HLD revised in the browser → targeted stale + re-reviews | pass (Phase 3) |
| Full SIM v1 tender G0–G5 on the tailnet VM with dev stubs | pass (Phase 3) |

## What these results do **not** establish

- No real KMS, AI runtime or connector was exercised; all external behaviour used stubs or test doubles.
- The G0–G5 policies remain **provisional**: SoD defaults and delegations are configuration, not ratified DoA.
- The host wrapper contract is unconfirmed; UAT-09 used locally generated keys and tokens.
- Performance, load and security penetration testing were not performed.
