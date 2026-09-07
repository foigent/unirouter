# Independent Review Records

This directory stores independent reviews under the current Agent Protocol. Root `AGENTS.md` defines mandatory pre-code triggers, exact-object requirements, reuse and the closing-review limit; other reviews are optional. Severity labels do not grant product or external authority.

Reviewers do not overwrite `EXECUTION-RECORD.md`, `FIX-SIGNAL.md`, `VERIFY-RESULT.md`, or another reviewer's file.

Use this filename format:

```text
<batch-id>-<reviewer>-<YYYYMMDD>.md
```

Each review must include:

1. Reviewer identity or agent name.
2. Whether the review is independent from implementation.
3. Scope and exact review object: design identity/digest, working-tree diff, commit or PR diff; a pre-code review does not require a code commit. Include the Active Milestone, invariants, non-goals, tests actually run and known blockers.
4. Commands/checks actually executed and their results.
5. Findings classified as `validated_blocker`, `validated_required`, `valid_deferred`, `rejected`, or `product_decision_required`; P0–P3 may additionally describe validated severity.
6. Final conclusion and any unverified boundary.

The execution owner verifies and adjudicates findings. Validated current P0/P1 blockers stop the affected promotion; post-release defects create a corrective batch without rewriting historical evidence or reopening completed product scope. Required current acceptance work is resolved; new non-blocking findings default to deferred. Record independent suggestions briefly without automatically creating permanent issues. Product decisions do not become implementation requirements without the corresponding authority.
