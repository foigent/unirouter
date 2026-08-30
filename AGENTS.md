# UniRouter repository instructions

## Model-adaptation learning closeout

For work that changes or validates a model, provider surface, execution Profile,
capability Probe, usage mapping or pricing draft:

1. At the start, read the applicable Baseline/ADR, current status and
   `docs/tech-ref/MODEL-ADAPTATION-LEARNINGS.md`. Use only lessons whose stated
   scope and trigger apply; verify drift-prone code, provider and Target facts.
2. Before the final gate for an independently shipped Delivery Batch, or before
   declaring an Active Milestone closed, inspect the same learning file. Update
   it only when the file's active-lesson admission threshold is fully met;
   merge, revise or retire existing entries instead of appending duplicates.
3. The closeout report must state active lessons added, revised/merged and
   retired; name the executable enforcement for each; list candidates not
   admitted and why; or state exactly `经验文件无需变化`.

This instruction does not turn reviewer suggestions, deferred findings or
unapproved product choices into requirements. It does not authorize code,
deployment, paid calls, release, canonical traffic or credential changes.

## Risk-proportionate independent review

Before coding a change to shared architecture, identity, migrations, finance,
permissions, routing, Release, or startup/recovery contracts, obtain one
independent read-only adversarial review. The review object must be exact and
unambiguous: a design artifact identity/digest, working-tree diff, commit, or PR
diff, together with the Active Milestone, invariants, non-goals, tests already
run, and known blockers. A pre-code review does not require a commit.

The reviewer may report only `validated_blocker`, `validated_required`,
`valid_deferred`, `rejected`, or `product_decision_required`; it does not edit
code, grant product authority, or expand scope. The main agent verifies and
adjudicates findings. At most one closing review follows, bound to the previous
review object, the new diff, adjudicated findings, and new test results. It does
not rediscover product requirements, but may report a blocker introduced by the
new diff. New non-blocking findings default to deferred.

Ordinary same-contract copy changes, fixtures, pricing manual-review drafts,
wording, and Catalog data that do not change identity or lifecycle do not
trigger a full architecture review. Current pointers, attachment eligibility,
Profile identity/version, operation/surface/protocol identity, Alias, Release
binding, or promotion to execution authority remain review triggers.
