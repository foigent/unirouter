import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { pathToFileURL } from 'node:url';

const rootDir = process.cwd();
const { chromium } = await loadPlaywright();
const backendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_BACKEND_URL || process.env.BACKEND_URL || 'http://localhost:8080');
const frontendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_FRONTEND_URL || process.env.FRONTEND_URL || 'http://localhost:3000');
const headless = (process.env.UNIROUTE_SMOKE_HEADLESS || 'true') !== 'false';
const screenshotPath = path.join(rootDir, 'output', 'playwright', 'prd2-batch22-license.png');

let browser;
let page;
let seeded = false;

try {
  await assertBackendReady();
  const seed = runSeed('seed');
  seeded = true;
  const token = await apiLogin(seed.admin_username, seed.admin_password);

  const missing = await apiFetch(token, '/license');
  assertLicenseStatus(missing, 'missing');
  if (!missing.verifier_configured) {
    throw new Error(`backend LICENSE_PUBLIC_KEY is not configured. Restart backend with LICENSE_PUBLIC_KEY=${seed.public_key}`);
  }

  runSeed('store-invalid');
  const invalid = await apiFetch(token, '/license');
  assertLicenseStatus(invalid, 'invalid');

  const rejected = await apiFetch(token, '/license', {
    method: 'PUT',
    body: JSON.stringify({ license: seed.licenses.invalid }),
  }, 400);
  assertLicenseStatus(rejected.status, 'invalid');

  browser = await chromium.launch({ headless });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    ignoreHTTPSErrors: true,
  });
  await routeFrontendAPIToBackend(context);
  page = await context.newPage();

  await login(page, seed.admin_username, seed.admin_password);
  await page.goto(`${frontendURL}/license`, { waitUntil: 'networkidle' });
  await waitForAnyVisible(page.getByRole('heading', { name: 'License' }));
  await waitForAnyVisible(page.getByText('Invalid', { exact: true }));
  await expectBodyNotToContain(page, seed.licenses.invalid.signature, 'raw invalid license signature leaked in browser');

  await importLicenseInBrowser(page, seed.licenses.active, 'Active');
  const active = await apiFetch(token, '/license');
  assertLicenseStatus(active, 'active');

  await importLicenseInBrowser(page, seed.licenses.expiring, 'Expiring');
  const expiring = await apiFetch(token, '/license');
  assertLicenseStatus(expiring, 'expiring');
  const warningEvent = await assertLicenseWarningEvent(token, seed);

  await importLicenseInBrowser(page, seed.licenses.expired, 'Expired');
  const expired = await apiFetch(token, '/license');
  assertLicenseStatus(expired, 'expired');

  await assertLicenseAudit(token, seed);
  await expectBodyNotToContain(page, seed.licenses.active.signature, 'raw active license signature leaked in browser');
  await expectBodyNotToContain(page, seed.licenses.expiring.signature, 'raw expiring license signature leaked in browser');
  await expectBodyNotToContain(page, seed.licenses.expired.signature, 'raw expired license signature leaked in browser');

  fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });

  console.log(JSON.stringify({
    status: 'PASS',
    batch: 22,
    backend_url: backendURL,
    frontend_url: frontendURL,
    org_id: seed.org_id,
    user_id: seed.user_id,
    public_key: seed.public_key,
    active_deployment_id: seed.licenses.active.payload.deployment_id,
    expiring_deployment_id: seed.licenses.expiring.payload.deployment_id,
    expired_deployment_id: seed.licenses.expired.payload.deployment_id,
    warning_event_id: warningEvent.id,
    screenshot: screenshotPath,
  }, null, 2));
} catch (err) {
  if (page) {
    fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
    await page.screenshot({ path: screenshotPath, fullPage: true }).catch(() => {});
    const body = await page.locator('body').textContent({ timeout: 5000 }).catch(() => '');
    console.error('--- PRD2 Batch 22 smoke page body ---');
    console.error((body || '').slice(0, 4000));
    console.error('--- screenshot ---');
    console.error(screenshotPath);
  }
  throw err;
} finally {
  if (browser) {
    await browser.close();
  }
  if (seeded && process.env.UNIROUTE_BATCH22_KEEP_LICENSE !== 'true') {
    try {
      runSeed('clear');
    } catch (cleanupErr) {
      console.error(`failed to clear Batch 22 license fixture: ${cleanupErr.message}`);
    }
  }
}

function trimTrailingSlash(value) {
  return String(value || '').replace(/\/+$/, '');
}

async function loadPlaywright() {
  try {
    return await import('playwright');
  } catch (err) {
    const localModule = path.join(rootDir, 'frontend', 'node_modules', 'playwright', 'index.mjs');
    if (!fs.existsSync(localModule)) {
      throw new Error(`playwright dependency is not installed; run npm install --prefix frontend first (${err.message})`);
    }
    return import(pathToFileURL(localModule).href);
  }
}

function runSeed(action) {
  const stdout = execFileSync('go', ['run', './scripts/prd2_batch22_license_seed.go', '-action', action], {
    cwd: rootDir,
    env: process.env,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  return stdout.trim() ? JSON.parse(stdout) : null;
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
  if (!response.ok || !body.token) {
    throw new Error(`login failed for ${username}: ${response.status} ${JSON.stringify(body)}`);
  }
  return body.token;
}

async function apiFetch(token, pathName, options = {}, expectedStatus = 200) {
  const response = await fetch(`${backendURL}/api/v1${pathName}`, {
    ...options,
    headers: {
      'content-type': 'application/json',
      authorization: `Bearer ${token}`,
      ...(options.headers || {}),
    },
  });
  const body = await parseJSONResponse(response);
  if (response.status !== expectedStatus) {
    throw new Error(`API ${pathName} expected ${expectedStatus}, got ${response.status}: ${JSON.stringify(body)}`);
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

function assertLicenseStatus(status, expected) {
  if (!status || status.status !== expected) {
    throw new Error(`expected license status ${expected}, got ${JSON.stringify(status)}`);
  }
  if (expected === 'missing') {
    if (status.valid || status.license) {
      throw new Error(`missing license should not be valid or include metadata: ${JSON.stringify(status)}`);
    }
    return;
  }
  if (expected === 'invalid') {
    if (status.valid || status.signature_valid || !status.error) {
      throw new Error(`invalid license shape mismatch: ${JSON.stringify(status)}`);
    }
    return;
  }
  if (!status.signature_valid) {
    throw new Error(`expected signature-valid ${expected} license: ${JSON.stringify(status)}`);
  }
  if (expected === 'active' && (!status.valid || status.warning)) {
    throw new Error(`active license should be valid without warning: ${JSON.stringify(status)}`);
  }
  if (expected === 'expiring' && (!status.valid || !status.warning)) {
    throw new Error(`expiring license should be valid with warning=true: ${JSON.stringify(status)}`);
  }
  if (expected === 'expired' && status.valid) {
    throw new Error(`expired license must not be valid: ${JSON.stringify(status)}`);
  }
}

async function routeFrontendAPIToBackend(context) {
  const frontendOrigin = new URL(frontendURL).origin;
  const backendOrigin = new URL(backendURL).origin;
  if (frontendOrigin === backendOrigin) {
    return;
  }
  await context.route('**/*', async route => {
    const requestURL = new URL(route.request().url());
    const firstSegment = requestURL.pathname.split('/').filter(Boolean)[0];
    if (requestURL.origin === frontendOrigin && ['api', 'v1', 'health'].includes(firstSegment)) {
      requestURL.protocol = new URL(backendOrigin).protocol;
      requestURL.host = new URL(backendOrigin).host;
      await route.continue({ url: requestURL.toString() });
      return;
    }
    await route.continue();
  });
}

async function login(page, username, password) {
  await page.goto(`${frontendURL}/login`, { waitUntil: 'networkidle' });
  await page.getByPlaceholder('请输入用户名').fill(username);
  await page.getByPlaceholder('请输入密码').fill(password);
  await Promise.all([
    page.waitForURL(/\/(?:dashboard|logs|$)/, { timeout: 20000 }),
    page.getByRole('button', { name: '登录' }).click(),
  ]);
}

async function importLicenseInBrowser(page, license, expectedStatusText) {
  const textarea = page.getByPlaceholder('{"payload": {...}, "signature": "..."}');
  await textarea.fill(JSON.stringify(license, null, 2));
  const responsePromise = page.waitForResponse(response => {
    return response.url().includes('/api/v1/license') && response.request().method() === 'PUT';
  }, { timeout: 20000 });
  await page.getByRole('button', { name: '导入' }).click();
  const response = await responsePromise;
  if (!response.ok()) {
    throw new Error(`browser license import failed: ${response.status()} ${await response.text()}`);
  }
  await waitForAnyVisible(page.getByText(expectedStatusText, { exact: true }));
}

async function assertLicenseWarningEvent(token, seed) {
  const events = await apiFetch(token, '/notification-events?event_type=license.warning&page=1&page_size=20');
  const deploymentID = seed.licenses.expiring.payload.deployment_id;
  const match = (events.data || []).find(item => item.resource_id === deploymentID);
  if (!match) {
    throw new Error(`license.warning event not found for ${deploymentID}: ${JSON.stringify(events)}`);
  }
  if (match.severity !== 'warning' || match.resource !== 'license') {
    throw new Error(`license.warning event shape mismatch: ${JSON.stringify(match)}`);
  }
  if (match.payload?.deployment_id !== deploymentID || match.payload?.status !== 'expiring') {
    throw new Error(`license.warning payload mismatch: ${JSON.stringify(match)}`);
  }
  if (JSON.stringify(match).includes(seed.licenses.expiring.signature)) {
    throw new Error(`license.warning event leaked raw signature: ${JSON.stringify(match)}`);
  }
  return match;
}

async function assertLicenseAudit(token, seed) {
  const rows = await apiFetch(token, `/audit-logs?org_id=${seed.org_id}&action=license.update&resource=license&page=1&page_size=20`);
  const deployments = [
    seed.licenses.active.payload.deployment_id,
    seed.licenses.expiring.payload.deployment_id,
    seed.licenses.expired.payload.deployment_id,
  ];
  for (const deploymentID of deployments) {
    const row = (rows.data || []).find(item => item.resource_id === deploymentID);
    if (!row) {
      throw new Error(`license.update audit row not found for ${deploymentID}: ${JSON.stringify(rows)}`);
    }
    const serialized = JSON.stringify(row);
    for (const license of Object.values(seed.licenses)) {
      if (serialized.includes(license.signature)) {
        throw new Error(`license.update audit leaked raw signature for ${deploymentID}: ${serialized}`);
      }
    }
    if (serialized.includes('"signature":')) {
      throw new Error(`license.update audit included raw signature field for ${deploymentID}: ${serialized}`);
    }
  }
}

async function waitForAnyVisible(locator, timeout = 20000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const count = await locator.count().catch(() => 0);
    for (let i = 0; i < count; i++) {
      if (await locator.nth(i).isVisible().catch(() => false)) {
        return;
      }
    }
    await new Promise(resolve => setTimeout(resolve, 200));
  }
  throw new Error(`locator did not become visible within ${timeout}ms`);
}

async function expectBodyNotToContain(page, needle, message) {
  const body = await page.locator('body').textContent({ timeout: 5000 });
  if (needle && body && body.includes(needle)) {
    throw new Error(message);
  }
}
