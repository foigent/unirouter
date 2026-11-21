# Standard V1 dispatch concurrency review

Reviewer `/root/standard_503_precode_review` performed both read-only
reviews independently from implementation. This record transcribes the
reviewer's findings and the execution owner's disposition.

The 1.15.8/M131 milestone scope is Standard V1 first use on one synthetic
project. The patch addresses pre-dispatch 503s for healthy public Responses
and Messages requests. It preserves the frozen quote, dispatch ownership, route
qualification, provider-attempt atomicity, and customer authorization ceiling.
Customer activation, production data, and paid suppliers are outside this review.

## Before coding

The reviewer checked base `60a6e6ffa68457f4e59dfa48a452389fc6de8f06`,
the gateway error mapping, the dispatch coordinator and revalidation queries,
the health writers, and the source observation transaction. This was a
source review without test execution.

- `validated_blocker` Ordinary successful requests update the deployment and
  credential rows. A concurrent Repeatable Read dispatch can hit SQLSTATE
  40001 while inserting the attempt's deployment foreign key or acquiring a
  later lock. Removing only the deployment health write leaves credential
  contention.
- `validated_required` Restrict Read Committed to healthy canonical Public
  Execution Evidence V2 without required capabilities or a half-open probe.
  Revalidate the exact authority rows with the existing locks before reading
  public qualification. Preserve other isolation modes, lease semantics,
  transactional evidence and commit-ambiguity recovery.
- `validated_required` Prove that a committed healthy write does not prevent
  dispatch, and that a real authority change after transaction start leaves no
  new durable attempt or owner.

## Closing review

The reviewer verified the six tracked changes against the same base, the
binary-diff SHA-256
`a69cb532a220a637275183086149651c8361ac056d5d1387bffb186931df7c43`,
and a clean `git diff --check`. The reviewer examined the authorization
paths, deterministic test and baseline overlay, and existing run logs without
independently executing the tests. The overlay changes only the test on the
baseline. Its attempt insert failed with SQLSTATE 40001. The candidate's
modelexecution race integration suite passed.

- `validated_blocker` Closed locally for the reproduced path. The deterministic
  regression succeeds on its first attempt after a committed health write.
  Concurrent credential disable rejects authorization and rolls back the
  attempt. The reviewer found no new code or finance blocker.
- `validated_required` The final public qualification read follows the
  authority locks. Repeatable Read continues to cover V1, verify, legacy,
  half-open probes and capability-sensitive dispatch. The gateway still
  requires an Applied result before provider I/O.
- `validated_required` The initial k6 summary only supports aggregate counts.
  The execution owner subsequently added exact HTTP 503 counters and
  per-request checks of attempts and settlement entries to the temporary
  test overlay. These runs are recorded in the execution record.
- `valid_deferred` In the narrowed branch, row validation may surface the
  internal Disabled status before public qualification could return Stale.
  Both gateway callers reject both statuses and preserve rollback.

The review is source and Local evidence. The ECS instance was not accessed.
Current-patch CI, Target acceptance and customer activation require their
own evidence.
