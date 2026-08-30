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
