import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const fixture = JSON.parse(process.env.MODEL_BROWSER_FIXTURE || '{}');
for (const field of ['base_url', 'username', 'password', 'provider_url', 'provider_key', 'public_model', 'upstream_model']) {
  assert.ok(fixture[field], `MODEL_BROWSER_FIXTURE.${field} is required`);
}

const operationID = 'openai.chat_completions.v1';
const publicContextLimit = 128_000;
const publicOutputLimit = 8_192;
const deploymentOutputLimit = 4_096;

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
  if (request.url().includes('/api/')) {
    apiEvents.push(`request ${request.method()} ${new URL(request.url()).pathname}`);
  }
});
page.on('response', (response) => {
  if (response.url().includes('/api/')) {
    apiEvents.push(`response ${response.status()} ${new URL(response.url()).pathname}`);
  }
});

async function selectOption(root, label, option) {
  const field = root.locator('.semi-form-field').filter({ hasText: label }).first();
  await field.locator('.semi-select').click();
  await page.locator('.semi-select-option:visible').getByText(option, { exact: true }).last().click();
  await field.getByText(option, { exact: true }).waitFor({ state: 'visible' });
}

function apiResponse(method, pathname) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === method && url.pathname === pathname;
  });
}

async function responseJSON(response, expectedStatus) {
  const body = await response.text();
  assert.equal(response.status(), expectedStatus, body);
  return JSON.parse(body);
}

try {
  await page.goto('/login');
  await page.getByLabel('用户名').fill(fixture.username);
  await page.locator('input[type="password"]').fill(fixture.password);
  const [loginResponse] = await Promise.all([
    apiResponse('POST', '/api/v1/auth/login'),
    page.getByRole('button', { name: '登录', exact: true }).click(),
  ]);
  assert.equal(loginResponse.status(), 200);
  await page.waitForURL((url) => url.pathname !== '/login');

  await page.goto('/channels');
  await page.getByRole('heading', { name: '模型接入', exact: true }).waitFor({ state: 'visible' });
  await page.getByRole('button', { name: '接入模型', exact: true }).click();
  const createSheet = page.locator('.semi-sidesheet:visible').filter({ hasText: '接入模型' }).last();
  await createSheet.waitFor({ state: 'visible' });

  await selectOption(createSheet, '接入模板', '自定义 OpenAI 兼容供应商');
  await createSheet.getByLabel('连接名称').fill('Model Browser DeepSeek');
  await createSheet.getByLabel('供应商标识').fill('deepseek-browser');
  await createSheet.getByLabel('Base URL').fill(fixture.provider_url);
  await createSheet.getByLabel('公共模型 ID').fill(fixture.public_model);
  await selectOption(createSheet, '目录生命周期', 'GA');
  await createSheet.getByLabel('公共模型上下文上限（Token）').fill(String(publicContextLimit));
  await createSheet.getByLabel('公共模型单次输出上限（Token）').fill(String(publicOutputLimit));
  await createSheet.getByLabel('上游模型 / 部署 ID').fill(fixture.upstream_model);
  await createSheet.getByLabel('Deployment 上下文上限（Token）').fill(String(publicContextLimit));
  await createSheet.getByLabel('Deployment 默认单次输出（Token）').fill(String(deploymentOutputLimit));
  await createSheet.getByLabel('供应商 API Key').fill(fixture.provider_key);

  const [createResponse] = await Promise.all([
    apiResponse('POST', '/api/v1/model-deployments'),
    createSheet.getByRole('button', { name: '创建模型接入', exact: true }).click(),
  ]);
  const created = await responseJSON(createResponse, 201);
  assert.equal(created.idempotency_replay, false);
  assert.deepEqual(created.reused, {
    connection: false,
    public_model: false,
    deployment: false,
    credential: false,
    operation: false,
  });
  assert.equal(created.application.public_model.model_name, fixture.public_model);
  assert.equal(created.application.public_model.context_length, publicContextLimit);
  assert.equal(created.application.public_model.output_limit, publicOutputLimit);
  assert.equal(created.application.deployment.upstream_model, fixture.upstream_model);
  assert.equal(created.application.deployment.max_context, publicContextLimit);
  assert.equal(created.application.deployment.default_max_output, deploymentOutputLimit);
  assert.equal(created.application.operations.length, 1);
  assert.equal(created.application.operations[0].operation_id, operationID);

  const deploymentID = created.application.deployment.id;
  const publicModelID = created.application.public_model.id;
  const credentialBindingID = created.application.credential.bindings[0]?.id;
  assert.ok(deploymentID > 0);
  assert.ok(publicModelID > 0);
  assert.ok(credentialBindingID > 0);

  let endpointSheet = page.locator('.semi-sidesheet:visible').filter({ hasText: `Deployment #${deploymentID}` }).last();
  await endpointSheet.waitFor({ state: 'visible' });
  await endpointSheet.getByRole('button', { name: '用此绑定探测精确 Operation', exact: true }).click();
  const probeModal = page.locator('.semi-modal:visible').filter({ hasText: `探测 Deployment #${deploymentID}？` }).last();
  await probeModal.waitFor({ state: 'visible' });
  const probePath = `/api/v1/model-deployments/${deploymentID}/operations/${operationID}/probes`;
  const [probeResponse] = await Promise.all([
    apiResponse('POST', probePath),
    probeModal.getByRole('button', { name: 'confirm', exact: true }).click(),
  ]);
  const probed = await responseJSON(probeResponse, 200);
  assert.equal(probed.deployment_id, deploymentID);
  assert.equal(probed.operation_id, operationID);
  assert.equal(probed.credential_binding_id, credentialBindingID);
  assert.equal(probed.success, true);
  assert.equal(probed.provider_success, true);
  assert.equal(probed.current, true);
  assert.equal(probed.stale, false);
  assert.equal(probed.operation_advanced, true);
  await endpointSheet.getByText('当前成功', { exact: true }).waitFor({ state: 'visible' });

  await endpointSheet.getByRole('button', { name: 'close', exact: true }).click();
  await endpointSheet.waitFor({ state: 'hidden' });

  const [refreshCollection] = await Promise.all([
    apiResponse('GET', '/api/v1/model-deployment-releases'),
    page.getByRole('button', { name: /刷新发布事实$/ }).click(),
  ]);
  assert.equal(refreshCollection.status(), 200);
  const authorButton = page.getByRole('button', { name: /编排新 Release$/ });
  await authorButton.click();
  const authorSheet = page.locator('.semi-sidesheet:visible').filter({ hasText: '编排 Canonical Release' }).last();
  await authorSheet.waitFor({ state: 'visible' });
  await authorSheet.getByLabel(`选择 deployment ${deploymentID} ${operationID}`).check();
  await authorSheet.getByLabel('Authoring reason').fill('Canonical browser acceptance: one exact UI-created deployment');

  const [authorResponse] = await Promise.all([
    apiResponse('POST', '/api/v1/model-deployment-releases'),
    authorSheet.getByRole('button', { name: '生成 Sealed Validated Release', exact: true }).click(),
  ]);
  const authored = await responseJSON(authorResponse, 201);
  assert.equal(authored.idempotency_replay, false);
  assert.equal(authored.release.public_model_id, publicModelID);
  assert.equal(authored.release.stage, 'validated');
  assert.equal(authored.release.version, 2);
  assert.equal(authored.release.route_items_count, 1);
  assert.deepEqual(authored.selections, [{ deployment_id: deploymentID, operation_id: operationID }]);
  const releaseID = authored.release.id;
  const releaseRef = authored.release.release_ref;
  assert.ok(releaseID > 0);
  assert.ok(releaseRef);
  await page.getByText(`原子发布编排收据 #${authored.receipt_id}`, { exact: true }).waitFor({ state: 'visible' });

  const activateButton = page.getByRole('button', { name: '首次启用', exact: true });
  await activateButton.waitFor({ state: 'visible' });
  await activateButton.click();
  const activateModal = page.locator('.semi-modal:visible').filter({ hasText: '首次启用？' }).last();
  await activateModal.waitFor({ state: 'visible' });
  const activatePath = `/api/v1/model-deployment-releases/${releaseRef}/activate-initial`;
  const [activateResponse] = await Promise.all([
    apiResponse('POST', activatePath),
    activateModal.getByRole('button', { name: 'confirm', exact: true }).click(),
  ]);
  const activated = await responseJSON(activateResponse, 200);
  assert.equal(activated.idempotency_replay, false);
  assert.equal(activated.action, 'activate_initial');
  assert.equal(activated.target_release_ref, releaseRef);
  assert.equal(activated.slot.public_model_id, publicModelID);
  assert.equal(Object.hasOwn(activated.slot, 'before_public_model_id'), false);
  assert.equal(Object.hasOwn(activated.slot, 'after_public_model_id'), false);
  assert.equal(activated.slot.after_stable_release_id, releaseID);
  assert.equal(activated.slot.after_version, 1);
  assert.deepEqual(activated.qualifications.map((item) => ({
    deployment_id: item.deployment_id,
    operation_id: item.operation_id,
    qualified: item.qualified,
  })), [{ deployment_id: deploymentID, operation_id: operationID, qualified: true }]);
  const targetRelease = activated.releases.find((item) => item.role === 'target');
  assert.equal(targetRelease?.after_stage, 'active');
  assert.equal(targetRelease?.after_version, 3);
  await page.getByText(`Lifecycle 收据 #${activated.receipt_id}`, { exact: true }).waitFor({ state: 'visible' });

  await page.goto(`/channels?deployment_id=${deploymentID}&operation_id=${encodeURIComponent(operationID)}&release_ref=${encodeURIComponent(releaseRef)}`);
  endpointSheet = page.locator('.semi-sidesheet:visible').filter({ hasText: `Deployment #${deploymentID}` }).last();
  await endpointSheet.waitFor({ state: 'visible' });
  await endpointSheet.getByText(`stable ${releaseRef}`, { exact: true }).waitFor({ state: 'visible' });
  await endpointSheet.getByRole('button', { name: '查看 Stable Release', exact: true }).waitFor({ state: 'visible' });

  assert.deepEqual(browserErrors, []);
  console.log('MODEL_ENDPOINT_CANONICAL_PATH create=ok probe=ok author=ok activate_initial=ok stable=ok');
} catch (error) {
  const body = (await page.locator('body').innerText()).slice(0, 4_000);
  throw new Error(`${error.stack || error.message}\napiEvents=${apiEvents.join(' | ')}\nbrowserErrors=${browserErrors.join(' | ')}\nbody=${body}`);
} finally {
  await context.close();
  await browser.close();
}
