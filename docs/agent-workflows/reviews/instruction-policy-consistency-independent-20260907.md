# Instruction policy consistency — independent review, 2026-09-07

Reviewer: `/root/policy_precode_review`, an independent read-only subagent. The execution owner transcribed this record from its tool-visible reports; this is not a self-review claim. No implementation files were edited by the reviewer.

## Scope and evidence

- Active task: approved project instruction/entrypoint/release-check consistency; no business contract expansion, paid CI, Target or external writes.
- Baseline: `17f98d80386dbfd914e0c90fd912463c12265549`.
- Pre-code design: `/private/tmp/unirouter-instruction-policy-review-20260907.md`, SHA256 `ad411b4ac4461f802733a6a59c8e1325446b3537b6fb873e824a2b84a793e35f`.
- Final implementation patch: `/private/tmp/unirouter-policy-evidence-20260907/final-candidate.patch`, SHA256 `a0a7846f8b5872b7c5fceb20716323d7bfdc35260ba044c592cd8f8f75f98eb5`; 18 exact file hashes in `final-candidate-hashes.json`.
- The single closing review first inspected the raw candidate and applicable policy, then reconciled prior findings and verifiable logs. Its incremental fix check remained within that closing review.

## Findings and disposition

- `validated_required`, resolved: keep frontend test exemptions narrow and retain type-check, Vitest and Docker coverage. Confirmed entrypoints/import patterns and shipped dist boundaries at the baseline; naming is not permanent proof of non-runtime use.
- `validated_required`, resolved: execute the real checker against isolated Git repositories; cover runtime/deployment/contract changes, mixed edits, deletion/rename, invalid refs, metadata and version changes, and diff errors. All implemented.
- `validated_required`, resolved: distinguish narrative path classification from changed product contracts; governance preserves contract review and version obligations.
- `validated_required`, resolved during closing review: the reviewer independently reproduced a `git diff --quiet` exit 73 being accepted as a changed file with otherwise valid version metadata. Final code removed every quiet call and uses one checked diff to derive all change flags; independent missing VERSION, missing CHANGELOG, dated-heading and diff-failure tests pass.
- `rejected`: no validated unresolved blocker in the final candidate. Explicit approvals, independent-review triggers and original required CI coverage remain. Removing the single new regression step yields byte-identical original CI; other workflows and product version files are unchanged.
- `rejected`: no remaining validated conflict in the reviewed principal entrypoints. The final three stale navigation descriptions were corrected. This does not assert completeness of all historical requirements.
- `rejected`: Documents restoration preserves page rendering, inspection and necessary rerendering; only five previously approved completion-boundary phrases were restored, preserving the new template reference.
- `valid_deferred`: broader Bundle CI splitting, additional script exemptions and semantic dependency analysis are outside this task. No remote protection/CI/Target validation or external authority follows from this review.

## Independently executed checks

- `PYTHONDONTWRITEBYTECODE=1 python3 scripts/test-release-metadata.py`: final 12 tests passed, 11.894 seconds.
- `bash -n scripts/check-release-metadata.sh`: passed.
- `git diff --check`: passed.
- 18 file hashes and CI byte comparison: passed.
- 281 relative Markdown links: no missing targets; closing increment changed no link targets.
- Documents installed contents match the reviewed after snapshot and SHA256.

Primary-worktree synchronization and its separate test run were performed by the execution owner after exact-preimage checks, not claimed as the reviewer's separate execution. This record grants no commit, push, merge, deployment, credential or traffic authority.
