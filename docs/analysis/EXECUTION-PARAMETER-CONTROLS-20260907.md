# Execution parameter controls and actual client projection

Review object: this design on main17f98d80386dbfd914e0c90fd912463c12265549,
treebdff2d5a3c22634e3bcd706c083e291a2b24e2ed,1.2.66/M114.

## Active milestone and accepted continuation

The user accepted the September7 productization plan and authorized continued
development/test delivery. This slice makes immutable model parameter controls
visible and makes supported OpenClaw thinking controls produce the documented
request shape. The wider goal still includes HTTP2 continuity, improving the
conservative stream counter, concentrated onboarding, equivalent Probe sharing,
formal delivery channels, proportionate CI, bounded platform reuse and qualified
retirement. None is waived or declared complete by this slice.

Latest Target evidence is1.2.66/M114. Existing14 protocol loops are historical;
Qwen3.7-Max genuinely first-created through data on unchanged1.2.65 images. Answer
quality failed independently. HTTP2 short-request reload observations still fail.
Raw isolated samples include GOAWAY0/last_stream0 and a distinct curl92 after200;
they do not establish one common cause or justify retrying ambiguous requests.
No ingress architecture change is included here.

## Evidence and problem

The public execution catalog already resolves stable Release pins, but exposes
only reasoning mode/effort values. Admin catalogs serialize Profile identity
without the referenced control contract. OpenClaw sync uses a capability boolean
and does not model toggle shape, budget, aliases or unsupported client controls.
Installed client Node24.19.0/OpenClaw has qwen/deepseek/zai and native-effort
compatibility projections and nullable thinkingLevelMap. A no-network invocation
of its exported builder demonstrated qwen on/off but ignored raw
params.thinking_budget. This builder is not sufficient proof of the real send
path; the regression must also stop the actual selected transport at onPayload.

Current stream enforcement compares UTF8 text-byte upper bounds to the reserved
completion allowance. Two8192 requests hit that guard despite fewer measured
provider tokens; a32768 request completed. This slice exposes the conservative
behavior; it does not solve it or relax accounting. A separate exact financial
design must distinguish upstream token limits, reservation exposure, visible
delivery and final usage before changing that authority.

## One read-only projection, no new execution identity

1. Add a typed modelprofile parameter-description projection that resolves the
   existing exact ReasoningContract and ChatProtocolContract. It describes the
   control wire fields/objects, supported and disabled values, defaults, aliases,
   budget range/default/effort exclusion, active output-field maxima and output
   field/range. It copies slices/maps and never mutates registry objects.
2. Keep Profile/Definition canonical bytes unchanged. Admin response DTOs embed
   the existing Profile plus an additive parameter_controls description; compiled,
   published and product-specific catalog paths use the same projection. No new
   data writer, Profile revision, schema, parser, Publication or Release.
3. Public opt-in execution metadata adds the same description only after the
   existing exact Profile resolution and validation. It also identifies the
   existing conservative stream accounting behavior. Stable/public limits and
   request defaults remain separate. Registry metadata is not promoted to active
   execution, and description is not combination qualification.
4. The model-create UI shows provider/public/default output limits separately
   from thinking budget, native levels, aliases and off support. Candidate labels
   remain documented controls; no claim of live qualification. Existing saved
   values and Release authoring limits are unchanged. Explain that a thinking
   budget is its own parameter ceiling, not extra usable output or a guarantee
   that the entire ceiling is executable with other limits.

## Client projection

Use only supported OpenClaw configuration fields, derived from the typed wire
contract rather than provider/model-name guessing. Retain the existing opt-in
sync command and config validation/write owner; dry-run does not write.

- Root enable_thinking boolean with no native effort: qwen format, tools retain
  their existing separate handling. Expose off and a single enabled choice via
  thinkingLevelMap; do not present all labels as distinct native strengths.
- Root reasoning_effort: project supported native values, explicit documented
  aliases and disabled values. For compatible nested thinking.type plus root
  effort, select the existing deepseek wire format only when the entire emitted
  shape is accepted. Required/always-on models never gain an unsupported off.
- A mixed contract that the installed client cannot express faithfully must be
  reported as unsupported for that control, not silently mapped to a different
  wire contract. Static budget controls remain visible but are not advertised as
  client-adjustable unless the real transport proves they are sent.
- Null thinkingLevelMap entries remove unsupported menu levels where the installed
  client supports that field. Explicit parameters, mappings or thinking formats
  that conflict with the current immutable contract fail before config mutation.
  Valid unrelated params and output experiment limits remain untouched.
- Existing legacy server metadata without parameter_controls remains compatible;
  it does not gain new inferred controls. Invalid new projection fails closed.
- The sync report lists native/mapped/unsupported controls and source without
  dumping user params, credentials or prompt content. New controls do not imply
  a newly qualified request shape. Normal Gateway preflight/dispatch is final.

Defaults and explicit off must be checked at both the real transport and its
normal runtime host, including absent options. Do not change global OpenClaw
defaults, install a plugin, patch the client SDK or add a UniRouter-only protocol.

## Verification and delivery

- Pure projection tests: Qwen3.7/3.8, DeepSeek, GLM, Kimi required, MiniMax
  always-on; independent budget/effort, aliases, field maxima and cloned maps.
- Catalog integration: exact data Definition and stable Release remain source;
  mutable registry drift cannot change controls, invalid pins do not fall back.
- UI: two Qwen control types differ; unknown budget is not zero; model switching
  cannot retain stale control descriptions; existing output values unchanged.
- Sync: dry-run, incompatible explicit settings, retained experiment limits,
  nullable menu projection, required/off, unknown wire shape, old-server fallback.
- Actual installed transport onPayload capture with synthetic key and invalid
  endpoint, throwing before HTTP send; deny fetch/undici creation. Include first
  and tool-result continuation, absent/default/off/native/mapped selections.
- Complete affected API schema/document parity, backend/frontend relevant tests,
  one closing review and required CI. Product behavior change increments VERSION.
- On development/test, refresh exact Target identity/images/ledger/Compose/storage
  before writes; use the formal existing rollout owner. Only the affected Qwen
  paths receive new paid verification after checking cumulative budgets. Flash
  currently16/30,CNY3.361465/5 reserved; Max9/30,CNY2.626116/5. Reservations are not
  invoices. Preserve original14 evidence, existing public limits and admin state.

No implementation tests have run for this candidate yet. Source and SDK inspection
and the exported-builder experiment were read-only, with zero Provider calls.
The independent reviewer must assess this exact design before implementation,
using only validated_blocker/validated_required/valid_deferred/rejected/
product_decision_required. User research and existing policy worktrees are untouched.

## Independent pre-code adjudication

Reviewer: parameter_controls_precode_review, read-only, exact initial design SHA256
ce2d68e64013cc4d9f55f95b11314b6f229836e66a9cd7c0e83b52f919727d0f.
Four validated_required findings accepted: select the real host/transport rather
than standalone SDK behavior; withhold max/auto-added ultra; separate config/menu
validation from real received-request validation; fail before writes on explicit
control conflicts and invalid/old-server transitions. No new product decision or
parallel Profile/Release authority was required. UTF8 accounting remains deferred
within this slice and required by the wider milestone.

Actual no-send captures refined the design: the selected transport does not send
the standalone SDK deepseek/zai toggle. Root effort may still be projected when
the exact contract has an independent effort and an enabled provider toggle
default. Qwen boolean-only uses its supported qwen format; the mixed Qwen contract
uses root effort and retains its enabled toggle default. Sending none requires
including none in the client's supported set. Any alias targeting native max must
also be withheld, otherwise the SDK silently downgrades it without max support.

For controls the transport cannot send, no menu is exposed. The installed host
reports off as its empty-menu fallback, but no disabling field is emitted; required
and provider-default reasoning therefore remain active. This client limitation is
reported explicitly, not presented as successful control of those models.

Initial captured evidence: 56 direct-transport onPayload captures, first and tool-result
continuation, zero Provider requests. Eight representative stable names cover Qwen
toggle/mixed effort, DeepSeek, GLM/Kimi effort and default-only GLM/MiniMax. Observed
host defaults: Qwen toggle low (enabled); effort contracts medium (with declared
alias mapping); empty menus off with no wire control. These are version-bound
client defaults, not changes to provider defaults or production qualification.

## Closing review correction

The sole closing review of patch f5bb31b98fc73ea02ccd1fbdff821a5ef70abf4e66b5016c1735c160e8dd3862
found a validated_blocker: the normal Agent's extra-parameter layer strips
DeepSeek V4 reasoning history when the candidate uses the generic openai format.
The initial direct-transport captures did not exercise that layer. Accepted repair:
exercise applyExtraParamsToAgent and signed reasoning history; for the installed
client's exact ID-triggered wrapper use deepseek format only when typed controls
match, exposing verified off/high. Arbitrary stable aliases keep generic root
effort and do not infer that wrapper or an off toggle. No Gateway contract changes.

Final local regression: 68 onPayload captures through the normal host's wrapper
and selected transport, ten model names including a custom stable alias, zero
Provider requests. DeepSeek active continuation retains reasoning_content; off
intentionally omits it. Qwen3.7 default is low/enabled, Qwen3.8 medium, native
DeepSeek high, alias and GLM5.2 medium, GLM5.3/KimiK3 low, default-only GLM5.1 and
MiniMaxM2.7 display off without a disabling wire field. These measurements are
client-version-bound and are not Provider qualification.

The wrapper also accepts raw extra_body/extraBody fields. Sync refuses reasoning
and output overrides in those fields before writes, preserving unrelated fields;
it does not claim the client is universally unable to express a thinking budget.
Reports retain provider defaults and explicitly leave unmeasured client defaults
to the host. All affected sync regressions passed after this repair. The existing
MAR-001 enforcement covers the lesson; 经验文件无需变化.
