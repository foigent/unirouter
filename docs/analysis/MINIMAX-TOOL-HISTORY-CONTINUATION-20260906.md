# MiniMax tool-result continuation: exact contract successors

Status: Local candidate, 1.2.60/M114, based on dfa46dd80f7946be9ba5a03843e334143485b640
(tree c0b59e43acd5118c6f26c7f58dd48fb7ee31e86b). No Target activation is implied.

## Problem and evidence

Highspeed data Profile1.3 accepts the first actual OpenClaw request, but its
reasoning contract requires nonempty assistant content in tool history. The SDK
returns valid tool calls/results with assistant content null. Full Profile
validation rejects the second request before Provider dispatch even though the
exact chat protocol accepts its tool association. Target readback for
op_c781813a0fd36771227b7309ca078139 shows zero Provider attempts, call logs and
observations. The first turn op_66485210ae8b8a4f217affafa06004c1 completed once
under Release49/Deployment32 with final provider usage and settlement.

A single official Highspeed diagnostic sent the captured continuation with only
upstream model and output limit1024 changed. It returned HTTP200, clean EOF,
17136 input/1024 output tokens and finish=length. Request SHA256
69c6e190ddc24a08596411d817c7d01796d5d5cf5334d60019d8db70b6615200;
response SHA256 296a0e4218f543c6fdf8c6083a7a0b2962c9dffcaa82ec05938951231386e159.
This proves input acceptance and continuation generation, not completed Agent,
answer quality or preservation of reasoning. Private payloads remain outside Git.
M3 has the same source-level content replay constraint; no live M3 continuation
success is claimed. The M111 M2.7 design already establishes the implementation
pattern, but its evidence does not qualify these other exact models.

## Single-authority change

- Highspeed Profile1.4 uses new reasoning.minimax-m2-required-embedded.v2,
  implemented by the existing M2.7 required-embedded behavior. The old M2v1 and
  M27v2 identities retain their contracts. Chat v8/cachev2 remain unchanged.
- M3 Profile1.5 uses reasoning.minimax-m3-adaptive-embedded.v3. It retains
  adaptive/disabled/default/split behavior and removes only mandatory assistant
  content replay. Chat v11 retains the separately shipped service_tier fix.
- Exact typed Probe composition gates admit the two reviewed combinations while
  retaining old valid tuples and rejecting incompatible provider/cache/reasoning
  mixtures. A new reasoning implementation reference makes old Artifact support
  manifests reject these definitions, including on activation/join/rollback.
- The single existing Bundle materializer receives two successor definitions,
  fourteen bindings and fourteen absent-to-current selectors. The Bundle also
  carries the unchanged formal M3 Profile1.4 prerequisite, preserving its exact
  digest. Existing recursive predecessor insertion handles this; no second
  writer or schema migration is introduced.
- Runtime dispatch uses the sealed Definition and preserves messages, tools,
  tool_choice and output parameters. It does not synthesize content, rewrite
  tool IDs or change OpenClaw. Defaults, limits, lineage, public model identity,
  pricing, usage/terminal contracts, credentials and safety revocation are fixed.

## Validation and delivery bounds

Constructed minimal SDK-shape regressions cover null/absent/preserved content,
wrong/missing results, duplicate tool IDs, unsupported split/thinking and limits,
and the full admission-to-provider-body path. Old contracts retain rejection.
Definition predecessor/equality and exact Bundle checks cover data integrity;
removing either new reasoning requirement from the support manifest must reject.
Six relevant Go packages pass the race gate. These are Local facts only.

Each new exact Deployment still needs its own Probe/Approval and sealed Release;
Publication cannot upgrade historical qualification or change active traffic.
The earlier M3 Profile1.4 artifact and all historical definitions/receipts remain.
After required platform fixes, use one reviewed exact-main development rollout,
then formal Bundle delivery and bounded real OpenClaw verification. No new model,
Hermes, Production, fallback, Pricing lifecycle or legacy writer retirement.

Highspeed cumulative conservative reservation after diagnostics is13/CNY8.0349,
including earlier failures and the zero-provider400 reservation. M3 is9/CNY2.68312.
No counter is reset or automatically refunded; any refinement requires exact
request evidence and the existing atomic budget owner. A paid action exceeding
30 attempts/CNY10 is blocked. finish=length is not full task completion.

## Independent review and learning closeout

Pre-code object /private/tmp/unirouter-minimax-tool-history-design-20260906.md
SHA256 a76fc74e57f19bc71470b042a12a171ee147a1be21054a3acb72e66aa2f83178.
Accepted findings: include exact composition gates, obtain precise supplier
continuation evidence, and expose the new combination as an explicit runtime
requirement. The sole closing review follows the final patch and test evidence.
MAR-001 and MAR-003 already govern composition and shared mechanics. Experience
admission adds no distinct invariant: 经验文件无需变化.
