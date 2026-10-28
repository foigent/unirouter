import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const fixture = JSON.parse(process.env.OVERVIEW_BROWSER_FIXTURE || '{}');
const emptyFixture = JSON.parse(process.env.OVERVIEW_EMPTY_FIXTURE || '{}');
assert.ok(fixture.base_url && fixture.empty_url && fixture.unavailable_url && fixture.ambiguous_url && fixture.selection_url);
assert.ok(fixture.rows.length > 50 && fixture.deployment_id > 0);
const allowed = ['platform_admin', 'org_admin', 'billing_admin', 'org_viewer'];
const en = {
  request: 'Requests by model', cost: 'Costs by model', inspector: 'Model details',
  trend: 'Requests and costs', quality: 'Model execution quality', recent: 'Recent executions',
  settlement: 'Settlement & notifications', refresh: 'Refresh overview', reload: 'Reload overview',
  error: 'Overview could not be loaded', unavailable: 'Unavailable',
  range: { 1: 'Last 24 hours', 7: 'Last 7 days', 30: 'Last 30 days' },
};
const zh = {
  request: '模型调用构成', cost: '模型费用构成', inspector: '模型详情',
  trend: '请求与费用趋势', quality: '模型运行质量', recent: '近期执行请求',
  settlement: '结算与通知', refresh: '刷新总览', reload: '重新加载总览',
  error: '总览数据加载失败', unavailable: '读取失败',
  range: { 1: '最近 24 小时', 7: '最近 7 天', 30: '最近 30 天' },
};
const artifactRoot = process.env.OVERVIEW_BROWSER_ARTIFACTS;
assert.ok(artifactRoot && path.isAbsolute(artifactRoot), 'explicit task-owned artifact directory required');
await mkdir(artifactRoot, { mode: 0o700 });
const screenshots = new Set();
let browser;
const contexts = new Set();
const cases = [];
const pageErrors = [];
let browserClosed = false;
let closing;
function closeOwnedBrowser() {
  closing ||= (async () => {
    await Promise.allSettled([...contexts].map(async context => { await context.close(); contexts.delete(context); }));
    if (browser && !browserClosed) { await browser.close(); browserClosed = true; contexts.clear(); }
  })();
  return closing;
}
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, async () => {
  try { await closeOwnedBrowser(); } finally {
    console.log(`OVERVIEW_BROWSER_RECEIPT ${JSON.stringify({ signal, browser_closed: browserClosed, owned_contexts_remaining: contexts.size })}`);
    process.exit(130);
  }
});

async function paint(page) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
}
function isStats(response) {
  return new URL(response.url()).pathname === '/api/v1/dashboard/stats' && response.request().method() === 'GET';
}
function daysFor(url) {
  const query = new URL(url).searchParams;
  const start = Date.parse(query.get('start_time'));
  const end = Date.parse(query.get('end_time'));
  assert.ok(Number.isFinite(start) && Number.isFinite(end) && start < end);
  return (end - start) / 86400000;
}
function waitStats(page, days, status = 200) {
  return page.waitForResponse(response => isStats(response) && daysFor(response.url()) === days && response.status() === status);
}
async function unpack(response) { return { url: response.url(), status: response.status(), data: await response.json() }; }
function section(page, title) {
  return page.locator('.ur-surface').filter({ has: page.getByRole('heading', { name: title, exact: true }) });
}
async function ready(page) {
  await page.locator('.ur-overview[aria-busy="false"] .ur-composition').first().waitFor({ state: 'visible' });
  assert.equal(await page.locator('.ur-composition').count(), 2);
  await paint(page);
}
function expectedMoney(value) {
  // Independent integer unit oracle, never importing the production formatter.
  const amount = BigInt(value), whole = amount / 1000000n, rem = amount % 1000000n;
  return `¥${whole}.${rem % 10000n === 0n ? (rem / 10000n).toString().padStart(2, '0') : rem.toString().padStart(6, '0')}`;
}
function percentile(values, fraction) {
  if (!values.length) return 0;
  const ordered = [...values].sort((a, b) => a - b);
  const pos = fraction * (ordered.length - 1), low = Math.floor(pos), high = Math.ceil(pos);
  return ordered[low] + (ordered[high] - ordered[low]) * (pos - low);
}
function expectedWindow(raw, url) {
  const query = new URL(url).searchParams;
  const start = Date.parse(query.get('start_time')), end = Date.parse(query.get('end_time'));
  const rows = raw.rows.filter(row => row.org_id === raw.org_id && Date.parse(row.created_at) >= start && Date.parse(row.created_at) < end);
  const groups = new Map(), trend = new Map();
  const bucketWidth = end - start <= 2 * 86400000 ? 3600000 : 86400000;
  // UTC buckets cover the full requested window, including empty edge buckets.
  // Request inclusion still uses the strict half-open interval above.
  for (let bucket = Math.floor(start / bucketWidth) * bucketWidth; bucket <= Math.floor(end / bucketWidth) * bucketWidth; bucket += bucketWidth) {
    trend.set(bucket, { requests: 0, cost: 0n });
  }
  for (const row of rows) {
    const model = row.model.trim() || row.requested_model.trim() || '(unknown)';
    const group = groups.get(model) || { requests: 0, cost: 0n, successes: 0, latencies: [] };
    group.requests++; group.cost += BigInt(row.cost_micros);
    if (row.status_code < 400) group.successes++;
    group.latencies.push(row.latency_ms); groups.set(model, group);
    const bucket = Math.floor(Date.parse(row.created_at) / bucketWidth) * bucketWidth;
    const point = trend.get(bucket) || { requests: 0, cost: 0n };
    point.requests++; point.cost += BigInt(row.cost_micros); trend.set(bucket, point);
  }
  return { rows, groups, trend, total: rows.length, cost: rows.reduce((sum, row) => sum + BigInt(row.cost_micros), 0n) };
}
function validateAPI(result, raw, role) {
  assert.equal(result.status, 200);
  const { data } = result, expected = expectedWindow(raw, result.url);
  const query = new URL(result.url).searchParams;
  assert.equal(query.get('org_id'), role === 'platform_admin' ? String(raw.org_id) : null);
  assert.equal(data.currency, 'CNY'); assert.equal(data.amount_scale, 1000000);
  assert.equal(data.total_requests, expected.total);
  assert.equal(data.today_cost_micros, expected.cost.toString());
  assert.equal(data.model_distribution.length, expected.groups.size);
  for (const row of data.model_distribution) {
    const group = expected.groups.get(row.model); assert.ok(group, `unexpected model ${row.model}`);
    assert.equal(row.requests, group.requests); assert.equal(row.cost_micros, group.cost.toString());
    assert.equal(row.success_count, group.successes); assert.equal(row.fail_count, group.requests - group.successes);
    assert.ok(Math.abs(row.avg_latency_ms - group.latencies.reduce((a, b) => a + b, 0) / group.requests) < 1e-8);
    assert.ok(Math.abs(row.p95_latency_ms - percentile(group.latencies, 0.95)) < 1e-8);
  }
  assert.ok(Math.abs(data.success_rate - (expected.total ? expected.rows.filter(row => row.status_code < 400).length / expected.total : 0)) < 1e-8);
  assert.ok(Math.abs(data.p95_latency_ms - percentile(expected.rows.map(row => row.latency_ms), 0.95)) < 1e-8);
  assert.ok(Math.abs(data.p99_latency_ms - percentile(expected.rows.map(row => row.latency_ms), 0.99)) < 1e-8);
  assert.equal(data.daily_trend.length, expected.trend.size);
  assert.deepEqual(data.daily_trend.map(point => Date.parse(point.date)), [...expected.trend.keys()]);
  assert.equal(data.daily_trend.reduce((sum, point) => sum + point.requests, 0), expected.total);
  assert.equal(data.daily_trend.reduce((sum, point) => sum + BigInt(point.cost_micros), 0n), expected.cost);
  for (const point of data.daily_trend) {
    const reference = expected.trend.get(Date.parse(point.date)); assert.ok(reference, `unexpected trend bucket ${point.date}`);
    assert.equal(point.requests, reference.requests); assert.equal(point.cost_micros, reference.cost.toString());
  }
  const walletAllowed = ['platform_admin', 'billing_admin'].includes(role);
  for (const key of ['wallet_balance_micros', 'wallet_granted_micros', 'wallet_frozen_micros', 'wallet_threshold_micros']) assert.equal(Object.hasOwn(data, key), walletAllowed);
  if (walletAllowed) {
    assert.equal(data.wallet_balance_micros, raw.wallet.balance || '0');
    assert.equal(data.wallet_granted_micros, raw.wallet.granted || '0');
    assert.equal(data.wallet_frozen_micros, raw.wallet.frozen || '0');
    assert.equal(data.wallet_threshold_micros, (BigInt(raw.wallet.threshold_cents || 0) * 10000n).toString());
  }
  assert.equal(Object.hasOwn(data, 'model_prices'), role !== 'org_viewer');
  assert.equal(Object.hasOwn(data, 'channel_health'), role === 'platform_admin');
  if (role === 'platform_admin' && raw.deployment_id) {
    assert.ok(data.channel_health.some(row => row.deployment_id === raw.deployment_id && row.model_operation_id === raw.operation_id));
  }
  const recent = raw.rows.filter(row => row.org_id === raw.org_id).sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at)).slice(0, 10);
  assert.equal(data.recent_logs.length, recent.length);
  for (let i = 0; i < recent.length; i++) {
    const row = data.recent_logs[i], reference = recent[i];
    assert.equal(row.request_id, reference.id); assert.equal(row.cost_micros, reference.cost_micros);
    assert.equal(row.model, reference.requested_model.trim() || reference.model.trim() || '(unknown)');
    if (reference.id === 'overview-current-00') {
      assert.equal(row.route_identity, 'canonical_deployment_operation');
      assert.equal(row.route_deployment_id, raw.deployment_id); assert.equal(row.route_model_operation_id, raw.operation_id);
    }
  }
  return expected;
}

async function installGuard(context, origin, traps) {
  await context.route('**/*', async route => {
    const url = new URL(route.request().url());
    const traffic = url.origin === 'https://provider.pr4.invalid' ? 'provider'
      : url.origin === 'https://feed.pr4.invalid' || (url.origin === origin && url.pathname === '/api/v1/integrations/xinsight/completed-executions') ? 'feed'
      : url.origin !== origin ? 'upstream' : '';
    if (!traffic) { await route.continue(); return; }
    const observed = await context.request.get(`${traps[traffic]}/observed`);
    assert.equal(observed.status(), 418);
    await route.fulfill({ status: 502, contentType: 'text/plain', body: 'fixture egress blocked' });
  });
}
async function contextAt(baseURL, traps = fixture.trap_urls) {
  const context = await browser.newContext({ baseURL, viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' });
  contexts.add(context); await installGuard(context, new URL(baseURL).origin, traps); return context;
}
async function closeContext(context) { await context.close(); contexts.delete(context); }
async function login(role, baseURL = fixture.base_url, raw = fixture) {
  const context = await contextAt(baseURL), page = await context.newPage();
  page.setDefaultTimeout(15000);
  const traffic = { stats: 0, mutations: [], errors: [] };
  page.on('pageerror', error => pageErrors.push(error.message));
  page.on('request', request => {
    const path = new URL(request.url()).pathname;
    if (path === '/api/v1/dashboard/stats' && request.method() === 'GET') traffic.stats++;
    if (path.startsWith('/api/') && !['GET', 'HEAD'].includes(request.method()) && !['/api/v1/auth/login', '/api/v1/auth/logout'].includes(path)) traffic.mutations.push(`${request.method()} ${path}`);
  });
  await page.goto('/login');
  assert.equal(await page.locator('html').getAttribute('lang'), 'en-US');
  await page.getByRole('button', { name: '中文', exact: true }).click();
  await page.getByLabel('用户名', { exact: true }).fill(raw.users[role]);
  await page.locator('input[type="password"]').fill(raw.password);
  await page.getByRole('button', { name: 'English', exact: true }).click();
  assert.ok((await page.getByLabel('Username', { exact: true }).inputValue()) === raw.users[role], 'login username draft was lost');
  assert.ok((await page.locator('input[type="password"]').inputValue()) === raw.password, 'login password draft was lost');
  await page.getByRole('button', { name: '中文', exact: true }).click();
  const profile = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/me' && r.status() === 200);
  const initial = allowed.includes(role) && ![fixture.unavailable_url, fixture.ambiguous_url].includes(baseURL) ? waitStats(page, 1) : null;
  const authResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/auth/login');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  assert.equal((await authResponse).status(), 200); await profile;
  await page.waitForURL(url => url.pathname !== '/login');
  return { context, page, traffic, initial: initial ? await unpack(await initial) : null };
}
async function authHeaders(page) { return { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('token'))}` }; }
async function apiGet(item, path) { return item.context.request.get(path, { headers: await authHeaders(item.page) }); }
async function control(item, action) {
  const response = await item.context.request.post('/__overview_fixture/control', { headers: await authHeaders(item.page), data: { action } });
  assert.equal(response.status(), 204);
}
async function language(page, locale) {
  const buttonName = locale === 'en-US' ? 'English' : '中文';
  const menu = page.locator('.mobile-menu-toggle');
  if (await menu.isVisible()) {
    assert.equal(await menu.getAttribute('aria-expanded'), 'false');
    await menu.click();
    await page.locator('.ur-sider.open .ur-mobile-language').getByRole('button', { name: buttonName, exact: true }).click();
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => document.querySelector('.mobile-menu-toggle')?.getAttribute('aria-expanded') === 'false');
    await page.locator('.ur-sider').evaluate(element => Promise.all(element.getAnimations().map(animation => animation.finished)));
    const drawer = await page.locator('.ur-sider').boundingBox();
    assert.ok(drawer && drawer.x + drawer.width <= 0.5, 'mobile navigation drawer did not finish closing');
  } else {
    await page.getByRole('toolbar').getByRole('button', { name: buttonName, exact: true }).click();
  }
  await page.waitForFunction(expected => document.documentElement.lang === expected, locale); await paint(page);
}
async function inspectCurrentAvailability(page, result, locale) {
  const english = locale === 'en-US';
  const channels = result.data.channel_health;
  assert.ok(Array.isArray(channels), 'platform current availability must remain available independently of window samples');
  const metric = page.locator('.ur-overview-kpis .ur-metric').filter({ has: page.getByText(english ? 'Routable deployment operations' : '当前可路由部署操作', { exact: true }) });
  assert.equal(await metric.count(), 1);
  assert.equal((await metric.locator('.ur-metric-value').innerText()).trim(), `${channels.filter(row => row.available === true).length}/${channels.length}`);
  await metric.getByText(english ? 'Current state · Not date-filtered' : '当前状态 · 不随时间筛选', { exact: true }).waitFor({ state: 'visible' });
  await section(page, english ? 'Deployment availability' : '模型部署可用状态').waitFor({ state: 'visible' });
}
async function inspectEmptyWindow(item, result, locale) {
  const { page } = item, labels = locale === 'en-US' ? en : zh;
  assert.equal(result.data.total_requests, 0);
  assert.equal(result.data.today_cost_micros, '0');
  assert.deepEqual(result.data.model_distribution, []);
  await page.getByText(locale === 'en-US' ? 'No requests or costs in this period' : '所选时段暂无请求与费用', { exact: true }).waitFor({ state: 'visible' });
  const trends = section(page, labels.trend);
  await trends.getByRole('heading', { name: locale === 'en-US' ? 'No trend samples in this period' : '所选时段暂无趋势样本', exact: true }).waitFor({ state: 'visible' });
  assert.equal(await trends.locator('.recharts-wrapper').count(), 0, 'no charts may imply fabricated samples in an empty window');
  assert.equal(await page.locator('.recharts-area-curve').count(), 0);
  assert.equal(await page.locator('.ur-composition-ring').count(), 0);
  await page.getByText(locale === 'en-US' ? 'No samples' : '暂无样本', { exact: true }).first().waitFor({ state: 'visible' });
  const recent = section(page, labels.recent);
  await recent.getByText(locale === 'en-US' ? 'Latest requests shown separately; not a complete aggregate of the selected period.' : '独立显示最近请求，不代表所选窗口完整聚合。', { exact: true }).waitFor({ state: 'visible' });
  const recentIDs = result.data.recent_logs.map(row => row.request_id);
  assert.deepEqual((await recent.locator('.ur-request-link').allTextContents()).map(text => text.trim()), recentIDs);
  await inspectCurrentAvailability(page, result, locale);
  assert.deepEqual(item.traffic.mutations, []);
  return { trend_state: 'no_samples', trend_curve_count: 0, recent_request_ids: recentIDs, current_deployment_operations: result.data.channel_health.length };
}
async function inspectDOM(item, result, raw, role, locale, width) {
  const { page } = item, labels = locale === 'en-US' ? en : zh;
  const expected = validateAPI(result, raw, role);
  await ready(page);
  assert.equal((await section(page, labels.request).locator('.ur-composition-total').innerText()).trim(), expected.total.toLocaleString(locale));
  assert.equal((await section(page, labels.cost).locator('.ur-composition-total').innerText()).trim(), expectedMoney(expected.cost));
  for (const [title, metric] of [[labels.request, 'requests'], [labels.cost, 'cost']]) {
    let displayed = 0n;
    for (const text of await section(page, title).locator('.ur-model-amount').allTextContents()) {
      if (metric === 'requests') displayed += BigInt(text.trim().replaceAll(',', ''));
      else {
        const [whole, fraction = ''] = text.trim().replace('¥', '').split('.');
        displayed += BigInt(whole) * 1000000n + BigInt(fraction.padEnd(6, '0'));
      }
    }
    assert.equal(displayed, metric === 'requests' ? BigInt(expected.total) : expected.cost, 'visible legend including Other must preserve the full denominator');
  }
  assert.equal(await section(page, labels.quality).locator('.ur-model-select').count(), expected.groups.size);
  assert.equal(await section(page, labels.settlement).count(), ['platform_admin', 'billing_admin'].includes(role) ? 1 : 0);
  assert.equal(await page.locator('.ur-overview-notifications').count(), role === 'platform_admin' ? 1 : 0);
  if (['platform_admin', 'billing_admin'].includes(role)) {
    assert.equal((await section(page, labels.settlement).locator('.ur-settlement-value').first().innerText()).trim(), expectedMoney(BigInt(raw.wallet.balance || 0) + BigInt(raw.wallet.granted || 0)));
  }
  assert.equal(await page.locator('.ur-deployment-health-row').count(), role === 'platform_admin' ? result.data.channel_health.length : 0);
  if (role === 'platform_admin') await inspectCurrentAvailability(page, result, locale);
  if (role !== 'platform_admin') assert.ok((await page.locator('.ur-overview-scope').innerText()).includes(raw.org_name));
  assert.equal((await page.locator('.ur-overview').innerText()).includes('FOREIGN-OVERVIEW-DO-NOT-DISPLAY'), false);
  const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth, body: document.body.scrollWidth }));
  assert.equal(size.viewport, width); assert.ok(size.document <= width && size.body <= width, JSON.stringify(size));
  if (expected.total) {
    assert.equal(await page.getByRole('img', { name: labels.request, exact: true }).count(), 1);
    assert.equal(await page.getByRole('img', { name: labels.cost, exact: true }).count(), 1);
    assert.equal(await section(page, labels.trend).locator('.recharts-area-curve').count(), 2);
    assert.ok(await section(page, labels.recent).getByText('overview-future-recent', { exact: true }).count());
    // Both tooltips must show facts from the real response, not an SVG count alone.
    const panels = section(page, labels.trend).locator('.ur-overview-trend');
    for (let i = 0; i < 2; i++) {
      const chart = panels.nth(i).locator('.recharts-wrapper'); await chart.scrollIntoViewIfNeeded();
      const box = await chart.boundingBox(); assert.ok(box);
      await page.mouse.move(box.x + 64 + (box.width - 80) / 2, box.y + box.height / 2);
      const tooltip = panels.nth(i).locator('.recharts-tooltip-wrapper');
      await tooltip.waitFor({ state: 'visible' });
      const text = await tooltip.innerText();
      assert.ok(result.data.daily_trend.some(point => text.includes(i === 1 ? expectedMoney(point.cost_micros) : point.requests.toLocaleString(locale))), `trend tooltip ${i} did not match API facts`);
    }
    await page.mouse.move(0, 0);
  }
  assert.deepEqual(item.traffic.mutations, []);
  const evidence = { role, locale, width, days: daysFor(result.url), requests: expected.total, cost_micros: expected.cost.toString(), api: result };
  if (await page.locator('.mobile-menu-toggle').isVisible()) {
    const drawer = await page.locator('.ur-sider').boundingBox();
    const overlayVisible = await page.locator('.sidebar-overlay').isVisible();
    assert.ok(drawer && drawer.x + drawer.width <= 0.5);
    assert.equal(overlayVisible, false);
    evidence.mobile_navigation = { drawer_right: drawer.x + drawer.width, overlay_visible: overlayVisible };
  }
  // Four representative layouts only; later refreshes do not overwrite evidence.
  const screenshot = `${role}-${locale}-${width}.png`;
  if (role === 'platform_admin' && evidence.days === 1 && raw === fixture && !screenshots.has(screenshot)) {
    // The app scrolls its content internally; capture the top overview viewport.
    await page.locator('.ur-content').evaluate(element => element.scrollTo({ top: 0, behavior: 'instant' }));
    await paint(page);
    await page.screenshot({ path: path.join(artifactRoot, screenshot), fullPage: true });
    screenshots.add(screenshot); evidence.screenshot = screenshot;
  }
  cases.push(evidence);
}
async function chooseRange(item, days, labels) {
  const promise = waitStats(item.page, days);
  await item.page.getByRole('button', { name: labels.range[days], exact: true }).click();
  const result = await unpack(await promise); await ready(item.page); return result;
}
async function refresh(item, labels, status = 200) {
  const promise = waitStats(item.page, 1, status);
  await item.page.getByRole('button', { name: labels.refresh, exact: true }).click();
  return unpack(await promise);
}

try {
  browser = await chromium.launch({ headless: true });
  const guard = await contextAt(fixture.base_url, fixture.control_urls);
  const guardPage = await guard.newPage();
  for (const target of ['https://provider.pr4.invalid/v1/chat/completions', 'https://feed.pr4.invalid/v1/completed-executions', `${fixture.base_url}/api/v1/integrations/xinsight/completed-executions`, 'https://unclassified-upstream.pr4.invalid/probe']) assert.equal((await guardPage.goto(target)).status(), 502);
  await closeContext(guard);
  const anonymous = await contextAt(fixture.base_url);
  assert.equal((await anonymous.request.get('/api/v1/dashboard/stats')).status(), 401);
  assert.equal((await anonymous.request.get('/api/v1/internal-status')).status(), 401);
  await closeContext(anonymous);

  for (const role of allowed) {
    const item = await login(role); let result = item.initial;
    assert.equal(result.data.model_distribution.length, 62);
    assert.equal(result.data.model_distribution.find(row => row.model === 'overview-model-00').cost_micros, '9007199254740994');
    assert.equal(result.data.model_distribution.find(row => row.model === 'original-name').cost_micros, '7');
    const foreign = await apiGet(item, `/api/v1/dashboard/stats?org_id=${fixture.other_org_id}`); assert.equal(foreign.status(), 403);
    const attention = await apiGet(item, '/api/v1/internal-status');
    assert.equal(attention.status(), ['platform_admin', 'org_admin'].includes(role) ? 200 : 403);
    if (attention.ok()) { const data = await attention.json(); assert.equal(data.open_critical, 1); assert.equal(data.open_warning, 1); }
    const longModel = [...expectedWindow(fixture, result.url).groups.keys()].find(model => model.includes('long-model-name'));
    await ready(item.page);
    const reads = item.traffic.stats;
    await section(item.page, zh.request).getByRole('button', { name: /overview-model-01/ }).click();
    assert.equal(await item.page.locator('.ur-inspector-model').innerText(), 'overview-model-01');
    await section(item.page, zh.cost).getByRole('button', { name: /original-name/ }).click();
    assert.equal(await item.page.locator('.ur-inspector-model').innerText(), 'original-name');
    const fallbackDetails = section(item.page, zh.inspector).locator('.ur-fact-list > div').filter({ has: item.page.getByText('请求费用', { exact: true }) });
    assert.equal((await fallbackDetails.locator('dd').innerText()).trim(), '¥0.000007');
    await item.page.locator('.ur-model-picker select').selectOption(longModel);
    await paint(item.page); assert.equal(item.traffic.stats, reads);
    assert.equal(await item.page.locator('.ur-inspector-model').innerText(), longModel);
    await section(item.page, zh.inspector).getByRole('button', { name: '全部模型', exact: true }).click();
    assert.equal(await item.page.locator('.ur-model-picker select').inputValue(), '');
    await section(item.page, zh.quality).getByRole('button', { name: longModel, exact: true }).click();
    assert.equal(await item.page.locator('.ur-inspector-model').innerText(), longModel);
    assert.equal(item.traffic.stats, reads);
    for (const width of [1280, 390]) {
      await item.page.setViewportSize({ width, height: 900 });
      for (const locale of ['en-US', 'zh-CN']) {
        const before = item.traffic.stats; await language(item.page, locale);
        await inspectDOM(item, result, fixture, role, locale, width);
        assert.equal(item.traffic.stats, before);
        assert.equal(await item.page.locator('.ur-model-picker select').inputValue(), longModel);
        assert.equal(await item.page.getByRole('button', { name: (locale === 'en-US' ? en : zh).range[1], exact: true }).getAttribute('aria-pressed'), 'true');
      }
    }
    for (const days of [7, 30, 1]) {
      result = await chooseRange(item, days, zh);
      await inspectDOM(item, result, fixture, role, 'zh-CN', 390);
    }
    if (role === 'platform_admin') {
      await control(item, 'hold-next');
      const older = waitStats(item.page, 7);
      await item.page.getByRole('button', { name: zh.range[7], exact: true }).click();
      const deadline = Date.now() + 15000;
      let entered = false;
      while (Date.now() < deadline && !entered) { entered = (await (await apiGet(item, '/__overview_fixture/control')).json()).entered; if (!entered) await new Promise(resolve => setTimeout(resolve, 25)); }
      assert.ok(entered, 'older actual handler request was not held');
      result = await chooseRange(item, 30, zh);
      await control(item, 'release'); validateAPI(await unpack(await older), fixture, role);
      await inspectDOM(item, result, fixture, role, 'zh-CN', 390);
      assert.equal(await item.page.getByRole('button', { name: zh.range[30], exact: true }).getAttribute('aria-pressed'), 'true');
      result = await chooseRange(item, 1, zh);
      for (const locale of ['en-US', 'zh-CN']) {
        const labels = locale === 'en-US' ? en : zh; await language(item.page, locale);
        await control(item, 'stats-fail-next'); const failed = await refresh(item, labels, 500);
        assert.equal(failed.data.code, 'dashboard_query_failed'); assert.equal(failed.data.reason_code, 'context_canceled');
        await item.page.getByRole('heading', { name: labels.error, exact: true }).waitFor();
        assert.equal(await item.page.locator('.ur-composition').count(), 0);
        const retry = waitStats(item.page, 1); await item.page.getByRole('button', { name: labels.reload, exact: true }).click();
        result = await unpack(await retry); await inspectDOM(item, result, fixture, role, locale, 390);
        await control(item, 'attention-fail');
        const attentionFailed = item.page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/internal-status' && new URL(r.url()).searchParams.get('org_id') === String(fixture.org_id) && r.status() === 500);
        result = await refresh(item, labels); await attentionFailed;
        await item.page.waitForFunction(text => document.querySelector('.ur-overview-notifications strong')?.textContent.trim() === text, labels.unavailable);
        await inspectDOM(item, result, fixture, role, locale, 390);
        await control(item, 'attention-recover');
        const attentionReady = item.page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/internal-status' && new URL(r.url()).searchParams.get('org_id') === String(fixture.org_id) && r.status() === 200);
        result = await refresh(item, labels); const notification = await (await attentionReady).json();
        assert.equal(notification.open_critical + notification.open_warning, 2);
        await item.page.waitForFunction(() => document.querySelector('.ur-overview-notifications strong')?.textContent.trim() === '2');
      }
    }
    assert.deepEqual(item.traffic.mutations, []); await closeContext(item.context);
  }
  for (const role of ['member', 'foreign']) {
    const item = await login(role);
    if (role === 'member') await item.page.waitForFunction(() => location.pathname !== '/' && location.pathname !== '/login');
    else await item.page.getByRole('heading', { name: zh.error, exact: true }).waitFor();
    assert.equal((await apiGet(item, '/api/v1/dashboard/stats')).status(), 403);
    assert.equal((await apiGet(item, '/api/v1/internal-status')).status(), 403);
    assert.equal(await item.page.locator('.ur-composition').count(), 0);
    if (role === 'member') assert.equal(item.traffic.stats, 0);
    assert.deepEqual(item.traffic.mutations, []); await closeContext(item.context);
  }
  for (const [baseURL, state, copy] of [[fixture.unavailable_url, 'unavailable', '无法确认客户组织上下文'], [fixture.ambiguous_url, 'ambiguous', '客户组织上下文不唯一']]) {
    const item = await login('platform_admin', baseURL);
    const profile = await (await apiGet(item, '/api/v1/me')).json(); assert.equal(profile.customer_context.state, state);
    await item.page.getByRole('heading', { name: copy, exact: true }).waitFor();
    assert.equal(item.traffic.stats, 0); assert.equal(await item.page.locator('.ur-composition').count(), 0);
    assert.equal((await apiGet(item, '/api/v1/dashboard/stats')).status(), 403);
    await closeContext(item.context);
  }
  const empty = await login('platform_admin', fixture.empty_url, emptyFixture);
  validateAPI(empty.initial, emptyFixture, 'platform_admin');
  await ready(empty.page);
  for (const locale of ['zh-CN', 'en-US']) {
    await language(empty.page, locale); const labels = locale === 'en-US' ? en : zh;
    assert.equal((await section(empty.page, labels.request).locator('.ur-composition-total').innerText()).trim(), '0');
    assert.equal((await section(empty.page, labels.cost).locator('.ur-composition-total').innerText()).trim(), '¥0.00');
    await empty.page.getByText(locale === 'en-US' ? 'No requests in this period' : '暂无请求', { exact: true }).waitFor();
    const emptyState = await inspectEmptyWindow(empty, empty.initial, locale);
    cases.push({ role: 'platform_admin', locale, width: 1280, days: 1, requests: 0, cost_micros: '0', state: 'empty', ...emptyState, api: empty.initial });
  }
  await closeContext(empty.context);
  // Existing real fixture has 48h/240h history but no 24h samples. Its recent
  // request projection must survive a zero selected-window aggregate.
  const history = await login('platform_admin', fixture.selection_url);
  await ready(history.page);
  assert.deepEqual(history.initial.data.recent_logs.map(row => row.request_id), ['selection-seven', 'selection-thirty']);
  const start = Date.parse(new URL(history.initial.url).searchParams.get('start_time'));
  assert.ok(history.initial.data.recent_logs.every(row => Date.parse(row.created_at) < start));
  for (const locale of ['zh-CN', 'en-US']) {
    await language(history.page, locale);
    const emptyState = await inspectEmptyWindow(history, history.initial, locale);
    cases.push({ role: 'platform_admin', locale, width: 1280, days: 1, requests: 0, cost_micros: '0', state: 'empty_window_with_recent_history', ...emptyState, api: history.initial });
  }
  await closeContext(history.context);
  assert.deepEqual(pageErrors, []);
} finally {
  await closeOwnedBrowser();
  await writeFile(path.join(artifactRoot, 'sql-api-dom.json'), JSON.stringify({
    sql: { org_id: fixture.org_id, rows: fixture.rows, wallet: fixture.wallet, deployment_id: fixture.deployment_id, operation_id: fixture.operation_id },
    cases, screenshots: [...screenshots], browser_closed: browserClosed, owned_contexts_remaining: contexts.size,
  }, null, 2), { mode: 0o600 });
  console.log(`OVERVIEW_BROWSER_RECEIPT ${JSON.stringify({ cases: cases.map(({ api, ...summary }) => summary), screenshots: [...screenshots], browser_closed: browserClosed, owned_contexts_remaining: contexts.size })}`);
}
