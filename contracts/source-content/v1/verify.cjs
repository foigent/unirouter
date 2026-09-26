#!/usr/bin/env node
/* Offline contract vectors only. No HTTP, credentials, services or product code. */
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const HERE = __dirname;
const moduleRoot = process.env.C0_NODE_MODULES;
const resolve = (name) => require.resolve(name, moduleRoot ? { paths: [moduleRoot] } : undefined);
let Ajv, addFormats, ajvPackage;
try {
  Ajv = require(resolve('ajv/dist/2020'));
  addFormats = require(resolve('ajv-formats'));
  ajvPackage = resolve('ajv/package.json');
} catch (error) {
  console.error('Use an existing Ajv 8 + ajv-formats installation via C0_NODE_MODULES. No installation is attempted.');
  process.exit(2);
}
const read = (file) => JSON.parse(fs.readFileSync(path.join(HERE, file), 'utf8'));
const sha = (bytes) => 'sha256:' + crypto.createHash('sha256').update(bytes).digest('hex');
const canonical = (value) => JSON.stringify(sort(value));
function sort(value) {
  if (Array.isArray(value)) return value.map(sort);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(k => [k, sort(value[k])]));
  return value;
}
const same = (a, b) => canonical(a) === canonical(b);
function requireRule(condition, rule) { if (!condition) throw new Error(rule); }
function sourceID(parts) {
  const hash = crypto.createHash('sha256');
  for (const part of parts) {
    const bytes = Buffer.from(part, 'utf8');
    const size = Buffer.alloc(4); size.writeUInt32BE(bytes.length);
    hash.update(size); hash.update(bytes);
  }
  return 'uf1_' + hash.digest('hex');
}
function manifestIdentity(m) {
  const fact = sourceID(['schema_version', 'source_instance', 'org_id', 'project_id', 'profile', 'request_id', 'observation_point'].map(k => m[k]).concat([m.attempt_id || '', m.content_item_id]));
  requireRule(m.source_fact_id === fact, 'source_fact_id');
  requireRule(m.event_id === sourceID([m.schema_version + '.revision', fact, m.revision]), 'event_id');
  for (const k of ['org_id', 'project_id', 'revision']) requireRule(BigInt(m[k]) <= 9223372036854775807n, 'int64_range');
  requireRule(m.recorded_at >= m.occurred_at, 'recorded_after_occurred');
  if (m.expires_at) requireRule(m.expires_at > m.captured_at, 'finite_expiry_after_capture');
}
const schema = read('c0-contract.schema.json');
const vectors = read('vectors.json');
// Conditional schemas inherit object types/properties from their parent or $ref.
// Keep schema/unknown-key strictness; these two Ajv lint switches are not validation relaxations.
const ajv = new Ajv({ allErrors: true, strict: true, strictTypes: false, strictRequired: false, validateFormats: true });
addFormats(ajv);
if (!ajv.validateSchema(schema)) throw new Error(JSON.stringify(ajv.errors));
ajv.addSchema(schema);
const validators = new Map();
for (const name of Object.keys(schema.$defs)) validators.set(name, ajv.getSchema(schema.$id + '#/$defs/' + name));
const results = [];
function record(id, layer, success, detail = {}) { results.push({ id, layer, pass: success, ...detail }); }
const documents = new Map(vectors.valid.map(v => [v.id, v.document]));
for (const v of vectors.valid) {
  const check = validators.get(v.schema);
  const ok = Boolean(check && check(v.document));
  record(v.id, 'schema_positive', ok, ok ? {} : { errors: check?.errors });
}
for (const v of vectors.invalid) {
  const check = validators.get(v.schema);
  const rejected = check && !check(v.document);
  const keywords = (check?.errors || []).map(e => e.keyword);
  record(v.id, 'schema_negative', Boolean(rejected && keywords.includes(v.expected_keyword)), { expected_keyword: v.expected_keyword, observed_keywords: [...new Set(keywords)] });
}
function validatePage(p, first) {
  if (first) requireRule(BigInt(p.after_sequence) === (BigInt(p.coverage_from.after_sequence) > BigInt(p.retention_floor.after_sequence) ? BigInt(p.coverage_from.after_sequence) : BigInt(p.retention_floor.after_sequence)), 'bootstrap_lower_bound');
  let previous = BigInt(p.after_sequence);
  const high = BigInt(p.high_watermark_sequence);
  requireRule(previous <= high, 'cursor_not_ahead');
  for (const e of p.items) {
    requireRule(['source_instance', 'org_id', 'project_id', 'profile'].every(k => e.document[k] === p[k]), 'scope');
    requireRule(BigInt(e.sequence) > previous && BigInt(e.sequence) <= high, 'sequence_order');
    previous = BigInt(e.sequence);
    manifestIdentity(e.document);
    requireRule(sha(canonical(e.document)) === e.source_sha256, 'source_sha256');
  }
  requireRule(previous === BigInt(p.last_sequence), 'last_sequence');
  requireRule(p.has_more === (previous < high), 'has_more');
  if (BigInt(p.retention_floor.after_sequence) > BigInt(p.coverage_from.after_sequence)) requireRule(p.gaps.some(g => g.reason === 'retention_floor_ahead'), 'retention_gap');
}
function validateAvailability(entry) {
  const m = entry.document, a = entry.availability;
  manifestIdentity(m);
  requireRule(sha(canonical(m)) === entry.source_sha256, 'source_sha256');
  if (a.state === 'available') {
    requireRule(['complete', 'partial'].includes(m.capture.completeness), 'available_requires_saved');
    requireRule(a.evaluated_at < m.expires_at, 'expired_cannot_be_available');
  }
  if (a.state === 'expired') requireRule(a.evaluated_at >= m.expires_at, 'expiry_time');
}
function domain(kind, input) {
  switch (kind) {
    case 'revision_chain': {
      let prev;
      for (const m of input.documents) {
        manifestIdentity(m);
        if (prev) {
          requireRule(prev.source_fact_id === m.source_fact_id, 'stable_fact_identity');
          requireRule(BigInt(m.revision) === BigInt(prev.revision) + 1n && m.previous_revision === prev.revision, 'revision_predecessor');
          requireRule(m.previous_hash === sha(canonical(prev)), 'previous_hash');
          requireRule(m.recorded_at >= prev.recorded_at, 'recorded_revision_order');
          if (m.content_ref && m.content_ref === prev.content_ref) requireRule(same(m.representation, prev.representation), 'immutable_representation');
        }
        prev = m;
      }
      return;
    }
    case 'first_page': return validatePage(input.page, true);
    case 'page': return validatePage(input.page, false);
    case 'limit_relation': {
      const { limits: l, policy: p, manifest: m } = input;
      requireRule(l.max_read_bytes >= l.max_saved_bytes, 'read_covers_save');
      requireRule(p.max_saved_bytes <= l.max_saved_bytes && p.max_saved_bytes <= p.quota_bytes, 'policy_within_deployment');
      requireRule(m.representation.saved_bytes <= p.max_saved_bytes, 'saved_within_policy');
      requireRule(l.max_buffer_bytes <= l.max_read_bytes, 'buffer_within_read_budget');
      return;
    }
    case 'policy_view': {
      const view = input.view;
      if (view.application_status === 'applied') requireRule(same(view.desired, view.effective), 'applied_equals_effective');
      return;
    }
    case 'reference': {
      const r = input.reference, m = input.manifest;
      manifestIdentity(m);
      for (const k of ['source_instance', 'org_id', 'project_id', 'request_id', 'source_fact_id', 'revision', 'content_ref', 'representation', 'expires_at']) requireRule(same(r[k], m[k]), 'reference_match');
      requireRule(r.source_sha256 === sha(canonical(m)), 'reference_source_sha256');
      requireRule(r.capture_status === m.capture.completeness, 'reference_capture');
      if (input.location.kind === 'byte_range') requireRule(input.location.offset + input.location.length <= m.representation.saved_bytes, 'byte_range');
      return;
    }
    case 'c_read_state_error': {
      // Declarative vectors on already-authenticated state, not an authorization implementation.
      const endpoints = [
        '/api/v1/integrations/xinsight/source/v2/content-capabilities',
        '/api/v1/integrations/xinsight/source/v2/content-manifests',
        '/api/v1/integrations/xinsight/source/v2/contents/{content_ref}'
      ];
      requireRule(same(input.endpoints, endpoints), 'c_read_endpoint_coverage');
      const state = input.authenticated_state;
      requireRule(typeof state.profile_intersection === 'boolean' &&
        ['active', 'paused'].includes(state.source_status) &&
        ['feed_active', 'feed_staged'].includes(state.credential_status), 'c_read_state_fixture');
      const expected = !state.profile_intersection
        ? [403, 'profile_not_authorized', false]
        : state.source_status !== 'active'
          ? [503, 'source_paused', true]
          : state.credential_status !== 'feed_active'
            ? [409, 'staged_credential_requires_pause', false] : undefined;
      requireRule(Boolean(expected), 'c_read_state_not_a_rejection');
      requireRule(validators.get('URContentReadStateErrorResponse')(input.response), 'c_read_error_structure');
      const actual = [input.response.http_status, input.response.body.error.code, input.response.body.error.retryable];
      requireRule(same(actual, expected), 'c_read_state_mapping');
      return;
    }
    case 'availability': return validateAvailability(input.entry);
    default: throw new Error('unknown_domain_case');
  }
}
for (const v of vectors.domain) {
  let error;
  try { domain(v.kind, v.input); } catch (e) { error = e.message; }
  record(v.id, 'offline_domain', v.expect_valid ? !error : error === v.expected_rule, { expected_valid: v.expect_valid, ...(error ? { observed_rule: error } : {}) });
}
for (const v of vectors.bytes) {
  const m = documents.get(v.manifest), hash = crypto.createHash('sha256');
  let size = 0;
  const update = b => { size += b.length; hash.update(b); };
  if (v.recipe.kind === 'hex') update(Buffer.from(v.recipe.hex, 'hex'));
  else if (v.recipe.kind === 'repeat') {
    for (let i = 0; i < v.recipe.count; i += 65536) update(Buffer.alloc(Math.min(65536, v.recipe.count - i), v.recipe.byte));
  } else throw new Error('Unknown byte recipe');
  const actual = 'sha256:' + hash.digest('hex');
  const matches = size === m.representation.saved_bytes && actual === m.representation.digest;
  record(v.id, 'offline_bytes', matches === v.expect_match, { generated_bytes: size, sha256: actual, expected_match: v.expect_match });
}
// These are format/source-hash checks on isolated alternatives, not one database event set.
for (const v of vectors.valid.filter(v => v.schema === 'Manifest')) {
  let error;
  try { manifestIdentity(v.document); } catch (e) { error = e.message; }
  record(v.id + '_identity', 'offline_identity', !error, error ? { error } : {});
}
for (const v of vectors.valid.filter(v => v.schema === 'ContentProjection')) {
  const value = { ...v.document }; delete value.projection_sha256;
  record(v.id + '_digest', 'offline_projection_digest', sha(canonical(value)) === v.document.projection_sha256);
}
const legacy = read('legacy/completed-execution-v1.schema.json');
const legacyValidator = ajv.compile(legacy);
const legacyGood = read('legacy/valid-settled.json');
record('legacy_completed_execution_v1_unchanged_valid', 'legacy_schema_snapshot', legacyValidator(legacyGood));
for (const [name, mutate] of [
  ['root_prompt', x => { x.prompt = 'synthetic forbidden prompt'; }],
  ['nested_response', x => { x.usage.response = 'synthetic forbidden response'; }],
  ['content_ref_extension', x => { x.content_ref = 'uc1_' + '0'.repeat(64); }]
]) {
  const value = structuredClone(legacyGood); mutate(value);
  record('legacy_v1_rejects_' + name, 'legacy_schema_snapshot', !legacyValidator(value));
}
const counts = {};
for (const r of results) { counts[r.layer] ??= { total: 0, passed: 0 }; counts[r.layer].total++; counts[r.layer].passed += Number(r.pass); }
const inputFiles = ['c0-contract.schema.json', 'vectors.json', 'legacy/completed-execution-v1.schema.json', 'legacy/valid-settled.json', 'verify.cjs', 'R1-内容凭据生命周期.md', 'sources.json'];
const output = {
  contract_version: '1.0.0-candidate.2', evidence_level: 'synthetic_offline_contract_only',
  generated_at: new Date().toISOString(), status: results.every(r => r.pass) ? 'pass_offline_contract_only' : 'fail_offline_contract',
  runtime: { node: process.version, executable: process.execPath, ajv_version: require(ajvPackage).version, ajv_package: ajvPackage },
  meta_schema_validated: true, compiled_definitions: validators.size,
  totals: { checks: results.length, passed: results.filter(r => r.pass).length, failed: results.filter(r => !r.pass).length }, counts,
  input_sha256: Object.fromEntries(inputFiles.map(file => [file, sha(fs.readFileSync(path.join(HERE, file)))])),
  product_tests_run: false, network_calls: 0, installed_dependencies: false,
  not_proven: ['authorization enforcement', 'signed cursor isolation', 'HTTP delivery or streaming', 'revocation races', 'capture durability or financial isolation', 'UI workflow', 'installation, migration, recovery or production acceptance'],
  checks: results
};
if (process.argv.includes('--write-results')) fs.writeFileSync(path.join(HERE, 'verification-result.json'), JSON.stringify(output, null, 2) + '\n');
console.log(JSON.stringify({ status: output.status, totals: output.totals, counts, failures: results.filter(r => !r.pass) }, null, 2));
process.exitCode = output.totals.failed ? 1 : 0;
