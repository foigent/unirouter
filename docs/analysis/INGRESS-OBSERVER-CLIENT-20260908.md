# Formal rollout observer: use the existing pinned ingress client

Status: exact pre-code review candidate; no code or Target change implements it.
Base d0365cdb04568c16a957a278a1979fea8f5145d8 / tree4aa4d51ab9a912f6f6283800f110041e04857088.

## Active milestone

Finish the accepted HTTP2 rollout fault diagnosis and make the existing formal
rollout observer repeatable without suppressing client errors. The same owner,
Plan/CAS/recovery, ingress/lease/drain and paid-call rules remain unchanged.
Qwen output contract and concentrated onboarding are separate slices.

## Exact evidence

Current observer scripts/internal-target-rollout.py:720 uses hostcurl7.81,
--retry0, discards body and accepts only exit0 plus exact401/protocol. New68
rollout34samplespassed; this does not overwrite earlier failures or prove longflow.

Isolated Target same-image nginx-debug experiment t-wl06wdcv7w01xj4 and bounded
readt-wl06wdd1b0dw83k: hostcurl100baselinepass; during12reloads fourstatus000/exit16
before request HEADERS (outgoingGOAWAYlastsid0/error0); oneexit92 after full200body
len2/END_STREAM thenRST_STREAMsid1/error0. Publicingressunchanged,paidcalls0.
RFC9113 sections6.8 and8.1 describe those distinct protocol cases.

Existing Nginx image sha256:0d3b80406a13a767339fbe2f41406d6c7da727ab89cf8fae399e81f780f814d1
already carries curl8.19/nghttp2 1.69. Controlled comparisont-wl06wddk3ob6ghs uses
that exact image onloopback: oldcurl/moderncurl/Node100each baselinepass;12reloads
moderncurl100pass andNode100pass,oldcurl1complete-bodyexit92. One moderncurl
sample had2connections; exactGOAWAY/no-duplicate-path read pendingt-wl06wddpuuozzeo.
This is evidence for a supported existing client, not proof every client/version
has uninterrupted rollout or permission to introduce Provider retries.

## Change

Reuse curl inside the exact already-running ingress container for both HTTP1.1
andHTTP2 observer samples. The Target already pins nginx_container_id; record
its image digest and curl-V output in a versioned observer-start receipt. Verify
that container is running, identity matches the Target, HTTPS/HTTP2 are supported,
and the client baseline is the qualified supported implementation before any
rollout mutation. No image pull, container creation, tool installation or manual
HTTP2 client/parser. No fallback to hostcurl when unavailable.

Each sample executes docker exec <exact64hexID> curl (argv, no shell), fixed
unauthenticated POST/v1/chat/completions with{}; no credentials can be provided.
Retain --retry0, --noproxy*, CA verification, exactforcedprotocol,5second request
andbounded subprocess deadlines, no redirects or HTTP downgrade. The client's
standard HTTP2 unprocessed-stream reconnect is transport behavior; record
num_connects, never turn timeout/ambiguous response into an application retry.
This path cannot send a Provider request because it contains no authentication.

Read a bounded response body (64KiB maximum), preserve its byte length/digest and
validate JSON error envelope plus401/exactprotocol/exit0. Record error class and
client identity without raw server error/body or environment values. A complete
body plus nonzeroexit is still a failed sample, including the oldexit92 case.
No treatingstatus000 as success. The client-image identity is rechecked during
readback; addressing by immutable containerID prevents a name-swap substitution.

The evidence format gains a named observer contract version. Historical observer
receipts remain checked with their exact existing criteria and failure history.
New receipts require client identity/body/num_connects evidence. Missing/unknown
versions, partialjsonl, count gaps and failed resume samples remain failures.
Do not rewrite earlier operation receipts or report existingfailedruns as passed.

InvocationfromNginxnetworknamespace observes the publicHTTPSendpoint. This differs
from hostnamespace and must be recorded. It verifies same publicTLS/ingress/backend
auth path, not an externalinternet vantage point. Read-only public401 connectivity
for bothprotocols must pass before enabling this path; otherwise retain blockage.

## Validation and release

Required unit regressions: fixedargv/nocredentials/noredirect, client preflight
before mutation, unavailable/changedcontainer or unsupportedclient failclosed,
protocol downgrade/nonzeroexit/5xx/invalidJSON/oversize/truncatedbody fail, exact
clientreceipt identity and historicalfailedreceipt preservation onresume.

Use existing formal Python test suite; no backend/productHTTPbehavior change.
This modifies shipped rollout validation and therefore updates VERSION/CHANGELOG
per current metadata policy; no schema/Bundle/Release/Provider/parser change.
Affected CI remains required; do not change CI gating to exempt this patch.

After integration, use exact-source formal owner for a same-image development
continuity run with the established longflow/cancellation controls if budgets and
supported input still permit; record complete shortclientcoverage and actual
loadedbackend withdrawal/drain/stop. Diagnostic experiments alone cannot close it.
At most necessary paid differential runs after currentbudgetreadback; never rerun
all14models for a verification-client change.

Known blockers: exactmodernreconnectread andpublic401networknamespace proof pending;
no candidate implementation tests yet. No Production, admin/securitygroup changes,
Provider retries, broadcleanup, newdependencies or fallback policy change.
Review read-only; classify only validated_blocker/validated_required/valid_deferred/
rejected/product_decision_required. Evidence paths are read-only supporting data,
not authorpassclaims to adopt without checking. 经验文件无需变化.

## Pre-code review adjudication and prerequisite readback

Independent reviewer /root/onboarding_precode_review reviewed the original
SHA256e0130c6e77166c904c7cf011a599873cae35bed94a0e2a8cc1802d8b14763a91
read-only and independently ran the unchanged37-test suite. OBC-01 accepted:
-q is the first curl argument; strict HTTP2 uses--http2-prior-knowledge.
OBC-02 accepted: only exact old start/sample/end shapes use the legacy reader;
missing/new/unknown mixed versions fail. No extra client or Provider-retry layer.
Only one closing review remains for the final diff and new test evidence.

The pending readt-wl06wddpuuozzeo succeeded. Modernrequest14 received GOAWAY0 with
laststream0 and retried a fresh connection; all100serverpaths appeared exactlyonce.
ActualpublicHTTPS401 probes from the existing ingress container passed both
protocols, each194bytes/bodySHA2547ad462fb04484f8e68e15a07b04505594e1b9210c02c5db3192b7dbeaab1a.
Raw diagnostic artifacts remain on Target and the local official CLI readback;
no production response/body, credentials or Provider calls were captured.

Implementation adds the versioned observer reader without weakening old verdicts.
Only supported future client versions meeting the minimum/features and actual
per-operation baseline can start; this is not a guarantee about all future curl
behavior, since any subsequent response failure still fails the window.
Local45tests pass; exact closing artifact/test results will supersede this count
if the diff changes. No1.2.69Target or complete continuity claim yet.
