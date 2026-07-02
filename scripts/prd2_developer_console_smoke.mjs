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
const requestID = `batch16-browser-${Date.now()}`;
const privatePrompt = `Batch 16 private prompt ${requestID}`;
const screenshotPath = path.join(rootDir, 'output', 'playwright', 'prd2-batch16-developer-console.png');

const upstream = await startMockUpstream();
let browser;
let page;

try {
  await assertBackendReady();
  const seed = runSeed(upstream.url);
  browser = await chromium.launch({ headless });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    ignoreHTTPSErrors: true,
  });
  await routeFrontendAPIToBackend(context);
  page = await context.newPage();

  await login(page, seed);
  await assertDeveloperConsoleLoaded(page, seed);
  const projectAPIKey = await createProjectKey(page);
  await callGateway(projectAPIKey, seed);
  await assertUsageVisible(page, seed, projectAPIKey);
  await assertProjectIsolation(page, seed);
  await assertAdminSurfacesBlocked(page);

  fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });

  console.log(JSON.stringify({
    status: 'PASS',
    backend_url: backendURL,
    frontend_url: frontendURL,
    project_id: seed.project_id,
    other_project_id: seed.other_project_id,
    request_id: requestID,
    model_alias: seed.model_alias,
    upstream_calls: upstream.calls.length,
    screenshot: screenshotPath,
  }, null, 2));
} catch (err) {
  if (page) {
    fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
    await page.screenshot({ path: screenshotPath, fullPage: true }).catch(() => {});
    const body = await page.locator('body').textContent({ timeout: 5000 }).catch(() => '');
    console.error('--- PRD2 Batch 16 smoke page body ---');
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
  const stdout = execFileSync('go', ['run', './scripts/prd2_developer_console_seed.go'], {
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
    if (req.headers.authorization !== 'Bearer upstream-browser-smoke-key') {
      res.writeHead(401, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ error: { message: 'bad upstream auth' } }));
      return;
    }

    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({
      id: `chatcmpl-${Date.now()}`,
      object: 'chat.completion',
      created: Math.floor(Date.now() / 1000),
      model: 'batch16-smoke-gpt-4o',
      choices: [
        {
          index: 0,
          message: { role: 'assistant', content: 'batch16-ok' },
          finish_reason: 'stop',
        },
      ],
      usage: {
        prompt_tokens: 10,
        completion_tokens: 5,
        total_tokens: 15,
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

async function login(page, seed) {
  await page.goto(`${frontendURL}/login`, { waitUntil: 'networkidle' });
  await page.getByPlaceholder('请输入用户名').fill(seed.username);
  await page.getByPlaceholder('请输入密码').fill(seed.password);
  await Promise.all([
    page.waitForURL(/\/developer(?:$|\?)/, { timeout: 20000 }),
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

async function assertDeveloperConsoleLoaded(page, seed) {
  await page.getByText('项目工作台', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('Developer Console', { exact: true }).first().waitFor({ timeout: 20000 });
  await page.getByText('当前项目', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText(`Project ID: ${seed.project_id}`, { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('Approved Models', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText('GPT-4o', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByText(seed.model_alias, { exact: true }).first().waitFor({ timeout: 20000 });
  await expectBodyNotToContain(page, seed.other_project_slug, 'developer console leaked another project');
}

async function createProjectKey(page) {
  await page.getByRole('button', { name: '创建项目 Key' }).click();
  await page.getByPlaceholder('frontend-prod').fill(`batch16-browser-${Date.now()}`);
  await page.getByRole('button', { name: '创建', exact: true }).click();
  await page.getByText('请立即保存完整项目 Key', { exact: true }).waitFor({ timeout: 20000 });
  const key = (await page.locator('.ur-secret-reveal code').textContent({ timeout: 10000 }) || '').trim();
  if (!/^ur-\d{4}-[a-f0-9]{32}$/.test(key)) {
    throw new Error(`created project API key has unexpected shape: ${key}`);
  }
  await page.getByRole('button', { name: '我已保存' }).click();
  await page.getByText('请立即保存完整项目 Key', { exact: true }).waitFor({ state: 'hidden', timeout: 10000 });
  await expectBodyNotToContain(page, key, 'full project API key remained visible after closing modal');
  return key;
}

async function callGateway(projectAPIKey, seed) {
  const response = await fetch(`${backendURL}/v1/chat/completions`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${projectAPIKey}`,
      'content-type': 'application/json',
      'x-request-id': requestID,
    },
    body: JSON.stringify({
      model: seed.model_alias,
      messages: [{ role: 'user', content: privatePrompt }],
      max_tokens: 16,
    }),
  });
  const body = await response.text();
  if (!response.ok) {
    throw new Error(`gateway call failed: ${response.status} ${body}`);
  }
  const parsed = JSON.parse(body);
  if (parsed?._uniroute?.request_id !== requestID) {
    throw new Error(`gateway response missing expected _uniroute request_id: ${body}`);
  }
  if (!upstream.calls.some(call => call.requestID === requestID && call.authorization === 'Bearer upstream-browser-smoke-key')) {
    throw new Error('mock upstream did not receive the gateway call with decrypted upstream key');
  }
}

async function assertUsageVisible(page, seed, projectAPIKey) {
  await page.getByRole('button', { name: '刷新' }).click();
  await page.getByText(requestID).waitFor({ timeout: 20000 });

  await page.getByPlaceholder('req-...').fill(requestID);
  await page.getByRole('button', { name: '查询' }).click();
  const lookupGrid = page.locator('.ur-dev-lookup-grid');
  await lookupGrid.getByText('Resolved', { exact: true }).waitFor({ timeout: 20000 });
  await lookupGrid.getByText(seed.model_alias, { exact: true }).waitFor({ timeout: 20000 });
  await lookupGrid.getByText(seed.model_name, { exact: true }).waitFor({ timeout: 20000 });
  await lookupGrid.getByText('batch16-mock', { exact: true }).waitFor({ timeout: 20000 });
  await lookupGrid.getByText('15', { exact: true }).waitFor({ timeout: 20000 });

  await expectBodyNotToContain(page, 'upstream-browser-smoke-key', 'upstream provider key leaked into Developer Console');
  await expectBodyNotToContain(page, projectAPIKey, 'full project API key leaked into Developer Console');
  await expectBodyNotToContain(page, privatePrompt, 'prompt content leaked into Request Lookup');
  await expectBodyNotToContain(page, 'batch16-ok', 'response content leaked into Request Lookup');
}

async function assertProjectIsolation(page, seed) {
  const token = await page.evaluate(() => window.localStorage.getItem('token'));
  if (!token) {
    throw new Error('missing JWT token after login');
  }
  const crossProjectLogs = await fetch(`${backendURL}/api/v1/logs?project_id=${seed.other_project_id}`, {
    headers: { authorization: `Bearer ${token}` },
  });
  if (crossProjectLogs.status !== 403) {
    throw new Error(`cross-project logs should be forbidden, got ${crossProjectLogs.status}: ${await crossProjectLogs.text()}`);
  }
  const adminAPI = await fetch(`${backendURL}/api/v1/channels`, {
    headers: { authorization: `Bearer ${token}` },
  });
  if (adminAPI.status !== 403) {
    throw new Error(`developer direct admin channels API should be forbidden, got ${adminAPI.status}: ${await adminAPI.text()}`);
  }
}

async function assertAdminSurfacesBlocked(page) {
  await page.goto(`${frontendURL}/channels`, { waitUntil: 'networkidle' });
  await page.waitForURL(/\/developer(?:$|\?)/, { timeout: 20000 });
  await page.getByText('项目工作台', { exact: true }).waitFor({ timeout: 20000 });
  await expectBodyNotToContain(page, '渠道管理', 'developer could see channel management route');
}

async function expectBodyNotToContain(page, needle, message) {
  const body = await page.locator('body').textContent({ timeout: 10000 });
  if (body && body.includes(needle)) {
    throw new Error(message);
  }
}
