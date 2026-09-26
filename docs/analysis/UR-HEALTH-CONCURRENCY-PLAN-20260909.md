# UR-A health transaction concurrency correction — proposal v1

Status: independent UR proposal for coordinator review; no health product code
has been changed. Owner task `01a08339-f189-7703-acd9-5d0a63a2873b`.
Base `cd9f354b8a104a6f0677345a5694b19c000ab015`; candidate remains local 1.3.0.
This is a separate object from the accepted M116 marker patch and original A plan.

## Problem and acceptance boundary

The original same-route health transaction starts Repeatable Read, locks/writes
its own attempt fence, then locks shared deployment/credential health rows.
Concurrent ordinary successes therefore reach those rows with older snapshots.
The 13:47 PG excerpt contains seven deployment FOR UPDATE serialization errors,
which include retries; the candidate-on/off HTTP samples contain three final
health 503s, two/one unresolved holds. They are not seven failed HTTP requests.
These requests had a parsed Provider response before health failed.

The hold preserves Provider liability and does not fabricate customer billing.
`provider_attempt_unresolved` has explicit refund/subsidy reconciliation; ordinary
settlement catch-up does not automatically clear it. This proposal prevents the
observed internal update conflict from causing avoidable exceptional holds. It
does not remove holds for actual uncertainty, relax paid-request admission or
promise that every concurrent request must receive HTTP 200.

## Alternatives and proposed decision

| Path | Assessment |
| --- | --- |
| More serialization retries | Leaves a shared-row retry race; latency and exceptional holds depend on concurrency. No retry-budget increase proposed. |
| Skip equal breaker writes | May reduce one hotspot but success must still persist credential last_used_at; does not close the failure class. |
| Session/advisory serialization before RR | Adds connection ownership, cleanup and lock-key lifecycle; an advisory SELECT inside RR still establishes a snapshot before waiting. No new lock service or session state proposed. |
| Table lock before RR snapshot | Serializes unrelated models/tenants and expands lock order; disproportionate to exact owned rows. |
| Locked current-row health transaction | Proposed: use Read Committed only for `applyHealthOnce`, retain all existing row locks, validation, CAS, atomic fence and projection. The later waiter can lock the committed current tuple and decide whether its immutable evidence remains valid. |

This deliberately changes the health transaction's isolation choice; it is not
being presented as unchanged RR or approved merely because it is a one-line edit.
The justification is that the health result is a transition of specifically
identified, locked authority rows, not a historical snapshot query. All success
dependencies below stay locked until commit. Coordinator approval and executable
concurrency/authority tests are required before claiming the change is safe.
Dispatch authorization, planner snapshots, source paging and financial business
transactions retain their existing isolation. No source schema/head/identity,
retry budget, Provider call, financial outcome or hold policy changes.

PostgreSQL 16 specifies that Read Committed locking reads wait and then return
the updated matching tuple, while RR aborts if that tuple changed after the
transaction snapshot. This alone is not an application safety proof: complex
unlocked predicates would make a simple isolation switch insufficient.
[PG16 transaction isolation](https://www.postgresql.org/docs/16/transaction-iso.html)
and [explicit locks](https://www.postgresql.org/docs/16/explicit-locking.html).

## Locked dependency inventory and preserved order

1. `billing/provider_attempt_fence.go`: exact org/request/attempt FOR UPDATE;
   evidence hash, dispatch-owner hash, Provider state/failure kind and existing
   health outcome validated under that lock. The same transaction writes the
   first health fence. Exact same-outcome replay returns without rewriting any
   projection; a conflicting outcome or owner cannot gain authority.
2. Canonical/verify: exact provider connection FOR SHARE, then deployment rows
   FOR UPDATE. Canonical uses the exact ID; verify locks every non-retired row
   for the legacy channel in ID order. Check current state, config/circuit
   revisions, identity bindings and verify equivalence after acquiring locks.
3. Success/auth failure/rate limit then lock the exact credential binding FOR
   UPDATE and validate connection, status, state revision and legacy mapping.
4. Legacy/verify then lock the exact channel FOR UPDATE and the exact channel
   key when required. Verify compares both projections while their rows are
   held; any second-side denial rolls back the already-written attempt fence.
5. Read database clock after these locks, compute the existing transition and
   use the existing exact prior-value CAS. Write all applicable canonical and
   legacy projections, then commit the attempt fence and projections together.

Health classifiers re-read exact locked rows and only return a rejection on CAS
miss. The release-undispatched special branch retains its exact receipt proof,
all-live probe-group test and successful no-op on route drift. Renewal retains
the live token/time check; a stale renewal cannot resurrect a terminal attempt.
No network request is made while these locks are held.

The verify fanout predicate needs a finite-writer check, not an assumption that
row locks block arbitrary inserts. Current production Go insertion sites are:
`modeldeployment/reconcile.go` (legacy mappings) and
`admin/model_application_contract.go` (canonical rows without legacy mapping).
Reconciliation updates/locks the existing provider connection before inserting
or replacing legacy deployments, conflicting with the health connection SHARE
lock. Canonical creation does not add a legacy-channel fanout member. The test
must force this parent-lock interleaving; if another supported writer can change
membership without that fence, this proposal is not sufficient and returns to
review. Direct arbitrary SQL is not asserted to satisfy the supported writer
contract. Existing connection→deployment→credential→legacy ordering is preserved.

## State and failure semantics

- Ordinary success clears failures and persists exact database last_used_at.
  No sampled/approximate time, omitted health write or replacement revision is
  introduced. Concurrent success/failure serializes by the existing locks; each
  uses the latest locked state. A failure that advances a revision makes an old
  success stale; the latter must not heal the new state.
- Administrative disable, identity/config drift, credential disable and probe
  lease loss remain typed rejections. If an admin change wins the lock first,
  the waiting health call validates that current version rather than accepting
  dispatch-time state. If health wins, the admin writer follows it.
- Cancellation, SQL failure or any non-Applied result rolls the whole transaction
  back. Existing bounded serialization/deadlock retry remains available, but
  retry-count tuning is not the mechanism of this fix.
- Commit ACK loss uses the existing exact-owner/outcome recovery path and fresh
  bounded context. It must neither apply health twice nor create a new attempt.
- Recovery for genuine unresolved attempts remains explicit and controlled;
  the test does not automatically refund, subsidize or erase a hold.

## Exact implementation and verification scope

Expected product change: the isolation argument and explanatory invariant in
`internal/modelexecution/health.go`. Existing field transitions/SQL/fences remain
unchanged unless review identifies a specific missing invariant. Document the
isolation correction in the candidate CHANGELOG and source delivery record.
No change to `authorizeOnce` is proposed. If the original four-worker HTTP case
still fails elsewhere, identify it separately rather than silently extending
this change or claiming overall acceptance.

Required tests, using only the authorized temporary PG16 and synthetic ordinary
model/Agent data:

1. Force a prior health-tuple update after the waiting call starts, with actual
   lock-wait evidence. Test canonical, legacy and verify. Verify current tuple
   transition on the first health transaction, not a lucky hidden retry.
2. Concurrent distinct-attempt success/success and success/failure; no lost
   fail count, regression of last_used_at, double projection or healing of a new
   revision. Exact same-attempt replay and conflicting outcome remain fenced.
3. Admin disable/version drift before lock acquisition; verify second-side
   rejection rolls both sides/fence back. Force supported verify membership
   writer interleaving and preserve whole-fanout equivalence.
4. Existing canonical/legacy/verify probe renewal, expiry, stolen-token,
   undispatched release and no-op release tests; cancellation while blocked;
   original commit ambiguity tests for health, renewal and authorization.
5. Original same-key/same-route four-worker handler experiment, capture on/off:
   report success, legitimate protection refusal and failure separately. Check
   no hold caused by the targeted internal health conflict, protection clears
   after normal completion, no double dispatch/charge, exact I-01 and source
   correspondence. Do not reinterpret refused RPS as successful capacity.
6. Run affected original coordinator/financial/gateway integration regression.
   Preserve all pre-change failures and reviewed marker evidence. Cross-profile
   source locks, original outbox recovery and authenticated consumer latency
   remain independent A obligations.

A is still blocked until the reviewed fix and remaining acceptance evidence
pass. This proposal itself is not implementation, a performance result or a
waiver of the source credential approval boundary.

## v2 addendum — projection update time after waiting

Independent review accepted the locked-current-row rationale but identified a
new RC interleaving: H1 starts at t1 and waits, H2 starts at t2 and commits its
health tuple, then H1 acquires that tuple and writes NOW()=t1. Existing RR would
reject that older snapshot; RC must not turn it into a stale updated_at write.
The original v1 text/hash is retained at
`RDgTyF/health-concurrency-plan-v1.md` (SHA-256
`9206d645959e0f8d77b44d9063b2b3f9e5553b8067b1ec774dec29317daee3f8`).

Proposed resolution: every projection write performed by this health transaction
sets updated_at to the same existing databaseTime/dbNow, read by clock_timestamp
after all applicable authority row locks are held. This timestamp means the
instant the locked transition is decided, not transaction start. It also keeps
canonical/legacy mirrors on the same transition instant. No extra clock query,
timestamp column, historical rewrite, time sampling or wall-clock monotonicity
promise is added. The existing last_used_at and lease calculations already use
this dbNow and retain their meanings. A renewal/release that is a no-op retains
the row's old updated_at.

This supersedes v1's expectation of leaving all projection SQL unchanged. Bind
that already-computed time into the four updated_at writes in each of
`health_canonical.go` and `health_legacy.go`: breaker, renewal, release and
credential update. The two actual probe cleanup writes in `health_release.go`
will use that function's existing lock-after dbNow as well, instead of separate
clock_timestamp calls. Attempt-fence timestamps remain their original per-attempt
receipt times; they are not the shared projection fields at issue. Dispatch
authorization SQL and all other writers remain unchanged.

Do not use a separate GREATEST on each mirror or invent a cross-table maximum:
those would change the meaning or leave mirror timestamps different. The
required interleaving is addressed by taking the existing clock after locks.
This does not claim to solve a host clock stepping backward or preexisting
future-dated data; neither global guarantee existed in the reviewed contract.

Additional executable proof: pause H1 after its attempt fence but before its
first shared authority row, run H2 from a later transaction through health
commit, resume H1 and verify all written shared updated_at values equal H1's
returned DatabaseTime and are later than H2's, without a hidden second health
transaction. Cover canonical, legacy and verify, with credential last_used_at,
mirror equivalence and exact-outcome replay. Exercise the real reconciliation
writer's channel/key updated_at inputs and new link.created_at derivation after
the transition, retaining downstream eligibility comparisons. Existing probe,
admin, release, renewal and commit ambiguity tests also cover these bound times.
This is confined to the reviewed health transaction and its affected timestamp
consumer, not a repository-wide timestamp cleanup.

PostgreSQL documents NOW() as transaction time and clock_timestamp as actual
call time. [PG16 current date/time functions](https://www.postgresql.org/docs/16/functions-datetime.html#FUNCTIONS-DATETIME-CURRENT).
The v2 addendum is pending coordinator closure review; product implementation
still waits for agreement on this precise revised object.
