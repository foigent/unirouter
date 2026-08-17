import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const fixture = JSON.parse(process.env.MODEL_BROWSER_FIXTURE || '{}');
for (const field of ['base_url', 'username', 'password', 'provider_url', 'provider_key', 'public_model', 'upstream_model', 'gateway_api_key']) {
  assert.ok(fixture[field], `MODEL_BROWSER_FIXTURE.${field} is required`);
}

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ baseURL: fixture.base_url, serviceWorkers: 'block' });
const page = await context.newPage();
const browserErrors = [];
const apiEvents = [];
page.on('pageerror', (error) => browserErrors.push(error.stack || error.message));
page.on('console', (message) => {
  if (message.type() === 'error') browserErrors.push(message.text());
});
page.on('request', (request) => {
  if (request.url().includes('/api/') || request.url().includes('/v1/chat/completions')) {
    apiEvents.push(`request ${request.method()} ${new URL(request.url()).pathname}`);
  }
});
page.on('response', (response) => {
  if (response.url().includes('/api/') || response.url().includes('/v1/chat/completions')) {
    apiEvents.push(`response ${response.status()} ${new URL(response.url()).pathname}`);
  }
});

async function selectOption(label, option) {
  const field = page.locator('.semi-form-field').filter({ hasText: label }).first();
  await field.locator('.semi-select').click();
  await page.locator('.semi-select-option').filter({ hasText: option }).last().click();
}

try {
  await page.goto('/login');
  await page.getByLabel('用户名').fill(fixture.username);
  await page.locator('input[type="password"]').fill(fixture.password);
  const loginResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/login') && response.request().method() === 'POST');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  assert.equal((await loginResponse).status(), 200);
  await page.waitForURL((url) => url.pathname !== '/login');

  await page.goto('/channels');
  await page.getByRole('heading', { name: '模型与端点', exact: true }).waitFor({ state: 'visible' });
  await page.getByRole('button', { name: '新增供应商端点', exact: true }).click();
  await page.getByRole('button', { name: '列表中没有？新增自定义兼容供应商', exact: true }).click();

  await page.getByLabel('供应商标识').fill('deepseek-browser');
  await page.getByLabel('连接名称（可选）').fill('Model Browser DeepSeek');
  await page.getByLabel('Base URL').fill(fixture.provider_url);
  await page.getByRole('textbox', { name: /新的公共模型 ID/ }).fill(fixture.public_model);
  await page.getByLabel('上游模型 / 部署 ID').fill(fixture.upstream_model);
  await page.getByLabel('供应商 API Key').fill(fixture.provider_key);
  await selectOption('路由角色', '主端点（优先使用）');

  const createResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/channels') && response.request().method() === 'POST');
  await page.getByRole('button', { name: '保存端点与凭证', exact: true }).click();
  const created = await createResponse;
  assert.equal(created.status(), 201, await created.text());

  const modelRow = page.locator('.semi-table-row').filter({ hasText: fixture.public_model }).first();
  await modelRow.waitFor({ state: 'visible' });
  await modelRow.getByRole('button', { name: '管理端点', exact: true }).click();
  const endpointSheet = page.locator('.semi-sidesheet:visible').filter({ hasText: fixture.public_model }).last();
  await endpointSheet.waitFor({ state: 'visible' });

  await endpointSheet.getByRole('button', { name: '真实检测', exact: true }).click();
  const testModal = page.locator('.semi-modal:visible').filter({ hasText: `检测 Model Browser DeepSeek？` }).last();
  await testModal.waitFor({ state: 'visible' });
  const [tested] = await Promise.all([
    page.waitForResponse((response) => /\/api\/v1\/channels\/\d+\/test$/.test(new URL(response.url()).pathname)),
    testModal.getByText('发起真实检测', { exact: true }).click(),
  ]);
  assert.equal(tested.status(), 200, await tested.text());
  const testedBody = await tested.json();
  assert.equal(testedBody.success, true);
  assert.equal(testedBody.model, fixture.public_model);
  assert.equal(testedBody.upstream_model, fixture.upstream_model);

  await endpointSheet.getByRole('button', { name: '首次启用', exact: true }).waitFor({ state: 'visible' });
  await endpointSheet.getByRole('button', { name: '首次启用', exact: true }).click();
  const [activated] = await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/v1/model-endpoints/activate') && response.request().method() === 'POST'),
    page.locator('.semi-modal:visible').getByText('确认首次启用', { exact: true }).click(),
  ]);
  assert.equal(activated.status(), 200, await activated.text());

  await endpointSheet.getByText('稳定路由', { exact: true }).waitFor({ state: 'visible' });
  await endpointSheet.getByText('已进入候选', { exact: true }).waitFor({ state: 'visible' });

  const gatewayResult = await page.evaluate(async ({ key, model }) => {
    const response = await fetch('/v1/chat/completions', {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${key}`,
        'Content-Type': 'application/json',
        'X-Request-ID': 'model-browser-gateway-call',
      },
      body: JSON.stringify({ model, messages: [{ role: 'user', content: 'golden path' }], stream: false }),
    });
    return { status: response.status, body: await response.text() };
  }, { key: fixture.gateway_api_key, model: fixture.public_model });
  assert.equal(gatewayResult.status, 200, gatewayResult.body);
  assert.match(gatewayResult.body, /golden-path-ok/);
  assert.match(gatewayResult.body, /_uniroute/);
  assert.deepEqual(browserErrors, []);
  console.log('MODEL_ENDPOINT_GOLDEN_PATH create=ok probe=ok activate=ok gateway=ok');
} catch (error) {
  const body = (await page.locator('body').innerText()).slice(0, 4_000);
  throw new Error(`${error.stack || error.message}\napiEvents=${apiEvents.join(' | ')}\nbrowserErrors=${browserErrors.join(' | ')}\nbody=${body}`);
} finally {
  await context.close();
  await browser.close();
}
