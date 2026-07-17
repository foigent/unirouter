import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import {
  buildAllowedCredentialOrigins,
  canonicalNavigationOrigin,
  fillFirstVisible,
  installMainFrameNavigationGuard,
  requireCredentialOrigin,
} from './prd4_real_sso_origin_guard.mjs';

const rootDir = process.cwd();
const requireFromFrontend = createRequire(path.join(rootDir, 'frontend', 'package.json'));

const backendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_BACKEND_URL || process.env.BACKEND_URL || 'http://localhost:8080');
const consoleURL = trimTrailingSlash(process.env.UNIROUTE_PRD4_SSO_CONSOLE_URL || process.env.UNIROUTE_SMOKE_FRONTEND_URL || process.env.FRONTEND_URL || '');
const issuerURL = trimTrailingSlash(process.env.UNIROUTE_PRD4_SSO_ISSUER_URL || process.env.OIDC_ISSUER_URL || '');
const providerKind = process.env.UNIROUTE_PRD4_SSO_PROVIDER_KIND || 'oidc';
const preflightOnly = process.env.UNIROUTE_PRD4_SSO_PREFLIGHT_ONLY === 'true' || process.argv.includes('--preflight');
const requiredApprovalValue = 'approved-target-real-sso';
const evidencePath = process.env.UNIROUTE_PRD4_SSO_EVIDENCE_PATH || defaultEvidencePath();
const headless = process.env.UNIROUTE_PRD4_SSO_HEADLESS !== 'false';

const missing = missingEnv();
if (preflightOnly) {
  console.log(JSON.stringify({
    status: missing.length === 0 ? 'READY' : 'SKIP',
    scope: 'PRD4 Batch 1I real SSO evidence harness preflight',
    backend_url: backendURL,
    console_url: consoleURL || null,
    issuer_url: issuerURL || null,
    provider_kind: providerKind,
    git_commit: gitCommit(),
    missing,
    required_approval_value: requiredApprovalValue,
    would_drive_browser: false,
    would_call_idp: false,
    would_read_database: false,
  }, null, 2));
  process.exit(0);
}
if (missing.length > 0) {
  throw new Error(`real SSO smoke is not configured; missing ${missing.join(', ')}`);
}
if (process.env.UNIROUTE_PRD4_SSO_ALLOW_BROWSER !== requiredApprovalValue) {
  throw new Error(`UNIROUTE_PRD4_SSO_ALLOW_BROWSER must equal ${requiredApprovalValue}`);
}

const startedAt = new Date().toISOString();
const backendConfig = await assertBackendOIDCConfig();
const discovery = await assertOIDCDiscovery();
const allowedCredentialOrigins = buildAllowedCredentialOrigins(
  discovery.authorization_endpoint,
  process.env.UNIROUTE_PRD4_SSO_ADDITIONAL_CREDENTIAL_ORIGINS,
);
const browserResult = await runBrowserSSO(backendConfig, allowedCredentialOrigins);
const me = await apiFetch('/me', browserResult.token);
assertExpectedProfile(me);

const audit = runAuditCheck(me.id);
const evidence = {
  status: 'PASS',
  batch: 'PRD4 Batch 1I',
  scope: 'real SSO evidence harness',
  started_at: startedAt,
  completed_at: new Date().toISOString(),
  git_commit: gitCommit(),
  backend_url: backendURL,
  console_url: consoleURL,
  provider_kind: providerKind,
  provider_name: backendConfig.provider_name,
  issuer_url: issuerURL,
  discovery: {
    issuer: discovery.issuer,
    authorization_endpoint_host: safeHost(discovery.authorization_endpoint),
    token_endpoint_host: safeHost(discovery.token_endpoint),
    jwks_uri_host: safeHost(discovery.jwks_uri),
  },
  login: {
    final_url_origin: safeOrigin(browserResult.finalURL),
    saw_login_button: browserResult.sawLoginButton,
    token_present: true,
  },
  user: {
    id: me.id,
    username: me.username,
    role: me.role,
    org_id: me.org_id,
    org_name: me.org_name,
    status: me.status,
  },
  audit,
};

assertNoSecretLeak(evidence, [
  process.env.UNIROUTE_PRD4_SSO_TEST_PASSWORD,
  process.env.OIDC_CLIENT_SECRET,
  process.env.UNIROUTE_PRD4_SSO_CLIENT_SECRET,
  browserResult.token,
]);

fs.mkdirSync(path.dirname(evidencePath), { recursive: true });
fs.writeFileSync(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`, { mode: 0o600 });
console.log(JSON.stringify({ ...evidence, evidence_path: evidencePath }, null, 2));

function missingEnv() {
  const missingValues = [];
  if (process.env.UNIROUTE_PRD4_SSO_ALLOW_BROWSER !== requiredApprovalValue) {
    missingValues.push('UNIROUTE_PRD4_SSO_ALLOW_BROWSER');
  }
  if (!consoleURL) missingValues.push('UNIROUTE_PRD4_SSO_CONSOLE_URL or UNIROUTE_SMOKE_FRONTEND_URL');
  if (!issuerURL) missingValues.push('UNIROUTE_PRD4_SSO_ISSUER_URL or OIDC_ISSUER_URL');
  if (!process.env.UNIROUTE_PRD4_SSO_TEST_USERNAME) missingValues.push('UNIROUTE_PRD4_SSO_TEST_USERNAME');
  if (!process.env.UNIROUTE_PRD4_SSO_TEST_PASSWORD) missingValues.push('UNIROUTE_PRD4_SSO_TEST_PASSWORD');
  if (!process.env.UNIROUTE_PRD4_SSO_DB_DSN && !process.env.DB_DSN) missingValues.push('UNIROUTE_PRD4_SSO_DB_DSN or DB_DSN');
  return missingValues;
}

async function assertBackendOIDCConfig() {
  const config = await fetchJSON(`${backendURL}/api/v1/auth/oidc/config`);
  if (!config?.enabled) {
    throw new Error(`backend OIDC config is not enabled: ${JSON.stringify(config)}`);
  }
  if (!config.login_url || !Array.isArray(config.scopes) || !config.scopes.includes('openid')) {
    throw new Error(`backend OIDC public config is incomplete: ${JSON.stringify(config)}`);
  }
  return config;
}

async function assertOIDCDiscovery() {
  const discovery = await fetchJSON(`${issuerURL}/.well-known/openid-configuration`);
  if (!discovery?.authorization_endpoint || !discovery?.token_endpoint || !discovery?.jwks_uri) {
    throw new Error(`OIDC discovery is missing required endpoints: ${JSON.stringify(discovery)}`);
  }
  if (discovery.issuer && trimTrailingSlash(discovery.issuer) !== issuerURL) {
    throw new Error(`OIDC issuer mismatch: ${discovery.issuer} != ${issuerURL}`);
  }
  return discovery;
}

async function runBrowserSSO(backendConfig, allowedCredentialOrigins) {
  const { chromium } = requireFromFrontend('playwright');
  const browser = await chromium.launch({ headless });
  const context = await browser.newContext();
  const page = await context.newPage();
  const allowedNavigationOrigins = new Set(allowedCredentialOrigins);
  allowedNavigationOrigins.add(canonicalNavigationOrigin(consoleURL, 'console URL'));
  allowedNavigationOrigins.add(canonicalNavigationOrigin(backendURL, 'backend URL'));
  const navigationGuard = await installMainFrameNavigationGuard(page, allowedNavigationOrigins);
  try {
    await page.goto(`${consoleURL}/login`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    const loginButton = page.getByRole('button', { name: /使用 .*登录/ }).last();
    await loginButton.waitFor({ state: 'visible', timeout: 15000 });
    await loginButton.click();
    navigationGuard.setAllowedOrigins(allowedCredentialOrigins);

    await fillFirstVisible(page, selectorList(process.env.UNIROUTE_PRD4_SSO_USERNAME_SELECTOR, [
      'input[name="username"]',
      'input[name="identifier"]',
      'input[type="email"]',
      'input[autocomplete="username"]',
      'input[autocomplete="email"]',
    ]), () => process.env.UNIROUTE_PRD4_SSO_TEST_USERNAME, 'username', allowedCredentialOrigins, navigationGuard);

    if (process.env.UNIROUTE_PRD4_SSO_AFTER_USERNAME_SELECTOR) {
      await page.locator(process.env.UNIROUTE_PRD4_SSO_AFTER_USERNAME_SELECTOR).first().click();
    }

    await fillFirstVisible(page, selectorList(process.env.UNIROUTE_PRD4_SSO_PASSWORD_SELECTOR, [
      'input[name="password"]',
      'input[type="password"]',
      'input[autocomplete="current-password"]',
    ]), () => process.env.UNIROUTE_PRD4_SSO_TEST_PASSWORD, 'password', allowedCredentialOrigins, navigationGuard);

    navigationGuard.assertAllowed();
    requireCredentialOrigin(page.url(), allowedCredentialOrigins, 'submit');
    navigationGuard.setAllowedOrigins(allowedNavigationOrigins);
    const submitSelector = process.env.UNIROUTE_PRD4_SSO_SUBMIT_SELECTOR || 'button[type="submit"]';
    await page.locator(submitSelector).first().click();

    await page.waitForFunction(() => Boolean(window.localStorage.getItem('token')), null, { timeout: 45000 });
    const token = await page.evaluate(() => window.localStorage.getItem('token'));
    if (!token) {
      throw new Error('SSO login completed without localStorage token');
    }
    return {
      token,
      finalURL: page.url(),
      providerName: backendConfig.provider_name,
      sawLoginButton: true,
    };
  } finally {
    try {
      await navigationGuard.dispose();
    } finally {
      await context.close();
      await browser.close();
    }
  }
}

async function apiFetch(pathName, token) {
  const response = await fetch(`${backendURL}/api/v1${pathName}`, {
    headers: { authorization: `Bearer ${token}` },
  });
  const body = await parseJSONResponse(response);
  if (!response.ok) {
    throw new Error(`API ${pathName} failed: ${response.status} ${JSON.stringify(body)}`);
  }
  return body;
}

function assertExpectedProfile(me) {
  if (!me?.id || !me.username || !me.role || !me.org_id) {
    throw new Error(`unexpected /me profile: ${JSON.stringify(me)}`);
  }
  if (process.env.UNIROUTE_PRD4_SSO_EXPECTED_ROLE && me.role !== process.env.UNIROUTE_PRD4_SSO_EXPECTED_ROLE) {
    throw new Error(`role mismatch: got ${me.role}, want ${process.env.UNIROUTE_PRD4_SSO_EXPECTED_ROLE}`);
  }
  if (process.env.UNIROUTE_PRD4_SSO_EXPECTED_ORG_ID && String(me.org_id) !== String(process.env.UNIROUTE_PRD4_SSO_EXPECTED_ORG_ID)) {
    throw new Error(`org_id mismatch: got ${me.org_id}, want ${process.env.UNIROUTE_PRD4_SSO_EXPECTED_ORG_ID}`);
  }
  if (process.env.UNIROUTE_PRD4_SSO_EXPECTED_USERNAME && me.username !== process.env.UNIROUTE_PRD4_SSO_EXPECTED_USERNAME) {
    throw new Error(`username mismatch: got ${me.username}, want ${process.env.UNIROUTE_PRD4_SSO_EXPECTED_USERNAME}`);
  }
}

function runAuditCheck(userID) {
  const stdout = execFileSync('go', ['run', './scripts/prd4_real_sso_audit_check.go'], {
    cwd: rootDir,
    env: {
      ...process.env,
      UNIROUTE_PRD4_SSO_USER_ID: String(userID),
      UNIROUTE_PRD4_SSO_EXPECTED_ISSUER: process.env.UNIROUTE_PRD4_SSO_EXPECTED_ISSUER || issuerURL,
    },
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  return JSON.parse(stdout);
}

async function fetchJSON(url) {
  const response = await fetch(url);
  const body = await parseJSONResponse(response);
  if (!response.ok) {
    throw new Error(`GET ${url} failed: ${response.status} ${JSON.stringify(body)}`);
  }
  return body;
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

function selectorList(primary, defaults) {
  if (primary) {
    return primary.split(',').map(item => item.trim()).filter(Boolean);
  }
  return defaults;
}

function assertNoSecretLeak(value, secrets) {
  const encoded = JSON.stringify(value);
  for (const secret of secrets) {
    if (secret && secret.length >= 8 && encoded.includes(secret)) {
      throw new Error('evidence contains a secret value');
    }
  }
}

function defaultEvidencePath() {
  const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, 'Z');
  return path.join(rootDir, 'docs', 'ops', 'evidence', `PRD4-B1I-real-sso-evidence-${stamp}.json`);
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

function safeHost(rawURL) {
  try {
    return new URL(rawURL).host;
  } catch {
    return '';
  }
}

function safeOrigin(rawURL) {
  try {
    return new URL(rawURL).origin;
  } catch {
    return '';
  }
}

function trimTrailingSlash(value) {
  return String(value || '').replace(/\/+$/, '');
}
