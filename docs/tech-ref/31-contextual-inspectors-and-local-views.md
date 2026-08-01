# Contextual Inspectors And Local Views

> Version: v1.0
> Date: 2026-07-31
> Scope: PRD7 P7-F02 Audit and Projects surfaces

## Product contract

Audit and Projects use a persistent list-plus-inspector workspace. When a
current page contains records, the first server-ordered record is selected
automatically. A valid deep-linked identity takes precedence. Read-only detail
never requires a separate “open” action.

Selection is available by pointer and keyboard. At desktop and medium widths,
the list and inspector remain side by side with independent scrolling. Below
900 CSS pixels they form one reading-order column, with the inspector following
the list. The page itself must not introduce horizontal overflow.

## URL contract

Each page owns only its documented query keys and preserves unrelated query
parameters and the hash:

- Audit: filter fields, `page`, `page_size`, and `audit_id`.
- Projects: `org_id`, `project_id`, `project_tab`, `project_page`, and
  `project_page_size`.

Positive identities and pagination values must be safe integers. Audit page
sizes and Project page sizes are limited to 20, 50, or 100. Invalid values
fall back to the page defaults rather than becoming API authority.

Changing a scope, filter, or page clears the prior selection before loading.
When the requested deep-linked identity is absent from the returned page, the
first item becomes the selected identity and the URL is corrected.

## Saved-view contract

Named views are browser-local operability metadata, not server-owned product
data:

- storage keys are `unirouter.audit.saved-views.v1` and
  `unirouter.projects.saved-views.v1`;
- each key retains at most 12 validated views;
- names contain 1 to 40 trimmed characters;
- an existing name is replaced atomically in the local array;
- Audit views contain filters only;
- Project views contain organization, project, tab and pagination identities;
- no audit payload, request/response body, credential, member record, model
  assignment or project description is stored.

Malformed or unavailable browser storage fails closed to an empty view list.
This version does not claim tenant-synchronized or cross-device saved views.

## Loading and truth behavior

Filters have draft and applied state. Editing an Audit field does not send a
request until Apply or Enter is used. A monotonically increasing request
version prevents a slower prior response from replacing the current result.

Loading a new scope clears prior rows and inspector facts. An unavailable
response exposes a persistent retry state and does not leave a stale detail
from another organization, page, or filter. Missing facts remain `-` or an
explicit empty state; they are not inferred.

Projects independently fence list, member, and model-set requests. Selecting a
new project clears member and model state before the new tab data is loaded.
Role and License checks continue to govern mutation controls; a persistent
inspector is not a new authorization boundary.

## Compatibility boundary

P7-F02 is a frontend interaction and local metadata feature. It does not change
the Audit or Project APIs, persistence, IAM, routing, billing, Outbox, Feed,
cursor/checkpoint, credential, X-Insight, or shared protocol behavior. The
existing create/edit/member/model mutation endpoints keep their prior
authorization and semantics.
