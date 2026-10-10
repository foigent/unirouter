# Starter issue drafts

These are prepared task cards, not published or assigned GitHub issues.
Maintainers should confirm scope against the release being prepared before
applying `good first issue` and assigning a reviewer. English is preferred for
titles; Chinese discussion and translation contributions are welcome.

## 1. Add a Simplified Chinese getting-started guide

**Problem:** the Chinese README links to an English-only model setup and
first-request guide. A separate Chinese Linux installer guide already exists.

**Entry:** `docs/community/getting-started.md`, `README.zh-CN.md`.

**Result:** add `docs/community/getting-started.zh-CN.md`, link both language
versions, and point the Chinese README to the translation. Keep commands,
environment variable names, release status, signature requirements and paid
provider-call notices consistent with the current English guide. Include the
existing [Chinese Linux installer guide](install-linux.zh-CN.md) as the
installation entry, keeping its preview status and tested scope explicit.

**Acceptance:** a bilingual maintainer checks semantic parity and every local
link. No deployment, provider account or paid request is needed. Do not invent
download URLs or translate stable API field names.

## 2. Turn the standard-library first-request snippet into a runnable example

**Problem:** getting started includes a Python snippet, but the existing client
examples require framework/SDK dependencies. A small standalone file makes the
first request easier to reproduce and diagnose.

**Entry:** the request snippet in `docs/community/getting-started.md` and
`examples/client-compatibility/README.md`.

**Result:** add a Python standard-library example under `examples/` using
`UR_BASE_URL`, `UR_API_KEY` and `UR_MODEL`. Validate missing settings before
sending; print the returned request ID and explain HTTP failures without
printing the key, request headers or an unredacted error payload. Keep provider
and model selection explicit; do not supply a working default credential.

**Acceptance:** demonstrate success, missing configuration and HTTP failure
against a local synthetic HTTP server. Show that the key never appears in
captured output. No real provider request is required. Link the example from
the English guide and update the Chinese translation if present.

## 3. Add a Simplified Chinese architecture guide

**Problem:** the Chinese entry point has a short UR/X-Insight explanation, but
the deeper architecture guide is English-only.

**Entry:** `docs/community/architecture.md`, `README.zh-CN.md`.

**Result:** add `docs/community/architecture.zh-CN.md` and language links.
Preserve UR's execution/billing responsibility, X-Insight's optional
asynchronous role, and the distinction between recorded evidence and model
answer correctness. Keep diagram node meaning and source links aligned.

**Acceptance:** a bilingual maintainer checks the translation and rendered
diagram. No API, product behavior or new capability claim changes. This task
does not rewrite the owner's [Why UniRouter](why-unirouter.md) essay.
