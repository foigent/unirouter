import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const rootDir = process.cwd();
const backendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_BACKEND_URL || process.env.BACKEND_URL || 'http://localhost:8080');
const preflightOnly = process.env.UNIROUTE_PRD4_UPSTREAM_PREFLIGHT_ONLY === 'true' || process.argv.includes('--preflight');
const requiredMutationValue = 'approved-target-real-upstream';
const maxTokens = boundedInt(process.env.UNIROUTE_PRD4_UPSTREAM_MAX_TOKENS || '16', 1, 64);
const evidencePath = process.env.UNIROUTE_PRD4_UPSTREAM_EVIDENCE_PATH || defaultEvidencePath();

const requiredEnv = [
  'OPENROUTER_API_KEY',
  'OPENROUTER_MODEL',
  'ANTHROPIC_API_KEY',
  'ANTHROPIC_MODEL',
  'UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION',
  'ENCRYPTION_KEY or UNIROUTE_SMOKE_ENCRYPTION_KEY',
  'API_KEY_HASH_SECRET or UNIROUTE_SMOKE_API_KEY_HASH_SECRET or JWT_SECRET',
];

const missing = missingEnv();
if (preflightOnly) {
  console.log(JSON.stringify({
    status: missing.length === 0 ? 'READY' : 'SKIP',
    scope: 'PRD4 Batch 1H real upstream evidence harness preflight',
    backend_url: backendURL,
    git_commit: gitCommit(),
    missing,
    required_mutation_value: requiredMutationValue,
    would_mutate_database: false,
    would_call_real_providers: false,
  }, null, 2));
  process.exit(0);
}
if (missing.length > 0) {
  throw new Error(`real upstream smoke is not configured; missing ${missing.join(', ')}`);
}
if (process.env.UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION !== requiredMutationValue) {
  throw new Error(`UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION must equal ${requiredMutationValue}`);
}

const startedAt = new Date().toISOString();
await assertBackendReady();
const seed = runSeed();
const adminToken = await apiLogin(seed.admin_username, seed.admin_password);

const checks = [];
for (const provider of seed.providers) {
  checks.push(await runGatewayCase(provider, false));
  checks.push(await runGatewayCase(provider, true));
}

const evidence = {
  status: 'PASS',
  batch: 'PRD4 Batch 1H',
  scope: 'real upstream evidence harness',
  started_at: startedAt,
  completed_at: new Date().toISOString(),
  backend_url: backendURL,
  git_commit: gitCommit(),
  project_id: seed.project_id,
  project_slug: seed.project_slug,
  api_key_prefix: seed.api_key_prefix,
  providers: seed.providers.map(provider => ({
    provider: provider.provider,
    model: provider.model,
    adapter_type: provider.adapter_type,
    channel_name: provider.channel_name,
    base_url: provider.base_url,
  })),
  checks,
};

assertNoSecretLeak(evidence, [
  process.env.OPENROUTER_API_KEY,
  process.env.ANTHROPIC_API_KEY,
  seed.api_key,
]);

fs.mkdirSync(path.dirname(evidencePath), { recursive: true });
fs.writeFileSync(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`, { mode: 0o600 });
console.log(JSON.stringify({ ...evidence, evidence_path: evidencePath }, null, 2));

function missingEnv() {
  const missingValues = [];
  if (!process.env.OPENROUTER_API_KEY) missingValues.push('OPENROUTER_API_KEY');
  if (!process.env.OPENROUTER_MODEL) missingValues.push('OPENROUTER_MODEL');
  if (!process.env.ANTHROPIC_API_KEY) missingValues.push('ANTHROPIC_API_KEY');
  if (!process.env.ANTHROPIC_MODEL) missingValues.push('ANTHROPIC_MODEL');
  if (process.env.UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION !== requiredMutationValue) {
    missingValues.push('UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION');
  }
  if (!process.env.ENCRYPTION_KEY && !process.env.UNIROUTE_SMOKE_ENCRYPTION_KEY) {
    missingValues.push('ENCRYPTION_KEY or UNIROUTE_SMOKE_ENCRYPTION_KEY');
  }
  if (!process.env.API_KEY_HASH_SECRET && !process.env.UNIROUTE_SMOKE_API_KEY_HASH_SECRET && !process.env.JWT_SECRET) {
    missingValues.push('API_KEY_HASH_SECRET or UNIROUTE_SMOKE_API_KEY_HASH_SECRET or JWT_SECRET');
  }
  return missingValues.filter(value => requiredEnv.includes(value));
}

function runSeed() {
  const stdout = execFileSync('go', ['run', './scripts/prd4_real_upstream_seed.go'], {
    cwd: rootDir,
    env: process.env,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  const parsed = JSON.parse(stdout);
  if (!parsed.api_key || !parsed.api_key_prefix || !Array.isArray(parsed.providers) || parsed.providers.length !== 2) {
    throw new Error(`unexpected seed output: ${stdout}`);
  }
  return parsed;
}

async function assertBackendReady() {
  const response = await fetch(`${backendURL}/health/ready`);
  if (!response.ok) {
    throw new Error(`backend is not ready: ${response.status} ${await response.text()}`);
  }
}

async function apiLogin(username, password) {
  const response = await fetch(`${backendURL}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const body = await parseJSONResponse(response);
  if (!response.ok || !body?.token) {
    throw new Error(`login failed for ${username}: ${response.status} ${JSON.stringify(body)}`);
  }
  return body.token;
}

async function apiFetch(pathName) {
  const response = await fetch(`${backendURL}/api/v1${pathName}`, {
    headers: { authorization: `Bearer ${adminToken}` },
  });
  const body = await parseJSONResponse(response);
  if (!response.ok) {
    throw new Error(`API ${pathName} failed: ${response.status} ${JSON.stringify(body)}`);
  }
  return body;
}

async function runGatewayCase(provider, stream) {
  const requestID = `prd4-upstream-${provider.provider}-${stream ? 'stream' : 'nonstream'}-${Date.now()}`;
  const response = await fetch(`${backendURL}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${seed.api_key}`,
      'content-type': 'application/json',
      'x-request-id': requestID,
    },
    body: JSON.stringify({
      model: provider.model,
      messages: [{ role: 'user', content: 'UniRouter PRD4 real upstream evidence. Reply with one short sentence.' }],
      max_tokens: maxTokens,
      temperature: 0,
      stream,
    }),
  });
  const rawBody = await response.text();
  if (!response.ok) {
    throw new Error(`${provider.provider} ${stream ? 'stream' : 'nonstream'} gateway call failed: ${response.status} ${sanitizeText(rawBody)}`);
  }

  let responseSummary;
  if (stream) {
    if (!rawBody.includes('data: [DONE]') || !rawBody.includes('_uniroute')) {
      throw new Error(`${provider.provider} stream response missing [DONE] or _uniroute metadata`);
    }
    responseSummary = {
      mode: 'stream',
      saw_done: true,
      saw_uniroute_metadata: true,
      bytes: rawBody.length,
    };
  } else {
    const parsed = parseJSONString(rawBody, `${provider.provider} non-stream response`);
    if (!parsed._uniroute?.request_id) {
      throw new Error(`${provider.provider} non-stream response missing _uniroute.request_id`);
    }
    responseSummary = {
      mode: 'nonstream',
      saw_uniroute_metadata: true,
      response_model: parsed.model || '',
      choice_count: Array.isArray(parsed.choices) ? parsed.choices.length : 0,
    };
  }

  const log = await waitForCallLog(requestID, provider, stream);
  const trace = await apiFetch(`/logs/${log.id}/trace`);
  const stages = new Set((trace.data || []).map(event => event.event_type));
  for (const requiredEvent of ['provider.response_accepted', 'billing.settle.succeeded', 'logging.call_log.recorded']) {
    if (!stages.has(requiredEvent)) {
      throw new Error(`${provider.provider} ${responseSummary.mode} trace missing ${requiredEvent}`);
    }
  }

  const evidence = {
    provider: provider.provider,
    adapter_type: provider.adapter_type,
    channel_name: log.channel,
    model: log.model,
    requested_model: log.requested_model || log.model,
    request_id: requestID,
    stream,
    http_status: response.status,
    log_id: log.id,
    log_status_code: log.status_code,
    usage_source: log.usage_source,
    total_tokens: log.total_tokens,
	cost_micros: log.cost_micros,
	currency: log.currency,
	amount_scale: log.amount_scale,
    latency_ms: log.latency_ms,
    ttft_ms: log.ttft_ms,
    trace_events: Array.from(stages).sort(),
    response: responseSummary,
  };
  assertNoSecretLeak(evidence, [
    process.env.OPENROUTER_API_KEY,
    process.env.ANTHROPIC_API_KEY,
    seed.api_key,
  ]);
  return evidence;
}

async function waitForCallLog(requestID, provider, stream) {
  const deadline = Date.now() + 10000;
  let lastBody;
  while (Date.now() < deadline) {
    const body = await apiFetch(`/logs?request_id=${encodeURIComponent(requestID)}&project_id=${seed.project_id}`);
    lastBody = body;
    const row = (body.data || [])[0];
    if (row) {
      if (row.status_code < 200 || row.status_code >= 300) {
        throw new Error(`${provider.provider} call log recorded non-success status: ${JSON.stringify(row)}`);
      }
      if (row.provider !== provider.provider || row.model !== provider.model || row.is_stream !== stream) {
        throw new Error(`unexpected call log row for ${requestID}: ${JSON.stringify(row)}`);
      }
	  if (!/^\d+$/.test(String(row.cost_micros || '')) || BigInt(row.cost_micros) < 1n
		|| row.currency !== 'CNY' || Number(row.amount_scale) !== 1000000) {
        throw new Error(`expected settled cost for ${requestID}, got ${JSON.stringify(row)}`);
      }
      if (!['upstream', 'counter_v2', 'adapter_estimated'].includes(row.usage_source)) {
        throw new Error(`unexpected usage_source for ${requestID}: ${JSON.stringify(row)}`);
      }
      return row;
    }
    await delay(250);
  }
  throw new Error(`call log not found for ${requestID}: ${JSON.stringify(lastBody)}`);
}

async function parseJSONResponse(response) {
  const text = await response.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function parseJSONString(text, label) {
  try {
    return JSON.parse(text);
  } catch (err) {
    throw new Error(`${label} was not JSON: ${err.message}; body=${sanitizeText(text)}`);
  }
}

function assertNoSecretLeak(value, secrets) {
  const encoded = JSON.stringify(value);
  for (const secret of secrets) {
    if (secret && secret.length >= 8 && encoded.includes(secret)) {
      throw new Error('evidence contains a secret value');
    }
  }
}

function sanitizeText(text) {
  let result = String(text || '');
  for (const secret of [process.env.OPENROUTER_API_KEY, process.env.ANTHROPIC_API_KEY, seed?.api_key]) {
    if (secret) {
      result = result.split(secret).join('[redacted]');
    }
  }
  return result.slice(0, 2000);
}

function defaultEvidencePath() {
  const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, 'Z');
  return path.join(rootDir, 'docs', 'ops', 'evidence', `PRD4-B1H-real-upstream-evidence-${stamp}.json`);
}

function gitCommit() {
  try {
    return execFileSync('git', ['rev-parse', 'HEAD'], {
      cwd: rootDir,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
  } catch {
    return 'unknown';
  }
}

function trimTrailingSlash(value) {
  return String(value || '').replace(/\/+$/, '');
}

function boundedInt(value, min, max) {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) return min;
  return Math.max(min, Math.min(max, parsed));
}

function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}
