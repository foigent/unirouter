# PRD2 Developer Console Spec

> Version: v0.1
> Phase: PRD2
> Status: Draft for Review
> Related PRD: `docs/PRD-Phase2.md` P2-F03, P2-F04, P2-F13

## 1. Product Intent

Developer Console gives enterprise internal developers controlled self-service access to approved AI models without granting platform-admin permissions.

It is not a separate public portal in PRD2. It is a role-aware area inside the existing UniRouter frontend application.

## 2. Navigation Model

One login, one frontend application, role-aware navigation.

Admin-visible areas:

- Dashboard
- Channels
- Channel Keys
- Model Registry
- Wallet / Billing
- Logs
- Audit
- Settings
- License

Developer-visible areas:

- Project Overview
- API Keys
- Approved Models
- Usage
- Request Lookup
- Docs
- Playground, P2

If a user has both admin and developer roles, the app may show a console switcher or grouped navigation. Backend authorization remains mandatory.

## 3. MVP Pages

### 3.1 Project Overview

Purpose:

- Show current project context, allowed model aliases, key health, recent usage, budget status, and recent failed requests.

Primary elements:

- Project selector.
- Current role badge.
- Allowed model aliases.
- Usage cards: requests, tokens, cost, error rate.
- Budget progress.
- Recent request list with request_id.

States:

- No accessible project.
- Project disabled.
- Budget threshold warning.
- No keys yet.

### 3.2 API Keys

Purpose:

- Let authorized developers or project admins create and manage project-scoped API keys.

Required controls:

- Create key.
- Key name.
- Allowed model aliases or inherit project allowed set.
- Optional IP allowlist.
- Disable key.
- Revoke key.
- Copy plaintext key once.

Rules:

- Plaintext key is shown only once.
- Provider/channel keys are never shown.
- Developer cannot create org/global keys.

### 3.3 Approved Models

Purpose:

- Show approved models and aliases for the selected project.

Default view should be curated, not a large marketplace grid.

Recommended sections:

- Recommended for this project.
- Model aliases.
- Embedding models.
- Deprecated models requiring migration.
- Advanced search.

Model detail should include:

- Alias name.
- Resolved model/provider family.
- Capability tags.
- Context length.
- Default output limit.
- Pricing reference.
- Data residency.
- Rate limit.
- Calling example.
- Error examples.
- Best practices.
- Deprecation and migration guidance where applicable.

### 3.4 Usage

Purpose:

- Let project users understand project consumption.

Filters:

- Time range.
- Model alias.
- API key.
- Status.
- Usage source.

Required displays:

- Requests.
- Tokens.
- Cost.
- Error rate.
- Estimated usage ratio.
- Table export where role permits.

### 3.5 Request Lookup

Purpose:

- Let developers troubleshoot by request_id without exposing restricted admin/provider data.

Input:

- request_id.

Display:

- status.
- model alias and resolved model.
- project.
- key prefix.
- tokens.
- cost.
- latency.
- provider family and channel display name if allowed.
- normalized error code.
- suggested documentation link when available.

Never display:

- provider key.
- full API key.
- plaintext prompt/response unless a future policy explicitly enables it.
- other project data.

### 3.6 Docs Entry

Purpose:

- Provide project-aware quickstart.

Content:

- Base URL.
- Current project key example after key creation.
- Python OpenAI SDK example.
- Node OpenAI SDK example.
- curl example.
- Model alias examples.
- Error troubleshooting with request_id.

## 4. P2 Playground

Playground is not P0 unless User changes scope.

If implemented:

- Uses project-scoped keys.
- Does not expose shared platform key.
- Defaults to approved model aliases.
- Shows stream output, cost, tokens, latency, request_id.
- Copies runnable code.
- Stores no prompt/response history by default unless storage policy is approved.

## 5. Permission Behavior

| User Role | Expected Developer Console Behavior |
|-----------|-------------------------------------|
| `project_admin` | Manage project members, keys, allowed models where permitted, budget |
| `developer` | Use models, create/manage permitted project keys, view project usage |
| `viewer` | Read-only project usage, models, docs |
| `billing_admin` | Usage/budget views, not project key creation unless also project role |
| `org_admin` | Can access all org projects |
| `platform_admin` | Full access |

Backend must return 403 for direct URL/API access when role is insufficient.

## 6. API Surface Draft

Draft route families:

```text
GET  /api/v1/projects
POST /api/v1/projects
GET  /api/v1/projects/{project_id}
PUT  /api/v1/projects/{project_id}

GET  /api/v1/projects/{project_id}/members
POST /api/v1/projects/{project_id}/members
PUT  /api/v1/projects/{project_id}/members/{user_id}
DELETE /api/v1/projects/{project_id}/members/{user_id}

GET  /api/v1/projects/{project_id}/keys
POST /api/v1/projects/{project_id}/keys
POST /api/v1/projects/{project_id}/keys/{key_id}/disable
DELETE /api/v1/projects/{project_id}/keys/{key_id}

GET  /api/v1/projects/{project_id}/models
GET  /api/v1/projects/{project_id}/usage
GET  /api/v1/projects/{project_id}/requests/{request_id}
```

Exact paths must be reconciled with existing admin API naming before implementation.

## 7. UI Quality Requirements

- Use the existing UI system and shared components where possible.
- Keep dense operational layout; do not build a marketing landing page.
- Do not show large model marketplace cards by default when a compact approved list is clearer.
- Tables and filters must support repeated enterprise use.
- Empty states should be actionable, such as "Create project key" or "Ask admin to approve models".
- Error states must include request_id where available.
- Mobile support should preserve navigation and key workflows, but PRD2 primary target is desktop enterprise console.

## 8. Testing Requirements

- Route parity for Developer Console API paths.
- Component tests for key creation one-time display.
- RBAC negative tests for direct URL access.
- Browser smoke for project overview, keys, approved models, usage, request lookup.
- No prompt/response plaintext display unless future policy explicitly enables it.

## 9. Open Decisions

- Whether developers can create keys by default or project_admin must grant this.
- Whether project budget enforcement is hard-blocking or alert-only in PRD2 first release.
- Whether Playground moves from P2 to P1 after Project/RBAC is stable.
