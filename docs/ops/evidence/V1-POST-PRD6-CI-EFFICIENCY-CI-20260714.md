# V1 Post-PRD6 CI Efficiency CI Evidence — 2026-07-14

## Classification

- Issue: `V1-CI-001`
- Category: confirmed engineering/operational regression
- Severity: High
- Evidence level: CI verified; closed
- PRD6 boundary: unchanged; PRD6 remains `Completed (Accepted Scope)`
- Predecessor evidence:
  `docs/ops/evidence/V1-POST-PRD6-CI-EFFICIENCY-LOCAL-20260714.md`

## Pull request and merge verification

1. PR #198 exact head
   `e9d0847f7034cff501d0234e920fe7c8e2e3d74b` passed run `29299202652`:
   Change Routing, Backend Tests, Frontend Build and Docker Build all completed
   successfully.
2. PR #198 was squash-merged as
   `31d87def0e7c689a6bea8f4f1e39cd9583959694` on
   `2026-07-14T01:54:35Z`.
3. A commit-scoped Actions readback found no automatic CI run for that merge
   commit. This verifies that the prior duplicate post-merge `push` run was
   removed rather than merely delayed.
4. One trusted `workflow_dispatch` run, `29299765793`, executed against exact
   main commit `31d87def0e7c689a6bea8f4f1e39cd9583959694`. Change Routing, Backend Tests,
   Frontend Build and Docker Build all completed successfully.

## Shared cache verification and explicit cleanup

1. Before the workflow change, the repository held 35 caches totaling
   `10,450,365,643` bytes. Thirty-four Go caches totaled `10,378,971,894`
   bytes and were predominantly isolated PR merge-ref entries.
2. The local evidence records three individually deleted closed-PR caches. The
   pre-seed readback was 32 caches totaling `9,571,677,983` bytes.
3. Trusted main run `29299765793` successfully executed its
   `Save shared Go cache from trusted main dispatch` step. Readback returned:
   - cache ID `5720112747`;
   - key
     `unirouter-go-v1-Linux-X64-go1.25.12-8948ad96a45a18d68fdc643dbbfbaf7d227ff64c24e77b2208d3c3b7a52359f5`;
   - ref `refs/heads/main`;
   - size `196,690,158` bytes.
4. Only after that readback, obsolete main `setup-go` cache `5576166108`
   (`265,032,250` bytes) was deleted by exact numeric ID. No wildcard, key-wide
   or bulk deletion was used.
5. The final readback reports 32 caches totaling `9,503,335,891` bytes,
   including 31 Go caches totaling `9,431,942,142` bytes. Cache
   `5576166108` is absent and shared cache `5720112747` is present.

## Documentation-only routing verification

PR #199 first evidence head
`67e603c73b01772983c15ee5b4ba485725d70810` produced run `29300434659`.
Change Routing completed successfully; Backend Tests, Frontend Build and Docker
Build each produced an explicit `skipped` conclusion. The workflow completed
successfully, no expensive job runner was started and the required contexts
were not left missing or Pending. This closes the last `V1-CI-001` gate.

No application behavior, target environment, credential, database, deployment,
production state or PRD6 accepted-scope state changed in this verification.
