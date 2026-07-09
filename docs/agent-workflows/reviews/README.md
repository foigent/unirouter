# Independent Review Records

This directory stores optional independent review results under Agent Protocol v2.0.

Reviewers do not overwrite `EXECUTION-RECORD.md`, `FIX-SIGNAL.md`, `VERIFY-RESULT.md`, or another reviewer's file.

Use this filename format:

```text
<batch-id>-<reviewer>-<YYYYMMDD>.md
```

Each review must include:

1. Reviewer identity or agent name.
2. Whether the review is independent from implementation.
3. Scope and baseline commit.
4. Commands/checks actually executed and their results.
5. Findings classified as P0, P1, P2, or P3.
6. Final conclusion and any unverified boundary.

P0/P1 findings block or reopen the batch. P2 findings require explicit triage. P3 findings are non-blocking suggestions.
