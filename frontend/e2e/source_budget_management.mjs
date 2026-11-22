import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { chromium } from 'playwright';

const fixture = JSON.parse(process.env.SOURCE_BUDGET_BROWSER_FIXTURE || '{}');
assert.ok(fixture.base_url);
const output = process.env.SOURCE_BUDGET_BROWSER_OUTPUT || 'output/playwright/source-budget';
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const errors = [];
const journeys = [];
const apiPath = '/api/v1/enterprise-finance/source-budget-bindings';
const sourcePath = `/api/v1/data-exchange/xinsight/source-connections/${fixture.project_id}`;
const sourceRead = '/api/v1/integrations/xinsight/source/v2/budgets';
const context = await browser.newContext({ baseURL: fixture.base_url, serviceWorkers: 'block', viewport: { width: 1440, height: 1100 } });
await context.route('**/*', async route => {
  if (new URL(route.request().url()).origin !== new URL(fixture.base_url).origin) {
    errors.push('unexpected external browser request');
    await route.abort();
  } else await route.continue();
});
const page = await context.newPage();
page.on('pageerror', error => errors.push(error.message));
const waitVisible = locator => locator.waitFor({ state: 'visible', timeout: 15000 });
const clickAndResponse = async (locator, endpoint) => {
  const response = page.waitForResponse(r => new URL(r.url()).pathname === endpoint && r.request().method() === 'POST');
  await locator.click();
  const result = await response;
  assert.equal(result.status(), 200, `POST ${endpoint}`);
  return result.json();
};

async function verifyStandardPricing() {
  await page.goto('/model-prices?tab=enterprise');
  const [settled, overrun] = fixture.standard_receipts;
  const quotePath = '/api/v1/enterprise-finance/customer-charge-quotes/';
  const getResponse = endpoint => page.waitForResponse(response =>
    response.request().method() === 'GET' && new URL(response.url()).pathname === endpoint);
  for (const labels of [
    ['Standard 客户报价回执', '客户报价 Request ID', '查询客户报价', '请求报价', '已扣客户金额', '已结算', '加载超额待核对', '待核对'],
    ['Standard quote receipt', 'Customer quote Request ID', 'Look up customer quote', 'Quoted amount', 'Charged amount', 'Settled', 'Load pending quote overruns', 'Awaiting review'],
  ]) {
    const [heading, input, lookup, quoted, charged, settledLabel, list, pendingLabel] = labels;
    if (heading === 'Standard quote receipt') {
      await page.getByRole('button', { name: 'English', exact: true }).click();
      await page.reload();
    }
    const scope = page.locator('.ur-surface').filter({ has: page.getByRole('heading', { name: heading, exact: true }) });
    const amount = (label, money) => scope.locator('.ur-metric')
      .filter({ has: page.getByText(label, { exact: true }) }).getByText(money, { exact: true });
    await scope.getByRole('textbox', { name: input }).fill(settled.quote.request_id);
    const settledResponse = getResponse(quotePath + settled.quote.request_id);
    await scope.getByRole('button', { name: lookup }).click();
    const settledRead = await settledResponse;
    assert.equal(settledRead.status(), 200);
    assert.deepEqual(await settledRead.json(), settled);
    assert.equal(settled.settlement_status, 'settled');
    assert.equal(settled.charged_customer_cost_micros, '7');
    await waitVisible(amount(quoted, '¥0.000010'));
    await waitVisible(amount(charged, '¥0.000007'));
    await waitVisible(scope.getByText(settledLabel, { exact: true }));
    await waitVisible(scope.getByText(settled.quote.pricing_version, { exact: true }));

    const listResponse = getResponse('/api/v1/enterprise-finance/customer-charge-quote-overruns');
    await scope.getByRole('button', { name: list }).click();
    const listRead = await listResponse;
    assert.equal(listRead.status(), 200);
    const { data } = await listRead.json();
    assert.equal(data.length, 1);
    const row = data[0];
    assert.equal(row.request_id, overrun.quote.request_id);
    assert.equal(row.project_id, fixture.project_id);
    assert.equal(row.wallet_authorized_cost_micros, '10');
    assert.equal(row.calculated_customer_cost_micros, '100');
    assert.equal(row.charged_customer_cost_micros, '0');
    assert.equal(row.uncovered_customer_cost_micros, '90');
    const overrunResponse = getResponse(quotePath + overrun.quote.request_id);
    await scope.getByRole('button', { name: overrun.quote.request_id, exact: true }).click();
    const overrunRead = await overrunResponse;
    assert.equal(overrunRead.status(), 200);
    assert.deepEqual(await overrunRead.json(), overrun);
    assert.equal(overrun.settlement_pending_reason, 'customer_authorization_exceeded');
    await waitVisible(amount(charged, '¥0.00'));
    await waitVisible(scope.getByText(pendingLabel, { exact: true }));
    await waitVisible(scope.getByText(overrun.quote.pricing_version, { exact: true }));
  }
  return {
    settled_id: settled.quote.request_id, overrun_id: overrun.quote.request_id,
    settled_charged_micros: '7', overrun_charged_micros: '0', overrun_uncovered_micros: '90',
    overrun_reason: overrun.settlement_pending_reason, locales: ['zh-CN', 'en-US'],
  };
}
try {
  await page.goto('/login');
  assert.equal(await page.locator('html').getAttribute('lang'), 'en-US', 'fresh sign-in defaults to English');
  await page.getByRole('button', { name: '中文', exact: true }).click();
  await page.getByLabel('用户名').fill('source-browser-platform_admin');
  await page.locator('input[type="password"]').fill(fixture.password);
  await clickAndResponse(page.getByRole('button', { name: '登录', exact: true }), '/api/v1/auth/login');
  await page.waitForURL(url => url.pathname !== '/login');
  await page.goto('/integrations?tab=xinsight');
  await page.locator('summary').filter({ hasText: /^维护与技术详情$/ }).click();
  const source = page.getByRole('region', { name: '来源事实连接' });
  await waitVisible(source.getByText('未配置 · 来源 synthetic-source-browser', { exact: true }));
  await fs.writeFile(path.join(output, 'initial-ui.txt'), await source.innerText());
  await source.getByLabel('来源凭据注册信息').fill(JSON.stringify(fixture.registration));
  await source.getByLabel('来源接收端 IP').fill('127.0.0.1/32');
  await clickAndResponse(source.getByRole('button', { name: '准备来源凭据', exact: true }), `${sourcePath}/prepare`);
  await source.getByRole('button', { name: '激活来源凭据', exact: true }).click();
  await clickAndResponse(page.getByRole('button', { name: '确定', exact: true }), `${sourcePath}/activate`);
  await clickAndResponse(source.getByRole('button', { name: '恢复来源读取', exact: true }), `${sourcePath}/resume`);
  await waitVisible(source.getByText('读取已启用', { exact: false }));
  const receiverHeaders = { Authorization: `Bearer ${fixture.receiver_token}` };
  assert.equal((await context.request.get(sourceRead, { headers: receiverHeaders })).status(), 200);
  journeys.push('real login, explicit A+D registration, activation and restricted source read');
  const scope = page.getByRole('region', { name: '预算来源范围' });
  await scope.getByLabel('企业项目范围引用').fill('browser-synthetic-scope');
  const bound = await clickAndResponse(scope.getByRole('button', { name: '绑定本项目预算来源', exact: true }), apiPath);
  assert.equal(bound.revision, '1');
  await waitVisible(scope.getByText(/版本 1 · 已绑定本项目/));
  await scope.getByRole('button', { name: '撤销预算绑定', exact: true }).click();
  const revoked = await clickAndResponse(page.getByRole('button', { name: '确定', exact: true }), apiPath);
  assert.equal(revoked.revision, '2');
  await waitVisible(scope.getByText(/版本 2 · 已撤销/));
  const rebound = await clickAndResponse(scope.getByRole('button', { name: '重新绑定本项目', exact: true }), apiPath);
  assert.equal(rebound.revision, '3');
  await waitVisible(scope.getByText(/版本 3 · 已绑定本项目/));
  journeys.push('explicit project binding, revoke and rebind using returned revisions 1/2/3');
  await clickAndResponse(source.getByRole('button', { name: '暂停来源读取', exact: true }), `${sourcePath}/pause`);
  const paused = await context.request.get(sourceRead, { headers: receiverHeaders });
  assert.equal(paused.status(), 503);
  assert.equal((await paused.json()).error.code, 'source_paused');
  await clickAndResponse(source.getByRole('button', { name: '恢复来源读取', exact: true }), `${sourcePath}/resume`);
  assert.equal((await context.request.get(sourceRead, { headers: receiverHeaders })).status(), 200);
  journeys.push('pause denies source read; resume restores current authorized read');
  await source.screenshot({ path: path.join(output, 'source-budget-active.png') });
  await fs.writeFile(path.join(output, 'final-ui.txt'), await source.innerText());
  const standard_pricing = await verifyStandardPricing();
  assert.deepEqual(errors, []);
  const report = { status: 'passed', journeys, standard_pricing, browser_errors: errors, scope: 'built frontend and real local handlers; synthetic identities; no external integration', screenshot: 'source-budget-active.png' };
  await fs.writeFile(path.join(output, 'report.json'), JSON.stringify(report, null, 2) + '\n');
  console.log(JSON.stringify(report));
} catch (error) {
  await fs.writeFile(path.join(output, 'failed-ui.txt'), await page.locator('body').innerText());
  throw error;
} finally {
  await context.close();
  await browser.close();
}
