import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';

const fixture = JSON.parse(process.env.OVERVIEW_BROWSER_FIXTURE);
const artifacts = path.join(process.env.OVERVIEW_BROWSER_ARTIFACTS, 'navigation');
await mkdir(artifacts, { recursive: true });
const cases = [], errors = [], external = [], api = [];
let browser, closed = false, closing, failure;
async function closeBrowser() {
  closing ||= (async () => { if (browser && !closed) { await browser.close(); closed = true; } })();
  return closing;
}
for (const signal of ['SIGINT','SIGTERM']) process.once(signal,async()=> {
  try { await closeBrowser(); } finally { console.log(`OVERVIEW_NAVIGATION_RECEIPT ${JSON.stringify({signal,browser_closed:closed,cases})}`);process.exit(130); }
});
function record(item) { cases.push(item); console.log(`UI_BROWSER_CASE ${JSON.stringify(item)}`); }
async function context(base) {
  const c = await browser.newContext({ baseURL: base, viewport: { width: 1440, height: 1000 } });
  await c.route('**/*', async route => {
    const u = new URL(route.request().url());
    if (u.origin === new URL(base).origin) return route.continue();
    external.push(u.origin + u.pathname); await route.abort('blockedbyclient');
  });
  return c;
}
async function login(role, base = fixture.base_url) {
  const c = await context(base), page = await c.newPage();
  page.on('pageerror', e => errors.push(String(e)));
  page.on('response', r => { const u = new URL(r.url()); if (u.pathname.startsWith('/api/')) api.push({ path: u.pathname + u.search, status: r.status(), method: r.request().method() }); });
  await page.goto('/login');
  await page.getByRole('button', { name: '中文', exact: true }).click();
  await page.getByLabel('用户名').fill(fixture.users[role]);
  await page.locator('input[type="password"]').fill(fixture.password);
  const signedIn = response(page, '/api/v1/auth/login', r => r.status() === 200);
  await page.getByRole('button', { name: '登录', exact: true }).click(); await signedIn;
  await ready(page); return { c, page };
}
async function ready(page) { await page.locator('.ur-overview[aria-busy="false"] .ur-composition').first().waitFor(); }
function response(page, pathname, condition = () => true) {
  const pending = page.waitForResponse(r => (pathname instanceof RegExp ? pathname.test(new URL(r.url()).pathname) : new URL(r.url()).pathname === pathname) && condition(r, new URL(r.url())));
  pending.catch(() => {}); // Awaiters still receive the original rejection; action failures can run finally.
  return pending;
}
async function bodyReady(page, heading) { await page.getByRole('heading', { name: heading, exact: true }).first().waitFor(); }
async function fillField(root, label, value) { const field = root.locator('.semi-form-field').filter({ hasText: label }); assert.equal(await field.count(), 1); await field.locator('input').first().fill(value); }
async function home(page) { await page.goto('/'); await ready(page); }
async function follow(page, name, expectedPath, heading, endpoint, check = () => {}) {
  const result = response(page, endpoint, r => r.status() === 200);
  await page.getByRole('button', { name, exact: true }).click();
  await page.waitForURL(u => u.pathname === expectedPath); await bodyReady(page, heading);
  const r = await result; const json = await r.json(); await check(new URL(page.url()), new URL(r.url()), json, page);
  record({ kind: 'overview-link', action: name, route: new URL(page.url()).pathname + new URL(page.url()).search, handler: endpoint, status: r.status() });
  await page.goBack(); await ready(page);
  assert.equal(await page.locator('html').getAttribute('lang'), 'zh-CN');
  assert.equal(new URL(page.url()).pathname, '/');
  assert.equal(await page.getByRole('button', { name: '最近 24 小时', exact: true }).getAttribute('aria-pressed'), 'true');
}
try {
  browser = await chromium.launch({ headless: true });
  if (process.env.OVERVIEW_NAVIGATION_ONLY === '1') {
    const control = await browser.newContext();
    await control.route('**/*', async route => {
      const u = new URL(route.request().url());
      const kind = u.hostname === 'provider.pr4.invalid' ? 'provider' : u.hostname === 'feed.pr4.invalid' || u.pathname === '/api/v1/integrations/xinsight/completed-executions' ? 'feed' : 'upstream';
      const observed = await control.request.get(`${fixture.control_urls[kind]}/observed`); assert.equal(observed.status(),418);
      await route.fulfill({status:502,body:'fixture causal outbound guard'});
    });
    const probe = await control.newPage();
    for (const target of ['https://provider.pr4.invalid/v1/chat/completions','https://feed.pr4.invalid/v1/completed-executions',`${fixture.base_url}/api/v1/integrations/xinsight/completed-executions`,'https://unclassified-upstream.pr4.invalid/probe']) assert.equal((await probe.goto(target)).status(),502);
    await control.close();
  }

  const { page, c } = await login('platform_admin');
  await page.getByText('构建环境：生产环境', { exact: true }).waitFor();
  assert.equal(await page.getByText('构建环境：生产环境', { exact: true }).getAttribute('title'), '构建时设置的环境标签，不表示当前服务的运行环境。');
  // The link is a real request ID in the independently seeded database.
  const requestID = 'overview-future-recent';
  await page.getByRole('button', { name: requestID, exact: true }).waitFor();
  await follow(page, requestID, '/logs', '执行请求', '/api/v1/logs', (url, query, data) => {
    assert.equal(url.searchParams.get('request_id'), requestID); assert.equal(query.searchParams.get('request_id'), requestID);
    assert.ok(data.data.length > 0); assert.ok(data.data.every(row => row.request_id === requestID));
  });
  const model = 'overview-model-01';
  await page.locator('.ur-model-picker select').selectOption(model);
  await follow(page, '查看执行请求', '/logs', '执行请求', '/api/v1/logs', (url, query, data) => {
    assert.equal(url.searchParams.get('model'), model); assert.equal(query.searchParams.get('model'), model);
    assert.ok(data.data.length > 0); assert.ok(data.data.every(row => row.model === model));
  });
  await follow(page, '查看用量分析', '/usage-analytics', '用量分析', '/api/v1/usage/analytics', (url, query, data) => {
    assert.equal(query.searchParams.get('org_id'), String(fixture.org_id)); assert.ok(data.summary);
  });
  await follow(page, '查看结算账户', '/wallet', '预算与成本中心', '/api/v1/wallet', (url, query, data) => {
    assert.equal(query.searchParams.get('org_id'), String(fixture.org_id)); assert.equal(data.org_id, fixture.org_id);
  });
  await follow(page, '处理通知', '/notifications', '通知中心', '/api/v1/notification-events', (url, query, data) => {
    assert.equal(query.searchParams.get('org_id'), String(fixture.org_id)); assert.ok(data.data.length >= 2);
  });
  await follow(page, '查看运行状态', '/status', '运行状态', '/api/v1/runtime/status', (url, query, data) => {
    assert.equal(data.runtime_environment.environment, 'development');
  });
  const deployment = page.locator('.ur-deployment-health-row').first();
  const deployName = await deployment.getAttribute('aria-label');
  await follow(page, deployName, '/channels', '模型接入', `/api/v1/model-deployments/${fixture.deployment_id}`, async (url, query, data, current) => {
    assert.equal(url.searchParams.get('deployment_id'), String(fixture.deployment_id)); assert.equal(url.searchParams.get('operation_id'), fixture.operation_id);
    await current.locator('.semi-sidesheet:visible').filter({ hasText: `Deployment #${fixture.deployment_id}` }).waitFor();
  });
  let stateRead = response(page, '/api/v1/dashboard/stats', (r,u) => r.status() === 200 && (Date.parse(u.searchParams.get('end_time'))-Date.parse(u.searchParams.get('start_time'))) / 86400000 === 7);
  await page.getByRole('button', { name: '最近 7 天', exact: true }).click(); await stateRead; await ready(page);
  await page.locator('.ur-model-picker select').selectOption('overview-model-01');
  let stateTarget = response(page, '/api/v1/usage/analytics', r => r.status() === 200);
  await page.getByRole('button', { name: '查看用量分析', exact: true }).click(); await stateTarget;
  await page.goBack(); await ready(page);
  assert.equal(await page.getByRole('button', { name: '最近 7 天', exact: true }).getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('.ur-model-picker select').inputValue(), 'overview-model-01');
  await page.locator('.ur-model-picker select').selectOption('');
  stateTarget = response(page, '/api/v1/logs', r => r.status() === 200);
  await page.getByRole('button', { name: '查看执行请求', exact: true }).click(); await stateTarget;
  await page.goBack(); await ready(page);
  assert.equal(await page.locator('.ur-model-picker select').inputValue(), '');
  assert.equal(await page.getByRole('button', { name: '最近 7 天', exact: true }).getAttribute('aria-pressed'), 'true');
  record({ kind: 'history-return', nondefault_range: '7d', selected_model_retained: 'overview-model-01', explicit_All_retained: true });
  // Data transitions come from different real time windows in an isolated PG DB.
  const sel = await login('platform_admin', fixture.selection_url);
  const picker = sel.page.locator('.ur-model-picker select');
  assert.equal(await picker.inputValue(), '');
  async function range(days) {
    const r = response(sel.page, '/api/v1/dashboard/stats', (r,u) => r.status() === 200 && (Date.parse(u.searchParams.get('end_time')) - Date.parse(u.searchParams.get('start_time'))) / 86400000 === days);
    await sel.page.getByRole('button', { name: days === 1 ? '最近 24 小时' : `最近 ${days} 天`, exact: true }).click(); await r; await ready(sel.page);
  }
  await range(7); assert.equal(await picker.inputValue(), 'selection-older-model');
  await picker.selectOption('selection-older-model');
  await range(30); assert.equal(await picker.inputValue(), 'selection-older-model');
  await picker.selectOption('selection-oldest-model');
  await range(7); assert.equal(await picker.inputValue(), 'selection-older-model');
  await sel.page.getByRole('status').filter({ hasText: 'selection-oldest-model' }).waitFor();
  await sel.page.setViewportSize({width:390,height:900});
  const localeReads = api.filter(x=>x.path.startsWith('/api/v1/dashboard/stats')).length;
  for (const [name, phrase] of [['English','Selected model selection-oldest-model'],['中文','所选模型 selection-oldest-model']]) {
    await sel.page.locator('.mobile-menu-toggle').click();
    await sel.page.locator('.ur-sider.open .ur-mobile-language').getByRole('button',{name,exact:true}).click();
    await sel.page.keyboard.press('Escape');
    await sel.page.waitForFunction(()=>document.querySelector('.mobile-menu-toggle')?.getAttribute('aria-expanded')==='false');
    await sel.page.locator('.ur-sider').evaluate(element=>Promise.all(element.getAnimations().map(animation=>animation.finished)));
    const drawer = await sel.page.locator('.ur-sider').boundingBox();
    assert.ok(drawer && drawer.x+drawer.width<=0.5, 'mobile drawer must finish closing');
    assert.equal(await sel.page.locator('.sidebar-overlay').isVisible(),false);
    const fallbackNotice = sel.page.getByRole('status').filter({hasText:phrase});
    await fallbackNotice.scrollIntoViewIfNeeded();
    const width = await sel.page.evaluate(()=>({document:document.documentElement.scrollWidth,body:document.body.scrollWidth}));
    assert.ok(width.document<=390 && width.body<=390,JSON.stringify(width));
    assert.equal(await picker.inputValue(),'selection-older-model');
    await sel.page.screenshot({path:path.join(artifacts,`model-selection-fallback-${name==='English'?'en':'zh'}-390.png`),fullPage:true});
  }
  assert.equal(api.filter(x=>x.path.startsWith('/api/v1/dashboard/stats')).length,localeReads);
  await sel.page.setViewportSize({width:1440,height:1000});
  record({kind:'bilingual-fallback-layout',width:390,model_intent_retained:true,extra_stats_reads:0,outer_overflow:false,drawer_fully_closed:true,screenshots:['model-selection-fallback-en-390.png','model-selection-fallback-zh-390.png']});

  await range(30); assert.equal(await picker.inputValue(), 'selection-oldest-model');
  await picker.selectOption(''); await range(1); await range(7); assert.equal(await picker.inputValue(), '');
  record({ kind: 'selection', actual_PG_windows: [1,7,30], assertions: ['first-empty-to-populated','present-user-selection-retained','absent-selection-explained','remembered-selection-restored','explicit-All-retained'] });
  await sel.c.close();
  // Real sidebar navigation. Main read + rendered page, no placeholder outlets.
  const nav = [
    ['模型与端点','模型接入','/channels','模型接入','/api/v1/model-deployments'],
    ['模型治理','模型接入','/channels?view=catalog','公共模型目录','/api/v1/model-registry'],
    ['调用条件诊断','模型接入','/channels?view=diagnostics','执行诊断','/api/v1/model-deployments'],
    ['访问审核','访问与安全','/access-review','访问审核','/api/v1/projects'],
    ['应用与 Agent','访问与安全','/applications','应用与 Agent','/api/v1/projects'],
    ['人员与权限','访问与安全','/organization-members','人员与权限','/api/v1/users'],
    ['高级模型限制','访问与安全','/agent-trust','高级模型限制','/api/v1/model-call-authorizations'],
    ['共享限额','访问与安全','/limit-policies','共享限额','/api/v1/limit-policies'],
    ['内容安全','访问与安全','/guardrail-policies','内容安全','/api/v1/guardrail-policies'],
    ['审计日志','访问与安全','/audit-logs','审计日志','/api/v2/audit-logs'],
    ['执行请求','监控与用量','/logs','执行请求','/api/v1/logs'],
    ['异步模型任务','监控与用量','/media-jobs','异步模型任务','/api/v1/projects'],
    ['用量分析','监控与用量','/usage-analytics','用量分析','/api/v1/usage/analytics'],
    ['通知中心','监控与用量','/notifications','通知中心','/api/v1/notification-events'],
    ['运行状态','监控与用量','/status','运行状态','/api/v1/runtime/status'],
    ['预算与成本中心','成本与财务','/wallet','预算与成本中心','/api/v1/wallet'],
    ['费率与计价','成本与财务','/model-prices','费率与计价','/api/v1/enterprise-finance/rate-cards'],
    ['成本优化','成本与财务','/cost-optimization','成本优化','/api/v1/cost-optimization/summary'],
    ['分摊与对账','成本与财务','/reconciliation','分摊与对账','/api/v1/reconciliation'],
    ['部署与网络','系统与集成','/deployment','部署与网络','/api/v1/runtime/status'],
    ['集成管理','系统与集成','/integrations','集成管理',null],
    ['API 与兼容性','系统与集成','/docs','API 与兼容性',null],
  ];
  for (const [name, group, expected, heading, endpoint] of nav) {
    await home(page); const sidebar = page.locator('.ur-sider');
    const modelSubview = name === '模型治理' || name === '调用条件诊断';
    const link = sidebar.getByText(modelSubview ? '模型与端点' : name, { exact: true });
    if (!await link.isVisible()) await sidebar.getByText(group, { exact: true }).click();
    if (modelSubview) { await link.click(); await bodyReady(page, '模型接入'); }
    const read = endpoint ? response(page, endpoint, r => r.status() === 200) : null;
    if (modelSubview) await page.getByRole('button', { name: name === '模型治理' ? '公共模型目录' : '执行诊断', exact: true }).click();
    else await link.click();
    await page.waitForURL(u => u.pathname + u.search === expected); await bodyReady(page, heading);
    if (read) { const observed = await read; if (['访问审核','应用与 Agent','异步模型任务'].includes(name)) { const body = await observed.json(); assert.equal(body.data.length,0); } }
    if (name === '应用与 Agent') await page.getByText('没有可读取的项目应用', { exact: true }).waitFor();
    if (name === '访问审核') await page.getByText('暂无可审核项目', { exact: true }).waitFor();
    if (name === '异步模型任务') await page.getByText('没有可见项目', { exact: true }).waitFor();
    record({ kind: modelSubview ? 'model-subview' : 'sidebar', action: name, route: expected, handler: endpoint, state: ['访问审核','应用与 Agent','异步模型任务'].includes(name) ? 'real-empty-project-list; project-business-actions-not-executed' : endpoint ? 'real_handler_200_and_component' : 'actual_component_initial_or_redirect_state' });
  }
  assert.deepEqual(api.filter(x => x.status >= 400), [], 'positive navigation must not hide failing secondary API reads');
  const directory = page.getByRole('textbox', { name: '搜索 API 接口', exact: true });
  await directory.fill('no-such-local-ui-endpoint'); await page.getByText('没有匹配的接口', { exact: true }).waitFor();
  await directory.fill('/api/v1/logs'); await page.getByText('/api/v1/logs', { exact: true }).first().waitFor();
  record({ kind: 'API-directory', assertions: ['actual-component-search','empty','matching-route-recovery'] });
  // Request list: real filter, pagination, empty, canceled-query error, recovery, detail.
  await page.goto('/logs'); await bodyReady(page, '执行请求');
  await page.locator('.ur-advanced-filters > summary').click();
  const modelFilter = page.locator('.ur-filter-group').filter({ has: page.locator('#logs-model-filter-label') }).locator('.semi-select');
  await modelFilter.click();
  let r = response(page, '/api/v1/logs', (r,u) => r.status() === 200 && u.searchParams.get('model') === 'overview-model-02');
  await page.locator('.semi-select-option:visible').getByText('overview-model-02', { exact: true }).click();
  let data = await (await r).json(); assert.equal(data.data.length, 1); assert.equal(data.data[0].model, 'overview-model-02');
  r = response(page, '/api/v1/logs', (r,u) => r.status() === 200 && u.searchParams.get('request_id') === 'no-such-ui-request');
  await page.getByRole('textbox', { name: '操作 ID 精确匹配', exact: true }).fill('no-such-ui-request');
  data = await (await r).json(); assert.equal(data.data.length, 0);
  await page.getByText('暂无日志记录', { exact: true }).waitFor();
  r = response(page, '/api/v1/logs', (r,u) => r.status() === 200 && !u.searchParams.get('model') && !u.searchParams.get('request_id'));
  await page.getByRole('button', { name: '清除', exact: true }).click(); await r;
  const next = page.locator('.semi-page-next');
  r = response(page, '/api/v1/logs', (r,u) => r.status() === 200 && u.searchParams.get('page') === '2'); await next.click();
  const secondPage = await (await r).json(); assert.ok(secondPage.data.length >= 2, 'fixture must have a second row on page2');
  const expectedDetail = secondPage.data[1]; const detailRow = page.locator(`tr[data-row-key="${expectedDetail.id}"]`);
  await detailRow.waitFor();
  assert.equal(await detailRow.count(), 1, 'detail row must exist');
  const detailResponse = response(page, `/api/v1/logs/${expectedDetail.id}/trace`, r => r.status() === 200);
  await detailRow.click(); const detail = await (await detailResponse).json();
  assert.equal(detail.request_id, expectedDetail.request_id); assert.ok(Array.isArray(detail.events));
  const receiptInspector = page.getByRole('complementary', { name: '执行决策凭证', exact: true });
  await receiptInspector.locator('.ur-request-id-full').getByText(expectedDetail.request_id, { exact: true }).waitFor();
  record({ kind: 'request-explorer', assertions: ['model-option-submitted','exact-request-ID-empty-filter','clear-restores-query','page2-real-read','matching-ID-real-trace-and-inspector'] });
  const control = await page.request.post('/__overview_fixture/control', { headers: { Authorization: `Bearer ${await page.evaluate(() => localStorage.getItem('token'))}` }, data: { action: 'logs-fail-next' } });
  assert.equal(control.status(), 204);
  r = response(page, '/api/v1/logs', r => r.status() === 500);
  await page.getByRole('button', { name: '刷新执行请求', exact: true }).click(); await r;
  r = response(page, '/api/v1/logs', r => r.status() === 200);
  await page.getByRole('button', { name: '重试列表', exact: true }).click(); await r;
  record({ kind: 'request-query-error', assertions: ['real-canceled-SQL-500','visible-retry','real-handler200-recovery'] });
  await page.goto('/usage-analytics'); await bodyReady(page, '用量分析');
  r = response(page, '/api/v1/usage/analytics', (r,u) => r.status() === 200 && u.searchParams.get('model') === 'overview-model-01');
  await page.getByRole('textbox', { name: '模型筛选', exact: true }).fill('overview-model-01');
  data = await (await r).json(); assert.ok(data.summary.total_requests > 0);
  const filteredSummary = data.summary;
  assert.equal(data.group_by.key, 'overall');
  const csvResponse = response(page, '/api/v1/usage/analytics', (r,u) => r.status() === 200 && u.searchParams.get('format') === 'csv' && u.searchParams.get('model') === 'overview-model-01' && u.searchParams.get('group_by') === 'overall');
  const exportButton = page.locator('.ur-page-header').getByRole('button').filter({ hasText: '导出 CSV' });
  assert.equal(await exportButton.count(), 1); await exportButton.click();
  const csv = (await (await csvResponse).text()).trim().split(/\r?\n/).map(line => line.split(','));
  assert.equal(csv.length, 2); assert.equal(csv[0][1], 'total_requests');
  assert.equal(Number(csv[1][1]), filteredSummary.total_requests);
  assert.equal(csv[1][5], filteredSummary.cost_micros);
  record({ kind: 'usage', assertions: ['model-filter-real-data','CSV-same-model-and-group-filter','CSV-count-and-cost-match-filtered-summary'] });

  // People: a genuine second page plus canceled editing (no permission write).
  let peopleRead = response(page, '/api/v1/users', r => r.status() === 200);
  await page.goto('/organization-members'); await bodyReady(page, '人员与权限'); await peopleRead;
  peopleRead = response(page, '/api/v1/users', (r,u) => r.status() === 200 && u.searchParams.get('page') === '2');
  await page.locator('.semi-page-next').first().click();
  const people = await (await peopleRead).json(); assert.ok(people.data.length > 0);
  const editPerson = people.data.find(person => person.username.startsWith('ui-nav-person-'));
  assert.ok(editPerson, 'page2 must contain a synthetic editable person');
  const peopleWrites = api.filter(x => x.path.startsWith('/api/v1/users') && x.method !== 'GET').length;
  await page.locator(`tr[data-row-key="${editPerson.id}"]`).getByRole('button', { name: `编辑用户 ${editPerson.username}`, exact: true }).click();
  const peopleSheet = page.locator('.semi-sidesheet:visible');
  await peopleSheet.locator(`input[value="${editPerson.username}"]`).waitFor();
  assert.equal(await peopleSheet.locator('input').first().inputValue(), editPerson.username);
  await peopleSheet.getByRole('button', { name: '取消', exact: true }).click();
  await peopleSheet.waitFor({state:'hidden'});
  assert.equal(api.filter(x => x.path.startsWith('/api/v1/users') && x.method !== 'GET').length, peopleWrites);
  record({ kind: 'people', person_id: editPerson.id, assertions: ['real-page2-and-rows','matching-record-edit-and-draft','cancel-no-permission-write'] });
  // Audit uses committed filters, followed by the actual event inspector.
  let auditRead = response(page, '/api/v2/audit-logs', r => r.status() === 200);
  await page.goto('/audit-logs'); await bodyReady(page, '审计日志'); let auditData = await (await auditRead).json();
  assert.ok(auditData.data.length > 0);
  const auditRow = page.locator(`tr[data-row-key="${auditData.data[0].id}"]`); await auditRow.waitFor(); assert.equal(await auditRow.count(), 1); await auditRow.click();
  await page.getByRole('complementary', { name: '审计事件检查器' }).getByText(`AUD-${auditData.data[0].id}`, { exact: true }).waitFor();
  await page.getByRole('textbox', { name: '审计动作筛选', exact: true }).fill('ui.no-such-audit-action');
  auditRead = response(page, '/api/v2/audit-logs', (r,u) => r.status() === 200 && u.searchParams.get('action') === 'ui.no-such-audit-action');
  await page.getByRole('button', { name: '应用筛选', exact: true }).click(); auditData = await (await auditRead).json(); assert.equal(auditData.data.length, 0);
  auditRead = response(page, '/api/v2/audit-logs', (r,u) => r.status() === 200 && !u.searchParams.get('action'));
  await page.getByRole('button', { name: '重置筛选', exact: true }).click(); auditData = await (await auditRead).json(); assert.ok(auditData.data.length > 0);
  record({ kind: 'audit', assertions: ['actual-row-inspector-ID','explicit-apply-filter','real-empty-result','reset-restores-data'] });
  // Public model registry search uses the real backend query and restores it.
  await page.goto('/channels?view=catalog'); await bodyReady(page, '公共模型目录');
  let registryRead = response(page, '/api/v1/model-registry', (r,u) => r.status() === 200 && u.searchParams.get('q') === 'ui-no-such-model');
  await page.getByRole('textbox', { name: '搜索模型目录', exact: true }).fill('ui-no-such-model');
  assert.equal((await (await registryRead).json()).data.length, 0);
  registryRead = response(page, '/api/v1/model-registry', (r,u) => r.status() === 200 && !u.searchParams.get('q'));
  await page.getByRole('textbox', { name: '搜索模型目录', exact: true }).fill(''); await registryRead;
  record({ kind: 'model-registry', assertions: ['real-query-search-empty','clear-restores-scope'] });
  // Financial read tabs and server-filtered reconciliation, no economic writes.
  await page.goto('/wallet'); await bodyReady(page, '预算与成本中心');
  let walletRead = response(page, '/api/v1/wallet/transactions', r => r.status() === 200);
  await page.getByRole('tab', { name: '结算流水', exact: true }).click(); assert.ok(Array.isArray((await (await walletRead).json()).data));
  walletRead = response(page, '/api/v1/grants', r => r.status() === 200);
  await page.getByRole('tab', { name: '兼容额度', exact: true }).click(); assert.ok(Array.isArray((await (await walletRead).json()).data));
  record({ kind: 'wallet', assertions: ['real-transactions-read','real-grants-read','customer-scope-retained'], economic_write: false });
  let recRead = response(page, '/api/v1/reconciliation', r => r.status() === 200);
  await page.goto('/reconciliation'); await bodyReady(page, '分摊与对账'); await recRead;
  const modelSelect = page.locator('.semi-select').filter({ has: page.locator('#reconciliation-model-label') });
  await modelSelect.click(); const modelOption = page.locator('.semi-select-option:visible').nth(1); assert.equal(await modelOption.count(), 1);
  const recModel = (await modelOption.innerText()).trim();
  recRead = response(page, '/api/v1/reconciliation', (r,u) => r.status() === 200 && u.searchParams.get('model') === recModel);
  await modelOption.click(); await recRead;
  record({ kind: 'reconciliation', assertions: ['actual-model-option','real-server-filter'], model: recModel, economic_write: false });
  // Resolve confirmation cancel and success target a seeded event, never a peer.
  let notificationsRead = response(page, '/api/v1/notification-events', r => r.status() === 200);
  await page.goto('/notifications'); await bodyReady(page, '通知中心');
  const targetEvent = (await (await notificationsRead).json()).data.find(event => event.status !== 'resolved');
  assert.ok(targetEvent, 'fixture must contain an unresolved event');
  const eventRow = page.locator(`tr[data-row-key="${targetEvent.id}"]`);
  await eventRow.getByRole('button', { name: '解决', exact: true }).waitFor();
  assert.equal(await eventRow.count(), 1);
  const resolutions = api.filter(x => x.path.includes('/resolve') && x.method === 'POST').length;
  await eventRow.getByRole('button', { name: '解决', exact: true }).click();
  let confirmation = page.locator('.semi-modal:visible').filter({ hasText: '确认解决事件' });
  await confirmation.getByRole('button', { name: '取消', exact: true }).click();
  await confirmation.waitFor({state:'hidden'});
  assert.equal(api.filter(x => x.path.includes('/resolve') && x.method === 'POST').length, resolutions);
  await eventRow.getByRole('button', { name: '解决', exact: true }).click();
  confirmation = page.locator('.semi-modal:visible').filter({ hasText: '确认解决事件' });
  const resolved = response(page, `/api/v1/notification-events/${targetEvent.id}/resolve`, r => r.request().method() === 'POST');
  notificationsRead = response(page, '/api/v1/notification-events', r => r.status() === 200);
  await confirmation.getByRole('button', { name: '确认解决', exact: true }).click();
  const resolution = await resolved; assert.equal(resolution.status(), 200);
  assert.deepEqual(await resolution.json(), { id: targetEvent.id, status: 'resolved' });
  const refreshedEvent = (await (await notificationsRead).json()).data.find(event => event.id === targetEvent.id);
  assert.equal(refreshedEvent?.status, 'resolved');
  record({ kind: 'notifications', event_id: targetEvent.id, assertions: ['cancel-confirmation-no-POST','matching-ID-confirmed-resolve-real-handler200','refreshed-real-event-resolved'] });
  // Confirm/cancel + backend validation and successful non-secret registration.
  await page.goto('/deployment'); await bodyReady(page, '部署与网络');
  const postsBefore = api.filter(x => x.path === '/api/v1/deployment/profiles' && x.method === 'POST').length;
  const registerDeployment = page.locator('.ur-page-header').getByRole('button').filter({ hasText: '登记附加部署' });
  assert.equal(await registerDeployment.count(), 1); await registerDeployment.click();
  let sheet = page.locator('.semi-sidesheet:visible').filter({ hasText: '内部登记标识' });
  await sheet.getByRole('button', { name: '取消', exact: true }).click();
  await sheet.waitFor({state:'hidden'});
  assert.equal(api.filter(x => x.path === '/api/v1/deployment/profiles' && x.method === 'POST').length, postsBefore);
  await registerDeployment.click();
  sheet = page.locator('.semi-sidesheet:visible').filter({ hasText: '内部登记标识' });
  await sheet.locator('input[value="poc"]').waitFor();
  await fillField(sheet, '内部登记标识', 'bad profile space');
  await fillField(sheet, '显示名称', 'Local UI registration');
  r = response(page, '/api/v1/deployment/profiles', r => r.request().method() === 'POST');
  await sheet.getByRole('button', { name: '保存登记', exact: true }).click(); assert.equal((await r).status(), 400); await sheet.waitFor();
  assert.equal(await sheet.locator('.semi-form-field').filter({ hasText: '内部登记标识' }).locator('input').inputValue(), 'bad profile space');
  assert.equal(await sheet.locator('.semi-form-field').filter({ hasText: '显示名称' }).locator('input').inputValue(), 'Local UI registration');
  await fillField(sheet, '内部登记标识', 'dep_ui_nav_profile');
  r = response(page, '/api/v1/deployment/profiles', r => r.request().method() === 'POST');
  await sheet.getByRole('button', { name: '保存登记', exact: true }).click();
  const registrationResponse = await r; assert.equal(registrationResponse.status(), 201);
  const registration = await registrationResponse.json();
  assert.equal(registration.profile_ref, 'dep_ui_nav_profile'); assert.equal(registration.runtime_mutation, 'none');
  await page.getByText('Local UI registration', { exact: true }).first().waitFor();
  record({ kind: 'deployment-registration', assertions: ['cancel-no-POST','server400-keeps-draft','create201-shows-real-record'] });
  const accountRead = response(page, '/api/v1/me', r => r.status() === 200);
  await page.getByRole('button', { name: '打开我的账号', exact: true }).click();
  await page.waitForURL(u => u.pathname === '/settings'); await bodyReady(page, '我的账号');
  const account = await (await accountRead).json(); assert.equal(account.username, fixture.users.platform_admin);
  record({ kind: 'account', assertions: ['real-user-chip-navigation','actual-account-component','real-me-identity'], credentials_changed: false });
  // The viewer is rejected by the actual role middleware and real route guard.
  const viewer = await login('org_viewer');
  assert.equal(await viewer.page.getByRole('button', { name: '查看结算账户', exact: true }).count(), 0);
  const denied = await viewer.page.request.get('/api/v1/wallet', { headers: { Authorization: `Bearer ${await viewer.page.evaluate(() => localStorage.getItem('token'))}` } });
  assert.equal(denied.status(), 403);
  await viewer.page.goto('/wallet'); await viewer.page.waitForURL(u => u.pathname !== '/wallet'); await ready(viewer.page);
  record({ kind: 'authority', role: 'org_viewer', hidden_wallet_action: true, backend_status: 403, guarded_route: new URL(viewer.page.url()).pathname });
  await viewer.c.close();
  const unexpected = api.filter(x => x.status >= 400 && !(x.method === 'GET' && x.path.startsWith('/api/v1/logs?') && x.status === 500) && !(x.method === 'POST' && x.path === '/api/v1/deployment/profiles' && x.status === 400));
  assert.deepEqual(unexpected, [], 'unexpected API errors outside the two asserted negative scenarios');
  assert.deepEqual(external, []); assert.deepEqual(errors, []);
  await page.screenshot({ path: path.join(artifacts, 'navigation-final.png'), fullPage: true });
  await c.close();
} catch (error) {
  failure = { message: String(error), pages: [] };
  if (browser) for (const c of browser.contexts()) for (const p of c.pages()) {
    const screenshot = `failure-${failure.pages.length}.png`;
    await p.screenshot({path:path.join(artifacts,screenshot),fullPage:true}).catch(()=>{});
    failure.pages.push({url:p.url(),headings:await p.locator('h1,h2,h3').allTextContents().catch(()=>[]),buttons:await p.getByRole('button').allTextContents().catch(()=>[]),screenshot});
  }
  throw error;
} finally {
  await closeBrowser();
  const record = { cases, api, errors, external, failure, browser_closed: closed };
  await writeFile(path.join(artifacts, 'result.json'), JSON.stringify(record, null, 2));
  console.log(`OVERVIEW_NAVIGATION_RECEIPT ${JSON.stringify({ cases, errors, external, failure, browser_closed: closed })}`);
}
