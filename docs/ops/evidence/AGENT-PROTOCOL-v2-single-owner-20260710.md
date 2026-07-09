# Agent Protocol v2 Single-Owner Decision Evidence

> Date: 2026-07-10
> Scope: UniRouter execution, repository integration, deployment authorization, evidence and optional review workflow

## Decision

The User approved replacing the mandatory Codex -> Canary -> Sentinel -> Atlas sequence with Codex as the single execution owner. Canary, Sentinel, Atlas, or other capable reviewers may still perform independent tests and checks, but they are no longer required before the next batch starts.

The approved defaults are:

1. Codex may create branches, commits, pushes and PRs, then merge after required CI passes.
2. Direct push to `main` is not allowed.
3. Codex may deploy to an explicitly identified and authorized non-production environment after access is available.
4. Each production deployment/rollback and other high-impact external action requires User approval.
5. P0/P1 independent-review findings block or reopen a batch; lower findings are triaged.
6. UniRouter and X-Insight remain separate projects with separate repositories, CI, releases and security policy.

## Verified Capability Baseline

- The local GitHub session has admin-level repository permission for `foigent/unirouter` and `foigent/x-insight`.
- UniRouter `main` protection enforces strict `Backend Tests`, `Frontend Build`, and `Docker Build` checks for administrators.
- No usable ECS SSH alias was found in the current SSH configuration.
- `XINSIGHT_POSTGRES_DSN` was not present in the current environment.

The last two facts are access prerequisites, not agent-role gates. They prevent target deployment or persistent import until a secure access method is supplied.

## Evidence And Status Rule

Role completion is replaced by evidence levels:

- `Local verified`
- `CI verified`
- `Target verified`
- `Production verified`
- `External integration verified`

One level cannot be inferred from another. Self-tests are recorded as execution-owner verification, not independent review.

## Secret And Approval Boundary

Passwords, private keys, tokens and DSNs must be injected through SSH agent, keychain, environment variables or an approved secret manager. They must not be written into Git, chat, shared Markdown or evidence artifacts.

Specific User approval remains required for production actions, destructive target database operations, credential lifecycle changes, expanded data export/raw payload capture, provider calls above an agreed limit, and public release/open-source changes.
