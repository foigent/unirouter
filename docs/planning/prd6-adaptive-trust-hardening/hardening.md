# Security Hardening Review: UniRouter Adaptive Trust Boundary

## Evidence Basis

I inspected the current UniRouter request path, streaming behavior, evidence
context, the accepted PRD5 cross-project contract, the PRD6 scalability audit,
and X-Insight's Phase 1 capability status. The evidence collection is bound to
SHA-256 `029d40e3c9ff7fb5b89ec534e1aeea42057bf170498be9b6bbafa73a773b8165`;
the reviewed UniRouter and X-Insight revisions are recorded in `context.md`.

The strongest observed constraint is simple: X-Insight is currently a controlled
evidence-import and readback system, while UniRouter owns live model dispatch.
Neither system currently owns arbitrary Agent tool side effects. The structural
question is therefore where future adaptive-risk analysis and enforcement
authority should live, not whether a new detector can be inserted somewhere.

## Constraints

- X-Insight cannot become a request-success, settlement, or failover dependency.
- Mandatory identity, tenant, capability, budget, and safety controls need a
  bounded local failure posture.
- Risk signals may attenuate existing authority but may not create authority.
- The actual tool, data, write, or export owner must mediate its own side effect.
- Raw prompt/response retention remains off by default.
- No new online cross-project contract is accepted by this proposal.
- Latency, classifier precision, revocation SLO, and event-lag budgets have not
  yet been measured on production workloads.

## Opportunity Portfolio

| Opportunity | Evidence | Options | Recommendation | Proposal |
| --- | --- | --- | --- | --- |
| Adaptive trust control-loop ownership | Current request path and stream seams (E01-E03); accepted sidecar-only boundary and X-Insight capability status (E04-E07); least-privilege and dual-speed control guidance (E08) | Gateway monolith; synchronous central X-Insight decision; split-plane signed local enforcement | Prefer the split plane under the current availability, ownership, and privacy constraints | [Adaptive trust control loop](proposals/adaptive-trust-control-loop.md) |

## Recommendation Summary

I recommend keeping the real-time kernel small and locally enforceable. X-Insight
should aggregate history and publish a short-lived, signed, attenuation-only
risk assertion. A policy authority should combine that assertion with the
subject's existing entitlement and produce audience-specific snapshots for
UniRouter and the actual Tool/IAM/data enforcement points. UniRouter should not
query X-Insight synchronously, and X-Insight should not sign a universal
`allow` token.

This option costs more protocol and key-lifecycle work than putting every
detector inside the gateway, but it contains outages and gives each component a
truthful authority boundary. It also lets us begin with metadata and capture
gaps, then add deeper analysis without changing the hot-path availability
contract. The recommendation would change if the product deliberately became a
single-process appliance with no external action systems and a measured workload
showed that all required analysis is bounded locally; that is not the current
evidence.

## Next Decisions

1. Accept, modify, or reject the split-plane design before creating an
   implementation handoff for P6-F23.
2. Select the first audience and action scope; the recommendation is model-route
   downscope only, not tool or export authority.
3. Define maximum assertion TTL, revocation SLO, risk hysteresis, and ordinary
   versus privileged-action expiry behavior.
4. Decide whether the first experiment remains metadata-only. The recommendation
   is yes.
5. Keep live shadow traffic, continuous ingestion, and target deployment behind
   their existing explicit authorization gates.
