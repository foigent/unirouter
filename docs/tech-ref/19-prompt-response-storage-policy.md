# Prompt And Response Storage Policy

> Version: v0.1
> Scope: PRD2 P2-F11 design decision only. No plaintext prompt or response storage is enabled by this document.

## Decision

PRD2 keeps prompt and response plaintext storage disabled by default.

The shipped request troubleshooting path remains metadata-only:

- request id
- org and project identifiers
- API key id or prefix where already allowed
- model, alias, provider family, and channel display metadata
- status, normalized error, and `doc_url`
- token counts, usage source, cost, and latency
- audit and billing metadata needed to diagnose a request

Prompt and response plaintext, tool payloads, files, images, embeddings, and full provider responses are not stored by default in PRD2.

## Optional Future Storage

Optional plaintext storage is a future policy-gated feature, not part of PRD2 runtime behavior. It must not be implemented until the User explicitly approves the policy and target use case.

If approved later, the minimum controls are:

| Control | Requirement |
|---------|-------------|
| Enablement | Disabled by default; enabled per org or per project by an authorized admin |
| Purpose | Explicit purpose such as regulated audit, customer support, or quality review |
| Retention | Required retention window with automatic expiry and purge |
| Encryption | Encrypted at rest with a key that is independent from JWT and API-key hash secrets |
| Redaction | Configurable redaction before display or export |
| RBAC | Restricted to platform/admin roles with project-scope checks where applicable |
| Audit | Every plaintext read, export, purge, and policy change records an audit event |
| Customer controls | Visible settings for enablement, retention, export, and deletion |
| Export | Export requires role checks and should include audit-friendly metadata |
| Incident response | Operators must be able to disable storage and purge retained content |

## Read Policy

If optional storage is approved later, reads must be audited before content is returned.

Audit rows should include:

- actor user id and role
- org id and project id
- request id or retained object id
- action such as `prompt_response.read` or `prompt_response.export`
- reason or ticket reference when supplied
- timestamp and source

Audit rows must not include plaintext content.

## Retention And Deletion

Any future implementation must support:

- default retention of zero days unless explicitly configured;
- separate prompt and response retention only if the policy requires it;
- automatic expiry;
- admin-triggered purge for an org, project, key, request id, or time range;
- deletion audit records that describe scope and count, not raw content.

## Redaction

Redaction should run before display or export, even if content is encrypted at rest.

Minimum redaction classes:

- API keys and bearer tokens;
- common cloud secret patterns;
- emails and phone numbers when configured;
- authorization headers and cookies;
- provider credentials and channel keys;
- customer-defined regular expressions.

Redaction failures must fail closed for display/export unless an explicitly approved break-glass policy exists.

## Cache And Analytics Boundary

Runtime cache, semantic cache, and advanced analytics must not silently enable plaintext retention.

If a future cache needs retained prompts, responses, embeddings, or normalized request bodies, it must reuse this policy and add cache-specific tenant isolation, invalidation, billing, and audit rules. The PRD2 cache research document remains research-only.

## PRD2 Acceptance Boundary

P2-F11 is accepted for PRD2 when:

- this policy exists and is indexed;
- the repository states that plaintext prompt/response storage is disabled by default;
- optional storage requires explicit future approval;
- enablement, retention, encryption, redaction, RBAC, audit-on-read, and customer controls are covered;
- no code path is introduced that persists plaintext prompts or responses by default.
