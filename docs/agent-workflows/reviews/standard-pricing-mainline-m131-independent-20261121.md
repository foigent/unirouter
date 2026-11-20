# Standard pricing on current main

Reviewers `/root/standard_mainline_port_review` and
`/root/standard_mainline_runtime_review` were independent of implementation.
Scope was the port from `ac53699d` onto exact GitHub main
`9ae41697dac1e376f2efb87177dd63ab2bec6b7d` at 1.15.7/M130. Both reviewers
traced local source and migrations at the exact revision. They did not run
tests or use a real Target during the before-code review.

- `validated_required` M131 is an explicit maintenance or fresh-install
  transition. Check the exact M130 ledger, no serving writers, and empty
  ingress. Preserve proper M130 withdrawal history, budget proof, namespace,
  and signed admission recovery. An empty guarded down can restore M130.
  Retained customer evidence prevents down.
- `validated_required` Record the quote and source reservation in the same
  transaction. Keep the stream counter and delivery fences. Treat confirmed
  Standard token overruns in normal settlement, withheld responses and stream
  output holds as pending without an automatic charge or generic reconciliation.
- `validated_required` Use the existing organization finance roles. The price
  table is versioned, approval must come from a distinct actor, and the binding
  uses the revision last read for the exact organization/project. The UI
  supports multiple model/operation entries and exact integer micro-CNY.
- Main already implements request-log ordering and server drain. The rate-card
  cache prefix and pre-response SSE error still require the verified fixes.

The accepted product rules cover the port. Real migration, Production and
adjustment of historical postings remain separately authorized actions.

The closing review of candidate diff SHA-256
`95167c7c3df06f315d920c56b972daea854328789358ffa744d1dedebc099094`
reproduced three blockers. The upgrade script omitted M130, the carrier
fixture rejected M131, and the Linux installer source gate rejected the
candidate. The author added the exact M130 predecessor, required the two
ledgers to agree, verified that the new pricing tables were empty before
admission, and updated the fixtures and source gates. The author reran all
three paths locally. A native Linux installer run is validated by CI.
