import { execFileSync } from 'node:child_process';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { pathToFileURL } from 'node:url';

const rootDir = process.cwd();
const { chromium } = await loadPlaywright();
const backendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_BACKEND_URL || process.env.BACKEND_URL || 'http://localhost:8080');
const frontendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_FRONTEND_URL || process.env.FRONTEND_URL || 'http://localhost:3000');
const headless = (process.env.UNIROUTE_SMOKE_HEADLESS || 'true') !== 'false';
const screenshotPath = path.join(rootDir, 'output', 'playwright', 'prd2-batch23-product.png');
const invalidProjectKey = 'ur-0000-deadbeefdeadbeefdeadbeefdeadbeef';

const upstream = await startMockUpstream();
let browser;
let page;

try {
  await assertBackendReady();
  const seed = runSeed(upstream.url);
  const adminToken = await apiLogin(seed.admin_username, seed.admin_password);
  const developerToken = await apiLogin(seed.developer_username, seed.developer_password);

  await assertErrorDocsAPI(adminToken);
  await assertErrorDocsAPI(developerToken);
  await assertGatewayDocURL(seed);
  const analytics = await assertUsageAnalyticsAPI(adminToken, seed);
  const status = await assertInternalStatusAPI(adminToken, seed);

  browser = await chromium.launch({ headless });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1050 },
    ignoreHTTPSErrors: true,
  });
  await routeFrontendAPIToBackend(context);
  page = await context.newPage();

  await login(page, seed.developer_username, seed.developer_password, /\/developer(?:$|\?)/);
  await page.getByText('Playground', { exact: true }).waitFor({ timeout: 20000 });
  const projectAPIKey = await createProjectKey(page);
  await assertPlaygroundBrowser(page, projectAPIKey, seed);

  await clearBrowserAuth(page);
  await login(page, seed.admin_username, seed.admin_password, /\/(?:dashboard|logs|$)/);
  await assertProductPagesBrowser(page, seed);

  fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });

  console.log(JSON.stringify({
    status: 'PASS',
    batch: 23,
    backend_url: backendURL,
    frontend_url: frontendURL,
    org_id: seed.org_id,
    project_id: seed.project_id,
    model_alias: seed.model_alias,
    provider: seed.provider,
    upstream_calls: upstream.calls.length,
    analytics_summary: analytics.summary,
    internal_status: status.status,
    screenshot: screenshotPath,
  }, null, 2));
} catch (err) {
  if (page) {
    fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
    await page.screenshot({ path: screenshotPath, fullPage: true }).catch(() => {});
    const body = await page.locator('body').textContent({ timeout: 5000 }).catch(() => '');
    console.error('--- PRD2 Batch 23 smoke page body ---');
    console.error((body || '').slice(0, 4000));
    console.error('--- screenshot ---');
    console.error(screenshotPath);
  }
  throw err;
} finally {
  if (browser) {
    await browser.close();
  }
  await upstream.close();
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

function runSeed(upstreamURL) {
  const stdout = execFileSync('go', ['run', './scripts/prd2_batch23_product_seed.go'], {
    cwd: rootDir,
    env: {
      ...process.env,
      UNIROUTE_SMOKE_UPSTREAM_URL: upstreamURL,
    },
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  return JSON.parse(stdout);
}

async function startMockUpstream() {
  const calls = [];
  const server = http.createServer(async (req, res) => {
    const chunks = [];
    req.on('data', chunk => chunks.push(chunk));
    await once(req, 'end');
    const body = Buffer.concat(chunks).toString('utf8');
    calls.push({
      method: req.method,
      url: req.url,
      authorization: req.headers.authorization || '',
      requestID: req.headers['x-request-id'] || '',
      body,
    });

    if (req.method !== 'POST' || !String(req.url || '').endsWith('/v1/chat/completions')) {
      res.writeHead(404, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ error: { message: 'not found' } }));
      return;
    }
    if (req.headers.authorization !== 'Bearer upstream-batch23-product-key') {
      res.writeHead(401, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ error: { message: 'bad upstream auth' } }));
      return;
    }

    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({
      id: `chatcmpl-batch23-${Date.now()}`,
      object: 'chat.completion',
      created: Math.floor(Date.now() / 1000),
      model: 'batch23-product-chat',
      choices: [
        {
          index: 0,
          message: { role: 'assistant', content: 'batch23-playground-ok' },
          finish_reason: 'stop',
        },
      ],
      usage: {
        prompt_tokens: 12,
        completion_tokens: 5,
        total_tokens: 17,
      },
    }));
  });

  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  return {
    url: `http://127.0.0.1:${address.port}`,
    calls,
    close: () => new Promise(resolve => server.close(resolve)),
  };
}

function once(emitter, eventName) {
  return new Promise(resolve => emitter.once(eventName, resolve));
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

async function apiFetchText(token, pathName) {
  const response = await fetch(`${backendURL}/api/v1${pathName}`, {
    headers: { authorization: `Bearer ${token}` },
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`API ${pathName} failed: ${response.status} ${text}`);
  }
  return text;
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

async function assertErrorDocsAPI(token) {
  const docs = await apiFetch(token, '/error-docs');
  const codes = new Set((docs.data || []).map(item => item.code));
  for (const code of ['auth_error.invalid_api_key', 'auth_error.model_not_allowed', 'server_error.upstream_error']) {
    if (!codes.has(code)) {
      throw new Error(`error-docs missing ${code}: ${JSON.stringify(docs)}`);
    }
  }
}

async function assertGatewayDocURL(seed) {
  const response = await fetch(`${backendURL}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${invalidProjectKey}`,
      'content-type': 'application/json',
    },
    body: JSON.stringify({
      model: seed.model_alias,
      messages: [{ role: 'user', content: 'invalid key smoke' }],
    }),
  });
  const body = await parseJSONResponse(response);
  if (response.status !== 401 || body?.error?.doc_url !== '/docs#error-auth_error.invalid_api_key') {
    throw new Error(`invalid gateway key did not return doc_url: ${response.status} ${JSON.stringify(body)}`);
  }
}

async function assertUsageAnalyticsAPI(token, seed) {
  const pathName = `/usage/analytics?org_id=${seed.org_id}&project_id=${seed.project_id}&provider=${encodeURIComponent(seed.provider)}&group_by=usage_source`;
  const analytics = await apiFetch(token, pathName);
  const summary = analytics.summary || {};
  if (Number(summary.total_requests || 0) < 3) {
    throw new Error(`usage analytics summary did not include seeded rows: ${JSON.stringify(analytics)}`);
  }
  if (Number(summary.estimated_tokens || 0) <= 0 || Number(summary.upstream_tokens || 0) <= 0) {
    throw new Error(`usage analytics estimated/upstream summary missing: ${JSON.stringify(summary)}`);
  }
  const rows = analytics.data || [];
  for (const expected of ['upstream', 'counter_v2', 'upstream_failed']) {
    if (!rows.some(row => row.key === expected)) {
      throw new Error(`usage analytics missing ${expected} group: ${JSON.stringify(rows)}`);
    }
  }
  const csv = await apiFetchText(token, `${pathName}&format=csv`);
  if (!csv.includes('Usage source,total_requests') || !csv.includes('counter_v2')) {
    throw new Error(`usage analytics CSV missing expected content: ${csv}`);
  }
  return analytics;
}

async function assertInternalStatusAPI(token, seed) {
  const status = await apiFetch(token, `/internal-status?org_id=${seed.org_id}&project_id=${seed.project_id}`);
  if (status.status !== 'warning') {
    throw new Error(`internal status should be warning, got ${JSON.stringify(status)}`);
  }
  if (!Array.isArray(status.incidents) || !status.incidents.some(item => item.title === seed.incident_title)) {
    throw new Error(`internal status missing seeded incident: ${JSON.stringify(status)}`);
  }
  return status;
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

async function createProjectKey(page) {
  await page.getByRole('button', { name: '创建项目 Key' }).click();
  await page.getByPlaceholder('frontend-prod').fill(`batch23-product-${Date.now()}`);
  await page.getByRole('button', { name: '创建', exact: true }).click();
  await page.getByText('请立即保存完整项目 Key', { exact: true }).waitFor({ timeout: 20000 });
  const key = (await page.locator('.ur-secret-reveal code').textContent({ timeout: 10000 }) || '').trim();
  if (!/^ur-\d{4}-[a-f0-9]{32}$/.test(key)) {
    throw new Error(`created project API key has unexpected shape: ${key}`);
  }
  await page.getByRole('button', { name: '我已保存' }).click();
  await page.getByText('请立即保存完整项目 Key', { exact: true }).waitFor({ state: 'hidden', timeout: 10000 });
  return key;
}

async function assertPlaygroundBrowser(page, projectAPIKey, seed) {
  await page.getByPlaceholder('粘贴项目 API Key').fill(projectAPIKey);
  await page.getByPlaceholder('输入本次测试 Prompt').fill(`Batch 23 playground prompt ${Date.now()}`);
  await page.getByRole('button', { name: '运行' }).click();
  await page.getByText('batch23-playground-ok', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('17 tokens', { exact: true }).waitFor({ timeout: 20000 });

  if (!upstream.calls.some(call => (
    call.authorization === 'Bearer upstream-batch23-product-key'
    && (call.body.includes(seed.model_alias) || call.body.includes(seed.model_name))
  ))) {
    throw new Error('mock upstream did not receive the Playground gateway call');
  }

  await page.getByPlaceholder('粘贴项目 API Key').fill(invalidProjectKey);
  await page.getByRole('button', { name: '运行' }).click();
  await page.getByText('Invalid API key', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('查看错误文档', { exact: true }).waitFor({ timeout: 20000 });
}

async function clearBrowserAuth(page) {
  await page.evaluate(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
  });
}

async function assertProductPagesBrowser(page, seed) {
  await page.goto(`${frontendURL}/usage-analytics`, { waitUntil: 'networkidle' });
  await page.getByText('用量分析', { exact: true }).first().waitFor({ timeout: 20000 });
  await page.getByText('筛选与分组', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('钻取结果', { exact: true }).waitFor({ timeout: 20000 });

  await page.goto(`${frontendURL}/status`, { waitUntil: 'networkidle' });
  await page.getByText('内部状态', { exact: true }).first().waitFor({ timeout: 20000 });
  await page.getByText(seed.incident_title, { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('warning', { exact: true }).first().waitFor({ timeout: 20000 });

  await page.goto(`${frontendURL}/docs`, { waitUntil: 'networkidle' });
  await page.getByText('Error Docs', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('auth_error.invalid_api_key', { exact: true }).waitFor({ timeout: 20000 });
}
