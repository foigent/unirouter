import { execFile } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import process from 'node:process';
import { pathToFileURL } from 'node:url';

const rootDir = process.cwd();
const { chromium } = await loadPlaywright();
const backendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_BACKEND_URL || process.env.BACKEND_URL || 'http://localhost:8080');
const frontendURL = trimTrailingSlash(process.env.UNIROUTE_SMOKE_FRONTEND_URL || process.env.FRONTEND_URL || 'http://localhost:3000');
const headless = (process.env.UNIROUTE_SMOKE_HEADLESS || 'true') !== 'false';
const screenshotPath = path.join(rootDir, 'output', 'playwright', 'prd2-batch19-notifications-budget.png');

let browser;
let page;
const receiver = await startWebhookReceiver();

try {
  await assertBackendReady();
  const seed = await runSeed(receiver.url);
  await receiver.waitForEvent(seed.event_id);

  const token = await apiLogin(seed.admin_username, seed.admin_password);
  await assertProjectBudgetAPI(token, seed);
  const event = await assertNotificationEventAPI(token, seed);
  await assertNotificationDeliveryAPI(token, seed, event.id);
  await assertNotificationWebhookAPI(token, seed);

  browser = await chromium.launch({ headless });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    ignoreHTTPSErrors: true,
  });
  await routeFrontendAPIToBackend(context);
  page = await context.newPage();

  await login(page, seed.admin_username, seed.admin_password, /\/(?:dashboard|logs|$)/);
  await assertNotificationCenterBrowser(page, seed);

  fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });

  console.log(JSON.stringify({
    status: 'PASS',
    batch: 19,
    backend_url: backendURL,
    frontend_url: frontendURL,
    org_id: seed.org_id,
    project_id: seed.project_id,
    event_id: seed.event_id,
    webhook_id: seed.webhook_id,
    webhook_receipts: receiver.receipts.length,
	budget_cents: seed.budget_cents,
	spent_micros: seed.spent_micros,
	currency: seed.currency,
	amount_scale: seed.amount_scale,
    screenshot: screenshotPath,
  }, null, 2));
} catch (err) {
  if (page) {
    fs.mkdirSync(path.dirname(screenshotPath), { recursive: true });
    await page.screenshot({ path: screenshotPath, fullPage: true }).catch(() => {});
    const body = await page.locator('body').textContent({ timeout: 5000 }).catch(() => '');
    console.error('--- PRD2 Batch 19 smoke page body ---');
    console.error((body || '').slice(0, 4000));
    console.error('--- screenshot ---');
    console.error(screenshotPath);
  }
  throw err;
} finally {
  if (browser) {
    await browser.close();
  }
  await receiver.close();
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

async function startWebhookReceiver() {
  const receipts = [];
  const waiters = [];
  const server = http.createServer((req, res) => {
    if (req.method !== 'POST') {
      res.statusCode = 405;
      res.end('method not allowed');
      return;
    }
    let body = '';
    req.setEncoding('utf8');
    req.on('data', chunk => { body += chunk; });
    req.on('end', () => {
      try {
        const parsed = JSON.parse(body || '{}');
        receipts.push(parsed);
        for (const waiter of waiters.splice(0)) {
          waiter();
        }
        res.statusCode = 204;
        res.end();
      } catch (err) {
        res.statusCode = 400;
        res.end(err.message);
      }
    });
  });

  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  return {
    receipts,
    url: `http://127.0.0.1:${address.port}/batch19-webhook`,
    async waitForEvent(eventID) {
      const deadline = Date.now() + 10000;
      while (Date.now() < deadline) {
        const match = receipts.find(item => Number(item.event_id) === Number(eventID));
        if (match) {
          if (match.event_type !== 'project_budget.threshold') {
            throw new Error(`unexpected webhook event type: ${JSON.stringify(match)}`);
          }
		  if (match.payload?.spent_micros !== '12000000' || match.payload?.currency !== 'CNY' || Number(match.payload?.amount_scale) !== 1000000) {
            throw new Error(`webhook payload did not include current-period spend: ${JSON.stringify(match)}`);
          }
          return match;
        }
        await new Promise(resolve => {
          waiters.push(resolve);
          setTimeout(resolve, 100);
        });
      }
      throw new Error(`webhook receiver did not receive event ${eventID}; receipts=${JSON.stringify(receipts)}`);
    },
    close: () => new Promise(resolve => server.close(resolve)),
  };
}

function runSeed(webhookURL) {
  return new Promise((resolve, reject) => {
    execFile('go', ['run', './scripts/prd2_batch19_notifications_budget_seed.go'], {
      cwd: rootDir,
      env: {
        ...process.env,
        UNIROUTE_BATCH19_WEBHOOK_URL: webhookURL,
      },
      encoding: 'utf8',
      maxBuffer: 1024 * 1024,
    }, (err, stdout, stderr) => {
      if (stderr) {
        process.stderr.write(stderr);
      }
      if (err) {
        reject(err);
        return;
      }
      try {
        resolve(JSON.parse(stdout));
      } catch (parseErr) {
        reject(new Error(`failed to parse seed output: ${parseErr.message}; stdout=${stdout}`));
      }
    });
  });
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

async function assertProjectBudgetAPI(token, seed) {
  const project = await apiFetch(token, `/projects/${seed.project_id}`);
  if (Number(project.budget_cents) !== seed.budget_cents || project.budget_period !== seed.budget_period) {
    throw new Error(`project budget API mismatch: ${JSON.stringify(project)}`);
  }
}

async function assertNotificationEventAPI(token, seed) {
  const events = await apiFetch(token, `/notification-events?event_type=project_budget.threshold&project_id=${seed.project_id}&page=1&page_size=20`);
  const match = (events.data || []).find(item => Number(item.id) === Number(seed.event_id));
  if (!match) {
    throw new Error(`budget notification event not found: ${JSON.stringify(events)}`);
  }
  if (match.delivery_state !== 'delivered' || match.severity !== 'critical') {
    throw new Error(`budget notification state mismatch: ${JSON.stringify(match)}`);
  }
	if (match.payload?.spent_micros !== seed.spent_micros || Number(match.payload?.budget_cents) !== seed.budget_cents
	  || match.payload?.currency !== seed.currency || Number(match.payload?.amount_scale) !== seed.amount_scale) {
    throw new Error(`budget notification payload mismatch: ${JSON.stringify(match)}`);
  }
  return match;
}

async function assertNotificationDeliveryAPI(token, seed, eventID) {
  const deliveries = await apiFetch(token, `/notification-deliveries?event_id=${eventID}&status=delivered&page=1&page_size=20`);
  const match = (deliveries.data || []).find(item => Number(item.event_id) === Number(eventID));
  if (!match) {
    throw new Error(`budget delivery not found: ${JSON.stringify(deliveries)}`);
  }
  if (Number(match.webhook_id) !== Number(seed.webhook_id) || Number(match.response_status) !== 204 || Number(match.attempts) < 1) {
    throw new Error(`budget delivery mismatch: ${JSON.stringify(match)}`);
  }
}

async function assertNotificationWebhookAPI(token, seed) {
  const webhooks = await apiFetch(token, `/notification-webhooks?project_id=${seed.project_id}&enabled=true&page=1&page_size=20`);
  const match = (webhooks.data || []).find(item => Number(item.id) === Number(seed.webhook_id));
  if (!match) {
    throw new Error(`budget webhook route not found: ${JSON.stringify(webhooks)}`);
  }
  const eventTypes = Array.isArray(match.event_types) ? match.event_types : [];
  if (match.name !== seed.webhook_name || !eventTypes.includes('project_budget.threshold')) {
    throw new Error(`budget webhook route mismatch: ${JSON.stringify(match)}`);
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

async function login(page, username, password, expectedURL) {
  await page.goto(`${frontendURL}/login`, { waitUntil: 'networkidle' });
  await page.getByPlaceholder('请输入用户名').fill(username);
  await page.getByPlaceholder('请输入密码').fill(password);
  await Promise.all([
    page.waitForURL(expectedURL, { timeout: 20000 }),
    page.getByRole('button', { name: '登录' }).click(),
  ]);
}

async function assertNotificationCenterBrowser(page, seed) {
  await page.goto(`${frontendURL}/notifications`, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: '通知中心' }).waitFor({ timeout: 20000 });
  await waitForAnyVisible(page.getByText('project_budget.threshold', { exact: true }));
  await waitForAnyVisible(page.getByText('Project budget threshold reached', { exact: true }));
  await waitForAnyVisible(page.getByText('delivered', { exact: true }));

  await clickTab(page, '投递');
  await waitForAnyVisible(page.getByText(seed.webhook_name, { exact: true }));
  await waitForAnyVisible(page.getByText('204', { exact: true }));

  await clickTab(page, 'Webhook');
  await waitForAnyVisible(page.getByText(seed.webhook_name, { exact: true }));
  await waitForAnyVisible(page.getByText(`Project ${seed.project_id}`, { exact: true }));
}

async function clickTab(page, name) {
  const byRole = page.getByRole('tab', { name });
  if (await byRole.count()) {
    await byRole.click();
    return;
  }
  await page.locator('.semi-tabs-tab').filter({ hasText: name }).first().click();
}

async function waitForAnyVisible(locator, timeout = 20000) {
  const deadline = Date.now() + timeout;
  let lastCount = 0;
  while (Date.now() < deadline) {
    lastCount = await locator.count();
    for (let index = 0; index < lastCount; index += 1) {
      if (await locator.nth(index).isVisible().catch(() => false)) {
        return;
      }
    }
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`no visible element matched locator after ${timeout}ms; count=${lastCount}`);
}
