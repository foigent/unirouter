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
const screenshotPath = path.join(rootDir, 'output', 'playwright', 'prd2-batch17-modelgov-usage.png');

let browser;
let page;

try {
  await assertBackendReady();
  const seed = runSeed();
  const adminToken = await apiLogin(seed.admin_username, seed.admin_password);
  const developerToken = await apiLogin(seed.developer_username, seed.developer_password);

  await assertModelGovernanceAPIs(adminToken, seed);
  await assertApprovedCatalogAPI(developerToken, seed);
  const projectAPIKey = await createProjectAPIKey(developerToken, seed);
  await assertRetiredGatewayBlocked(projectAPIKey, seed);
  const reconciliation = await assertReconciliationAPI(adminToken, seed);
  await assertEstimatedRatioNotification(adminToken, seed);

  browser = await chromium.launch({ headless });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    ignoreHTTPSErrors: true,
  });
  await routeFrontendAPIToBackend(context);
  page = await context.newPage();

  await login(page, seed.admin_username, seed.admin_password, /\/(?:dashboard|logs|$)/);
  await assertModelGovernanceBrowser(page, seed);
  await assertReconciliationBrowser(page);

  await clearBrowserAuth(page);
  await login(page, seed.developer_username, seed.developer_password, /\/developer(?:$|\?)/);
  await assertDeveloperCatalogBrowser(page, seed);

  fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });

  console.log(JSON.stringify({
    status: 'PASS',
    batch: 17,
    backend_url: backendURL,
    frontend_url: frontendURL,
    org_id: seed.org_id,
    project_id: seed.project_id,
    model_set_id: seed.model_set_id,
    retired_model: seed.retired_model,
    reconciliation_model: seed.reconciliation_model,
    reconciliation_summary: reconciliation.summary,
    screenshot: screenshotPath,
  }, null, 2));
} catch (err) {
  if (page) {
    fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
    await page.screenshot({ path: screenshotPath, fullPage: true }).catch(() => {});
    const body = await page.locator('body').textContent({ timeout: 5000 }).catch(() => '');
    console.error('--- PRD2 Batch 17 smoke page body ---');
    console.error((body || '').slice(0, 4000));
    console.error('--- screenshot ---');
    console.error(screenshotPath);
  }
  throw err;
} finally {
  if (browser) {
    await browser.close();
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

function runSeed() {
  const stdout = execFileSync('go', ['run', './scripts/prd2_batch17_modelgov_usage_seed.go'], {
    cwd: rootDir,
    env: process.env,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  return JSON.parse(stdout);
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

async function apiFetch(token, pathName, options = {}) {
  const response = await fetch(`${backendURL}/api/v1${pathName}`, {
    ...options,
    headers: {
      'content-type': 'application/json',
      authorization: `Bearer ${token}`,
      ...(options.headers || {}),
    },
  });
  const body = await parseJSONResponse(response);
  if (!response.ok) {
    throw new Error(`API ${pathName} failed: ${response.status} ${JSON.stringify(body)}`);
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

async function assertModelGovernanceAPIs(token, seed) {
  const createdName = `batch17-created-${Date.now()}`;
  const created = await apiFetch(token, '/model-registry', {
    method: 'POST',
    body: JSON.stringify({
      org_id: seed.org_id,
      model_name: createdName,
      display_name: 'Batch17 Created Model',
      provider_family: 'openai_compatible',
      lifecycle: 'staging',
      context_length: 8192,
      output_limit: 1024,
      capability_tags: 'chat,json',
      data_residency: 'global',
      approved_catalog: false,
    }),
  });
  if (!created.id) {
    throw new Error(`model registry create did not return id: ${JSON.stringify(created)}`);
  }
  await apiFetch(token, `/model-registry/${created.id}`, {
    method: 'PUT',
    body: JSON.stringify({
      lifecycle: 'ga',
      approved_catalog: true,
      migration_recommendation: 'Created by Batch 17 smoke',
    }),
  });
  const listed = await apiFetch(token, `/model-registry?q=${encodeURIComponent(createdName)}`);
  const found = (listed.data || []).find(item => item.model_name === createdName);
  if (!found || found.lifecycle !== 'ga' || found.approved_catalog !== true) {
    throw new Error(`model registry create/edit/list did not round-trip: ${JSON.stringify(listed)}`);
  }

  const assigned = await apiFetch(token, `/projects/${seed.project_id}/model-sets`);
  if (!(assigned.data || []).some(item => Number(item.id) === Number(seed.model_set_id))) {
    throw new Error(`seeded project model set assignment missing: ${JSON.stringify(assigned)}`);
  }
  await apiFetch(token, `/projects/${seed.project_id}/model-sets`, {
    method: 'PUT',
    body: JSON.stringify({ model_set_ids: [seed.model_set_id] }),
  });
}

async function assertApprovedCatalogAPI(token, seed) {
  const catalog = await apiFetch(token, `/model-catalog?project_id=${seed.project_id}`);
  const names = new Set((catalog.data || []).map(item => item.model_name));
  if (!names.has(seed.ga_model) || !names.has(seed.deprecated_model)) {
    throw new Error(`approved catalog missing GA/deprecated models: ${JSON.stringify(catalog)}`);
  }
  if (names.has(seed.unapproved_model) || names.has(seed.retired_model)) {
    throw new Error(`approved catalog exposed unapproved/retired models: ${JSON.stringify(catalog)}`);
  }
  const deprecated = (catalog.data || []).find(item => item.model_name === seed.deprecated_model);
  if (!deprecated || deprecated.migration_recommendation !== seed.migration_advice) {
    throw new Error(`deprecated migration recommendation missing from catalog: ${JSON.stringify(deprecated)}`);
  }
}

async function createProjectAPIKey(token, seed) {
  const created = await apiFetch(token, '/keys', {
    method: 'POST',
    body: JSON.stringify({
      org_id: seed.org_id,
      project_id: seed.project_id,
      name: `batch17-retired-${Date.now()}`,
      rpm_limit: 60,
      qps_limit: 10,
      allowed_models: '*',
    }),
  });
  if (!created.key || !created.key.startsWith('ur-')) {
    throw new Error(`project API key creation failed: ${JSON.stringify(created)}`);
  }
  return created.key;
}

async function assertRetiredGatewayBlocked(projectAPIKey, seed) {
  const response = await fetch(`${backendURL}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${projectAPIKey}`,
      'content-type': 'application/json',
      'x-request-id': `batch17-retired-${Date.now()}`,
    },
    body: JSON.stringify({
      model: seed.retired_model,
      messages: [{ role: 'user', content: 'retired model should be blocked' }],
      max_tokens: 16,
    }),
  });
  const body = await parseJSONResponse(response);
  if (response.status !== 403 || body?.error?.reason !== 'model_retired') {
    throw new Error(`retired model should be hard-blocked by gateway, got ${response.status}: ${JSON.stringify(body)}`);
  }
}

async function assertReconciliationAPI(token, seed) {
  const recon = await apiFetch(token, `/reconciliation?model=${encodeURIComponent(seed.reconciliation_model)}&page=1&page_size=2`);
  const summary = recon.summary || {};
  if (summary.estimated_ratio_scope !== 'filtered_query') {
    throw new Error(`reconciliation summary should be filtered_query, got ${JSON.stringify(summary)}`);
  }
  if (summary.estimated_tokens !== 80 || summary.upstream_tokens !== 20 || summary.estimated_ratio !== 0.8) {
    throw new Error(`unexpected reconciliation aggregate: ${JSON.stringify(summary)}`);
  }
  if (summary.estimated_usage_alert !== true) {
    throw new Error(`estimated usage alert was not raised: ${JSON.stringify(summary)}`);
  }
  if (recon.data.length !== 2 || Number(recon.total) < 3) {
    throw new Error(`pagination should not change full-query aggregate or total: ${JSON.stringify(recon)}`);
  }
  return recon;
}

async function assertEstimatedRatioNotification(token, seed) {
  for (let attempt = 0; attempt < 8; attempt += 1) {
    const events = await apiFetch(token, '/notification-events?event_type=token_metering.estimated_ratio.high&page=1&page_size=20');
    const match = (events.data || []).find(event => {
      const payload = event.payload || {};
      return payload.filters?.model === seed.reconciliation_model &&
        payload.estimated_ratio_scope === 'filtered_query' &&
        Number(payload.estimated_ratio) === 0.8;
    });
    if (match) {
      return;
    }
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error('high estimated-ratio notification event was not visible');
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

async function login(page, username, password, expectedURL) {
  await page.goto(`${frontendURL}/login`, { waitUntil: 'networkidle' });
  await page.getByPlaceholder('请输入用户名').fill(username);
  await page.getByPlaceholder('请输入密码').fill(password);
  await Promise.all([
    page.waitForURL(expectedURL, { timeout: 20000 }),
    page.getByRole('button', { name: '登录' }).click(),
  ]);
}

async function clearBrowserAuth(page) {
  await page.evaluate(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
  });
}

async function assertModelGovernanceBrowser(page, seed) {
  await page.goto(`${frontendURL}/model-governance`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: '模型治理' }).waitFor({ timeout: 20000 });
  await page.getByRole('heading', { name: 'Model Registry' }).waitFor({ timeout: 20000 });
  await page.getByPlaceholder('搜索模型、显示名或 Provider').fill(seed.ga_model);
  await page.getByText(seed.ga_display, { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText(seed.ga_model, { exact: true }).waitFor({ timeout: 20000 });
}

async function assertReconciliationBrowser(page) {
  await page.goto(`${frontendURL}/reconciliation`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Token 对账' }).waitFor({ timeout: 20000 });
  await page.getByText('估算占比', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('来源筛选', { exact: true }).waitFor({ timeout: 20000 });
}

async function assertDeveloperCatalogBrowser(page, seed) {
  await page.getByRole('heading', { name: '项目工作台' }).waitFor({ timeout: 20000 });
  await page.getByText('Approved Models', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText(seed.ga_display, { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText(seed.deprecated_display, { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText(seed.migration_advice, { exact: true }).waitFor({ timeout: 20000 });
  await expectBodyNotToContain(page, seed.unapproved_display, 'Developer Console exposed an unapproved model');
  await expectBodyNotToContain(page, seed.retired_display, 'Developer Console exposed a retired model');
}

async function expectBodyNotToContain(page, needle, message) {
  const body = await page.locator('body').textContent({ timeout: 10000 });
  if (body && body.includes(needle)) {
    throw new Error(message);
  }
}
