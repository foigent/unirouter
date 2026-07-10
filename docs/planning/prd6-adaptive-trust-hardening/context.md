# Local Analysis Context

> Analysis ID: `hardening_20260711_unirouter_adaptive_trust`
> Evidence collection SHA-256: `029d40e3c9ff7fb5b89ec534e1aeea42057bf170498be9b6bbafa73a773b8165`
> Source drift at collection: none observed

## Source Roots

- UniRouter: `/Users/m4/Desktop/projects/UniRouter`
- X-Insight: `/Users/m4/Desktop/projects/X-Insight`

## Revisions

- UniRouter reviewed revision: `6c8c192d8610662e4c1a4f852ec15d6fb4bb836f`
- X-Insight reviewed revision: `e43add8e0184ba1eb059484053bc5e9d9f4cf6ed`

## Evidence Registry

| ID | Kind | Local source | What it establishes |
|----|------|--------------|---------------------|
| E01 | source | `/Users/m4/Desktop/projects/UniRouter/cmd/uniroute/main.go` and `cmd/uniroute/gateway_handlers.go` | Current authentication, admission, rate-limit, model-governance, and relay order; no generic Tool Gateway route |
| E02 | source | `/Users/m4/Desktop/projects/UniRouter/internal/gateway/relay.go` | Current input check, billing/routing seams, non-stream normalization, and the OpenAI-compatible pre-parse stream flush gap |
| E03 | source | `/Users/m4/Desktop/projects/UniRouter/internal/traceutil/evidence_context.go` | Existing trace/session/actor/delegation evidence context is not a capability token |
| E04 | document | `/Users/m4/Desktop/projects/UniRouter/docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md` | Accepted cross-project v1 boundary is operator-mediated sidecar reconciliation, not online policy control |
| E05 | document | `/Users/m4/Desktop/projects/UniRouter/docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md` | Source-backed 100x, Agent fan-out, tenant, billing, stream, and durability risks |
| E06 | external document | `/Users/m4/Desktop/projects/X-Insight/docs/architecture/runtime-contract-capability-status.md` | X-Insight Phase 1 implemented and unimplemented capability boundary |
| E07 | external document | `/Users/m4/Desktop/projects/X-Insight/docs/discussion/unirouter-xinsight-integration-boundary.md` | Existing design explicitly keeps X-Insight off settlement, failover, and live-request critical paths |
| E08 | standards/guidance | NIST AI RMF GenAI Profile, NIST Agent Identity, OWASP LLM01/LLM06, Google DeepMind AI Control, OpenAI prompt-injection guidance | Risk-based monitoring, identity and authority, complete mediation, dual-speed control, and source-to-sink constraint principles |

## Collection Manifest

The collection digest is the SHA-256 of the sorted eight-line manifest below,
including each artifact's SHA-256 and path as collected. External web guidance
is represented by stable reader-facing URLs in the proposal and is not included
in the local file digest.

```text
1560dd42e08c9ea1fe2d89bcab3db8f30eb6d427a197b175a08c8fee7065bab0  internal/traceutil/evidence_context.go
367c679d17e6d3dd60c6c597b3f8338ccb8f416cfb7cda08a8e3ba909fa165eb  /Users/m4/Desktop/projects/X-Insight/docs/architecture/runtime-contract-capability-status.md
5fd02458ff8ec62783db7fca3771362ea6ce0ada388dabbb00316c6ad6bbcc69  /Users/m4/Desktop/projects/X-Insight/docs/discussion/unirouter-xinsight-integration-boundary.md
6d6c72de47ee7aacce97c598045194de3179dad180867519570949cab1b6d919  cmd/uniroute/main.go
7082da6726e5137f6565fc8e6861e7d0c0bb792f7077714b3a6c6f9f927a86b3  cmd/uniroute/gateway_handlers.go
bfef5fdcc0662cce9e68b3872701c5551c67d12723c8439f08f2e63c441f2ac8  docs/ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md
c1c82e3e5d470405f6da574ed14a2ddca81b0411ef636aa002d2c03647dec123  docs/ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md
fdd3d5eddeaf8b8f8ea0ea657b3edf64ee51e5a8a6a6b56b4f9d5e0602ee606d  internal/gateway/relay.go
```

## Scope Note

This is a proposal analysis, not proof of implementation. No deployment,
credential change, target request, database expansion, exporter, continuous
ingestion, or paid shadow traffic was performed. No `implementation/` artifact
is created because the new adaptive-trust option has not been selected by the
User.
