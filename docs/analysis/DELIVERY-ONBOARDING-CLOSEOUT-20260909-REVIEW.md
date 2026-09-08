# Independent pre-code review and owner decisions

Reviewer: Parfit (`/root/stream_checkpoint_precode_review`), read-only review on 2026-09-09.
Object: design SHA256 `96e121d74beae31a19f03bdedc446d62d604c4ffc332b81437da9120f3c68ce4`,
base main `44a6a158f28f05fb447b183bc529ea7499c02c56`.

The independent findings were returned before implementation. The owner accepted:

| Classification | Finding and required correction |
| --- | --- |
| validated_required | Fix the build wrapper and preflight to the same local Docker daemon; inherited context can override DOCKER_HOST. Test endpoint and builder overrides. |
| validated_required | Check Compose support for the existing !override merge syntax: stable version at least 2.24.4. |
| validated_required | Read the immutable Approval for the exact latest Probe. Preferred ordering cannot prove absence of another Approval. No new writer or schema. |
| validated_required | Only an explicit preflight setup/check failure suppresses expensive CI jobs. Keep Change Routing failed; missing/unknown/skipped/cancelled outputs retain conservative routing. |
| rejected | A new Approval API or a reopened Release architecture is unnecessary. Existing writer and immutable boundaries suffice. |
| valid_deferred | X-Insight, another full model campaign and independent Backend test splitting remain outside this slice. |
| product_decision_required | None. |

No further blocker was identified after these corrections. The implementation
must retain exact Probe selections, actor/target fences, stop on uncertain
Approval responses, and preserve Release/Slot. This record is an owner transcript
of the independent result, not a claim of independent implementation review.

## Closing-review required correction

The same reviewer found that a 2xx Approval response could be incomplete while
the workspace continued sending the remaining commands. The owner accepted
`validated_required` and added local receipt checks for positive safe Approval ID,
exact deployment/operation/capability/Probe, qualified/state and evidence_ref.
Empty, textual, incomplete, mismatched and negative-qualification responses now
stop the batch and require readback; a confirmed first command is not replayed.
The corrected patch object is SHA256
`c7d1357a727ce65ea1ec9e1d1fbe869b2974241efcefb8e06bb81af32bb8084a`.
20 workspace/client tests and final frontend types/build pass. Final independent closing review found no remaining blocker/required. Reviewer
independently passed 65 rollout, 13 ingress, 6 build-wrapper, 14 routing and
20 workspace tests, plus both real-DB integration/race ordering cases.
No Target mutation is inferred from these results.
