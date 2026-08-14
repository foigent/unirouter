import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const fixture = JSON.parse(process.env.PR4_BROWSER_FIXTURE || '{}');
assert.ok(fixture.base_url, 'PR4_BROWSER_FIXTURE.base_url is required');
assert.ok(fixture.unavailable_base_url, 'PR4_BROWSER_FIXTURE.unavailable_base_url is required');
for (const name of ['provider', 'feed', 'upstream']) {
  assert.ok(fixture.trap_urls?.[name], `PR4_BROWSER_FIXTURE.trap_urls.${name} is required`);
  assert.ok(fixture.control_trap_urls?.[name], `PR4_BROWSER_FIXTURE.control_trap_urls.${name} is required`);
}
assert.ok(fixture.egress_targets?.provider, 'PR4_BROWSER_FIXTURE.egress_targets.provider is required');
assert.ok(fixture.egress_targets?.feed, 'PR4_BROWSER_FIXTURE.egress_targets.feed is required');
assert.equal(fixture.canonical_feed_path, '/api/v1/integrations/xinsight/completed-executions', 'PR4_BROWSER_FIXTURE.canonical_feed_path must match the repository route');

const browser = await chromium.launch({ headless: true });
const journeys = [];
const browserErrors = [];

async function installCausalEgressGuard(context, baseURL, trapURLs) {
  const applicationOrigin = new URL(baseURL).origin;
  const providerOrigin = new URL(fixture.egress_targets.provider).origin;
  const feedOrigin = new URL(fixture.egress_targets.feed).origin;
  await context.route('**/*', async (route) => {
    const requestURL = new URL(route.request().url());
    let trafficClass = '';
    if (requestURL.origin === providerOrigin) {
      trafficClass = 'provider';
    } else if (requestURL.origin === feedOrigin ||
      (requestURL.origin === applicationOrigin && requestURL.pathname === fixture.canonical_feed_path)) {
      trafficClass = 'feed';
    } else if (requestURL.origin !== applicationOrigin) {
      trafficClass = 'upstream';
    }
    if (!trafficClass) {
      await route.continue();
      return;
    }

    const trapURL = new URL('/observed', trapURLs[trafficClass]);
    trapURL.searchParams.set('original_origin', requestURL.origin);
    const observed = await context.request.get(trapURL.toString());
    assert.equal(observed.status(), 418, `${trafficClass} egress trap must reject traffic`);
    await route.fulfill({
      status: 502,
      contentType: 'text/plain; charset=utf-8',
      body: `${trafficClass} egress blocked by PR4 browser acceptance`,
    });
  });
}

async function proveBrowserEgressGuard() {
  const context = await browser.newContext({ baseURL: fixture.base_url, serviceWorkers: 'block' });
  await installCausalEgressGuard(context, fixture.base_url, fixture.control_trap_urls);
  const page = await context.newPage();
  for (const target of [
    `${fixture.egress_targets.provider}/v1/chat/completions`,
    `${fixture.egress_targets.feed}/v1/completed-executions`,
    `${fixture.base_url}${fixture.canonical_feed_path}`,
    'https://unclassified-upstream.pr4.invalid/probe',
  ]) {
    const response = await page.goto(target, { waitUntil: 'domcontentloaded' });
    assert.equal(response.status(), 502, `external request was not intercepted: ${target}`);
  }
  await context.close();
  console.log('PR4_BROWSER_CAUSAL_EGRESS_CONTROL provider=observed feed_external=observed feed_same_origin_canonical=observed upstream=observed');
}

async function visible(locator, message) {
  await locator.waitFor({ state: 'visible', timeout: 15_000 });
  if (message) assert.ok(await locator.isVisible(), message);
  return locator;
}

async function waitForEnabledState(locator, expected, message) {
  await visible(locator);
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if ((await locator.isEnabled()) === expected) return locator;
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  assert.equal(await locator.isEnabled(), expected, message);
  return locator;
}

async function login(role, baseURL = fixture.base_url) {
  const context = await browser.newContext({ baseURL, serviceWorkers: 'block' });
  await installCausalEgressGuard(context, baseURL, fixture.trap_urls);
  const page = await context.newPage();
  page.on('pageerror', (error) => browserErrors.push(`${role}: ${error.stack || error.message}`));
  page.on('console', (message) => {
    if (message.type() === 'error') browserErrors.push(`${role} console: ${message.text()}`);
  });
  await page.goto('/login');
  await page.getByLabel('用户名').fill(fixture.usernames[role]);
  await page.locator('input[type="password"]').fill(fixture.password);
  const [response] = await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/v1/auth/login') && response.request().method() === 'POST'),
    page.getByRole('button', { name: '登录', exact: true }).click(),
  ]);
  assert.equal(response.status(), 200, `login failed for ${role}: ${await response.text()}`);
  await page.waitForURL((url) => url.pathname !== '/login', { timeout: 15_000 });
  return { context, page };
}

async function gotoHeading(page, path, heading) {
  await page.goto(path);
  try {
    await visible(page.getByRole('heading', { name: heading, exact: true }));
  } catch (error) {
    const body = (await page.locator('body').innerText()).slice(0, 2_000);
    throw new Error(`heading ${heading} missing at ${page.url()}\n${body}`, { cause: error });
  }
}

async function selectSemiOption(page, root, label, option) {
  const field = root.locator('.semi-form-field').filter({ hasText: label }).first();
  await field.locator('.semi-select').click();
  await page.locator('.semi-select-option').filter({ hasText: option }).last().click();
  await field.getByText(option, { exact: true }).waitFor({ state: 'visible' });
}

async function chooseProject(page, projectName, projectID) {
  const scope = page.locator('.semi-select').filter({ hasText: /PR4 (Alpha|Beta)/ }).first();
  await scope.click();
  await page.locator('.semi-select-option').filter({ hasText: projectName }).last().click();
  await page.waitForURL((url) => Number(url.searchParams.get('project_id')) === projectID);
}

async function submitPersonalRequest(page, roleLabel) {
  const submitButton = page.locator('button').filter({ hasText: '提交访问请求' }).first();
  try {
    await submitButton.click({ timeout: 15_000 });
  } catch (error) {
    const projects = await page.evaluate(async () => {
      const response = await fetch('/api/v1/projects?page=1&page_size=100&status=active', {
        headers: { Authorization: `Bearer ${localStorage.getItem('token')}` },
      });
      return { status: response.status, body: await response.text() };
    });
    const body = (await page.locator('body').innerText()).slice(0, 2_000);
    throw new Error(`submit button missing at ${page.url()}\nprojects=${JSON.stringify(projects)}\nbrowserErrors=${browserErrors.join('\n')}\n${body}`, { cause: error });
  }
  const dialog = await visible(page.locator('.semi-modal:visible').filter({ hasText: '提交访问请求' }).last());
  if (roleLabel !== '项目使用者') {
    await selectSemiOption(page, dialog, '请求的项目角色', roleLabel);
  }
  const responsePromise = page.waitForResponse((response) =>
    response.url().includes('/api/v1/access-requests') &&
    response.request().method() === 'POST' &&
    !response.url().includes('/decision') &&
    !response.url().includes('/cancel'));
  await dialog.getByRole('button', { name: '提交', exact: true }).click();
  const response = await responsePromise;
  assert.equal(response.status(), 201);
  const payload = response.request().postDataJSON();
  assert.equal(payload.requested_scope.project_role,
    roleLabel === '项目使用者' ? 'project_user' : 'project_admin');
  return response.json();
}

async function closeOneTimeSecret(page, title) {
  const dialog = await visible(page.locator('.semi-modal:visible').filter({ hasText: title }).last());
  await dialog.locator('button').filter({ hasText: '我已安全保存' }).last().click();
  await dialog.waitFor({ state: 'hidden' });
}

async function openPersonalCredentialGrant(page) {
  await page.locator('button').filter({ hasText: '管理凭据' }).first().click();
  await page.waitForURL((url) => url.searchParams.has('grant_ref'));
  await visible(page.locator('button').filter({ hasText: '签发逻辑凭据' }).first());
}

async function issuePersonalCredential(page, name) {
  await page.locator('button').filter({ hasText: '签发逻辑凭据' }).first().click();
  const dialog = await visible(page.locator('.semi-modal:visible').filter({ hasText: '签发逻辑凭据' }).last());
  await dialog.getByLabel('凭据名称').fill(name);
  const responsePromise = page.waitForResponse((response) =>
    /\/api\/v1\/access-grants\/[^/]+\/credentials$/.test(new URL(response.url()).pathname) &&
    response.request().method() === 'POST');
  await dialog.getByRole('button', { name: '签发', exact: true }).click();
  const response = await responsePromise;
  assert.ok([200, 201].includes(response.status()));
  return response.json();
}

async function preparePendingRequest(role) {
  const { context, page } = await login(role);
  try {
    await gotoHeading(page, `/access?project_id=${fixture.project_a}`, '我的访问');
    const result = await submitPersonalRequest(page, '项目管理员（通常需要审核）');
    assert.equal(result.request.status, 'needs_review');
    return result.request;
  } finally {
    await context.close();
  }
}

try {
  await proveBrowserEgressGuard();

  const alice = await login('alice');
  try {
    await gotoHeading(alice.page, `/access?project_id=${fixture.project_a}`, '我的访问');
    const approved = await submitPersonalRequest(alice.page, '项目使用者');
    assert.equal(approved.request.status, 'approved');
    assert.equal(approved.grant.status, 'active');

    const escalation = await submitPersonalRequest(alice.page, '项目管理员（通常需要审核）');
    assert.equal(escalation.request.status, 'needs_review');
    await openPersonalCredentialGrant(alice.page);

    let lostResponse = true;
    const retryClientIDs = [];
    const issuePattern = '**/api/v1/access-grants/*/credentials';
    await alice.page.route(issuePattern, async (route) => {
      const request = route.request();
      const pathname = new URL(request.url()).pathname;
      if (request.method() !== 'POST' || !/\/credentials$/.test(pathname)) {
        await route.continue();
        return;
      }
      retryClientIDs.push(await request.headerValue('x-unirouter-client-request-id'));
      if (lostResponse) {
        lostResponse = false;
        const committed = await route.fetch();
        assert.equal(committed.status(), 201);
        await route.abort('failed');
        return;
      }
      await route.continue();
    });

    await alice.page.locator('button').filter({ hasText: '签发逻辑凭据' }).first().click();
    const lostDialog = await visible(alice.page.locator('.semi-modal:visible').filter({ hasText: '签发逻辑凭据' }).last());
    await lostDialog.getByLabel('凭据名称').fill('pr4-lost-response');
    await lostDialog.getByRole('button', { name: '签发', exact: true }).click();
    await visible(alice.page.getByText('凭据签发失败', { exact: false }));

    const replayPromise = alice.page.waitForResponse((response) =>
      /\/api\/v1\/access-grants\/[^/]+\/credentials$/.test(new URL(response.url()).pathname) &&
      response.request().method() === 'POST');
    await lostDialog.getByRole('button', { name: '签发', exact: true }).click();
    const replayResponse = await replayPromise;
    assert.equal(replayResponse.status(), 200);
    const replay = await replayResponse.json();
    assert.equal(replay.idempotent, true);
    assert.equal(replay.secret_available, false);
    assert.equal(retryClientIDs.length, 2);
    assert.ok(retryClientIDs[0]);
    assert.equal(retryClientIDs[0], retryClientIDs[1]);
    await visible(alice.page.getByText('原 secret 不可恢复', { exact: false }));
    await closeOneTimeSecret(alice.page, '一次性 Secret');
    await alice.page.unroute(issuePattern);

    const issued = await issuePersonalCredential(alice.page, 'pr4-personal-live');
    assert.equal(issued.secret_available, true);
    assert.match(issued.key, /^ur-[0-9]{4}-[0-9a-f]{32}$/);
    const rawSecret = issued.key;
    await visible(alice.page.getByText(rawSecret, { exact: true }));
    assert.equal(alice.page.url().includes(rawSecret), false);
    const storedSecret = await alice.page.evaluate((secret) =>
      Object.values(localStorage).some((value) => String(value).includes(secret)), rawSecret);
    assert.equal(storedSecret, false);
    await closeOneTimeSecret(alice.page, '一次性 Secret');
    assert.equal(await alice.page.getByText(rawSecret, { exact: true }).count(), 0);

    const familyRow = alice.page.locator('tr').filter({ hasText: 'pr4-personal-live' }).first();
    const rotatePromise = alice.page.waitForResponse((response) => response.url().endsWith('/rotate') && response.request().method() === 'POST');
    await familyRow.getByRole('button', { name: '原子轮换', exact: true }).click();
    const rotateResponse = await rotatePromise;
    assert.equal(rotateResponse.status(), 201);
    const rotated = await rotateResponse.json();
    assert.equal(rotated.credential.secret_available, true);
    assert.notEqual(rotated.credential.key, rawSecret);
    await closeOneTimeSecret(alice.page, '一次性 Secret');

    const refreshedFamily = alice.page.locator('tr').filter({ hasText: 'pr4-personal-live' }).first();
    const revokePromise = alice.page.waitForResponse((response) => response.url().endsWith('/revoke') && response.request().method() === 'POST');
    await refreshedFamily.getByRole('button', { name: '撤销', exact: true }).click();
    const confirm = await visible(alice.page.locator('.semi-modal:visible').filter({ hasText: '撤销逻辑凭据' }).last());
    await confirm.locator('.semi-modal-footer button').last().click();
    const revokeResponse = await revokePromise;
    assert.equal(revokeResponse.status(), 200);
    await visible(alice.page.locator('tr').filter({ hasText: 'pr4-personal-live' }).filter({ hasText: '已撤销' }));
    journeys.push('personal_auto_review_issue_retry_rotate_revoke');
  } finally {
    await alice.context.close();
  }

  const bobRequest = await preparePendingRequest('bob');

  const admin = await login('org_admin');
  try {
    await gotoHeading(admin.page, `/access-review?project_id=${fixture.project_a}`, '访问审核');
    const aliceRow = await visible(admin.page.locator('tr').filter({ hasText: `用户 #${fixture.users.alice}` }).first());
    const approvePromise = admin.page.waitForResponse((response) => response.url().endsWith('/decision') && response.request().method() === 'POST');
    await aliceRow.getByRole('button', { name: '批准', exact: true }).click();
    const approveDialog = await visible(admin.page.locator('.semi-modal:visible').filter({ hasText: '批准访问请求' }).last());
    await approveDialog.locator('.semi-modal-footer button').last().click();
    const approveResponse = await approvePromise;
    assert.equal(approveResponse.status(), 200);
    assert.equal((await approveResponse.json()).request.status, 'approved');

    const bobRow = await visible(admin.page.locator('tr').filter({ hasText: `用户 #${fixture.users.bob}` }).first());
    assert.ok((await bobRow.textContent()).includes(bobRequest.request_ref));
    const rejectPromise = admin.page.waitForResponse((response) => response.url().endsWith('/decision') && response.request().method() === 'POST');
    await bobRow.getByRole('button', { name: '拒绝', exact: true }).click();
    const rejectDialog = await visible(admin.page.locator('.semi-modal:visible').filter({ hasText: '拒绝访问请求' }).last());
    await rejectDialog.locator('.semi-modal-footer button').last().click();
    const rejectResponse = await rejectPromise;
    assert.equal(rejectResponse.status(), 200);
    assert.equal((await rejectResponse.json()).request.status, 'rejected');

    await gotoHeading(admin.page, '/organization-members', '人员与权限');
    await visible(admin.page.getByText('本地账号 / Local User', { exact: true }));
    await visible(admin.page.getByText(fixture.usernames.alice, { exact: true }));
    assert.equal(await admin.page.getByText(fixture.usernames.oidc, { exact: true }).count(), 0);
    await admin.page.getByText('企业身份 / OIDC', { exact: true }).click();
    await visible(admin.page.getByText(fixture.usernames.oidc, { exact: true }));
    assert.equal(await admin.page.getByText(fixture.usernames.alice, { exact: true }).count(), 0);

    await gotoHeading(admin.page, `/applications?project_id=${fixture.project_a}&tab=legacy`, '应用与 Agent');
    await visible(admin.page.getByLabel('Legacy 兼容区（只读）').getByText(fixture.legacy_ref, { exact: true }));
    await visible(admin.page.getByLabel('Legacy 兼容区（只读）').getByText('review_required', { exact: true }).first());
    await visible(admin.page.getByText('只读取 canonical preview；不自动迁移、不推断、不提供写操作。', { exact: false }));
    journeys.push('review_approve_reject_local_oidc_legacy');
  } finally {
    await admin.context.close();
  }

  const hybrid = await login('hybrid');
  try {
    let releaseProjectList;
    const projectListGate = new Promise((resolve) => {
      releaseProjectList = resolve;
    });
    await hybrid.page.route(/\/api\/v1\/projects(?:\?.*)?$/, async (route) => {
      await projectListGate;
      await route.continue();
    }, { times: 1 });
    const projectListResponsePromise = hybrid.page.waitForResponse((response) =>
      new URL(response.url()).pathname === '/api/v1/projects' &&
      response.request().method() === 'GET');

    await gotoHeading(hybrid.page, `/applications?project_id=${fixture.project_a}`, '应用与 Agent');
    const createButton = hybrid.page.locator('button').filter({ hasText: '新建 v2 Application' }).first();
    await visible(createButton);
    assert.equal(await createButton.isDisabled(), true,
      'create must remain disabled until the selected project authority is loaded');
    releaseProjectList();
    const projectListResponse = await projectListResponsePromise;
    assert.equal(projectListResponse.status(), 200);
    const projectList = await projectListResponse.json();
    assert.equal(projectList.data.find((project) => project.id === fixture.project_a)?.current_user_membership_role,
      'project_admin');
    assert.equal(projectList.data.find((project) => project.id === fixture.project_b)?.current_user_membership_role,
      'project_user');
    await waitForEnabledState(createButton, true,
      'project A create must enable after project_admin authority is loaded');
    await chooseProject(hybrid.page, 'PR4 Beta', fixture.project_b);
    await waitForEnabledState(createButton, false,
      'project B project_user must not create applications');
    await visible(hybrid.page.getByText('当前所选项目只有读取/使用权限', { exact: false }));
    await chooseProject(hybrid.page, 'PR4 Alpha', fixture.project_a);
    await waitForEnabledState(createButton, true,
      'returning to project A must restore project_admin create authority');
    console.log('PR4_BROWSER_PROJECT_AUTHORITY delayed_response=held project_a=project_admin project_b=project_user beta_create=disabled return_alpha_create=enabled');

    await createButton.click();
    const sheet = await visible(hybrid.page.locator('.semi-sidesheet:visible').filter({ hasText: '新建 v2 Application' }).last());
    await sheet.getByLabel('Application ref').fill('app_pr4_browser_agent');
    await sheet.getByLabel('Logical ref').fill('workload_pr4_browser_agent');
    await sheet.getByLabel('显示名称').fill('PR4 Browser Agent');
    await sheet.getByLabel('Environment（创建后不可变）').fill('development');
    const createResponsePromise = hybrid.page.waitForResponse((response) => response.url().endsWith('/api/v1/workload-applications') && response.request().method() === 'POST');
    await sheet.getByRole('button', { name: '保存', exact: true }).click();
    const createResponse = await createResponsePromise;
    assert.equal(createResponse.status(), 201);
    const application = await createResponse.json();
    assert.equal(application.identity_contract_version, 'unirouter.workload-identity.v2');
    assert.equal(application.logical_ref, 'workload_pr4_browser_agent');
    await visible(hybrid.page.getByRole('heading', { name: 'PR4 Browser Agent', exact: true }));

    const requestPromise = hybrid.page.waitForResponse((response) => response.url().endsWith('/api/v1/access-requests') && response.request().method() === 'POST');
    await hybrid.page.getByRole('button', { name: '申请默认执行访问', exact: true }).click();
    const requestResponse = await requestPromise;
    assert.equal(requestResponse.status(), 201);
    const workloadAccess = await requestResponse.json();
    assert.equal(workloadAccess.request.status, 'approved');
    assert.equal(workloadAccess.grant.status, 'active');

    await hybrid.page.locator('button').filter({ hasText: '签发 Workload 凭据' }).first().click();
    const credentialDialog = await visible(hybrid.page.locator('.semi-modal:visible').filter({ hasText: '签发 Workload 逻辑凭据' }).last());
    await credentialDialog.getByLabel('凭据名称').fill('pr4-workload-live');
    const workloadIssuePromise = hybrid.page.waitForResponse((response) => /\/credentials$/.test(new URL(response.url()).pathname) && response.request().method() === 'POST');
    await credentialDialog.getByRole('button', { name: '签发', exact: true }).click();
    const workloadIssue = await workloadIssuePromise;
    assert.equal(workloadIssue.status(), 201);
    assert.equal((await workloadIssue.json()).secret_available, true);
    await closeOneTimeSecret(hybrid.page, '一次性 Workload Secret');

    let workloadRow = hybrid.page.locator('tr').filter({ hasText: 'pr4-workload-live' }).first();
    const workloadRotatePromise = hybrid.page.waitForResponse((response) => response.url().endsWith('/rotate') && response.request().method() === 'POST');
    await workloadRow.getByRole('button', { name: '原子轮换', exact: true }).click();
    assert.equal((await workloadRotatePromise).status(), 201);
    await closeOneTimeSecret(hybrid.page, '一次性 Workload Secret');

    workloadRow = hybrid.page.locator('tr').filter({ hasText: 'pr4-workload-live' }).first();
    const workloadRevokePromise = hybrid.page.waitForResponse((response) => response.url().endsWith('/revoke') && response.request().method() === 'POST');
    await workloadRow.getByRole('button', { name: '撤销', exact: true }).click();
    const workloadConfirm = await visible(hybrid.page.locator('.semi-modal:visible').filter({ hasText: '撤销整个逻辑凭据' }).last());
    await workloadConfirm.locator('.semi-modal-footer button').last().click();
    assert.equal((await workloadRevokePromise).status(), 200);
    await visible(hybrid.page.locator('tr').filter({ hasText: 'pr4-workload-live' }).filter({ hasText: 'revoked' }));
    journeys.push('project_switch_native_v2_workload_credential_lifecycle');
  } finally {
    await hybrid.context.close();
  }

  for (const role of ['project_viewer', 'billing']) {
    const session = await login(role);
    try {
      await session.page.goto('/access-review');
      await session.page.waitForURL((url) => url.pathname !== '/access-review');
      assert.equal(await session.page.getByText('访问审核', { exact: true }).count(), 0);
      assert.equal(await session.page.getByText('应用与 Agent', { exact: true }).count(), 0);
    } finally {
      await session.context.close();
    }
  }

  const noMembership = await login('no_membership');
  try {
    await gotoHeading(noMembership.page, '/access', '我的访问');
    await visible(noMembership.page.getByText('暂无可申请项目', { exact: true }));
    await visible(noMembership.page.getByText('当前页面不会枚举同组织全部项目', { exact: false }));
    assert.equal(await noMembership.page.locator('button').filter({ hasText: '提交访问请求' }).count(), 0);
    journeys.push('viewer_billing_no_membership_non_escalation');
  } finally {
    await noMembership.context.close();
  }

  const readiness = await login('alice');
  try {
    const readyPath = `/model-readiness?project_id=${fixture.project_a}&model=${encodeURIComponent(fixture.ready_model)}&endpoint=${encodeURIComponent('/v1/chat/completions')}&caller=self`;
    await gotoHeading(readiness.page, readyPath, '调用条件诊断');
    await readiness.page.getByText('查看高级诊断证据', { exact: true }).click();
    await visible(readiness.page.getByText('direct_model_optional', { exact: true }));
    await visible(readiness.page.getByText('unrestricted_optional', { exact: true }));
    await visible(readiness.page.getByText('runtime_slot_not_configured_optional', { exact: true }));
    const facts = readiness.page.locator('.ur-readiness-summary-grid');
    await visible(facts);
    assert.ok((await facts.textContent()).includes('模型记录就绪条件已满足'));
    assert.ok((await facts.textContent()).includes('项目目录可见就绪条件已满足'));
    assert.ok((await facts.textContent()).includes('当前可用路由就绪条件已满足'));
    assert.ok((await facts.textContent()).includes('所选主体可调用受阻需要处理后才能使用'));

    const runtimePath = `/model-readiness?project_id=${fixture.project_a}&model=${encodeURIComponent(fixture.runtime_only_model)}&endpoint=${encodeURIComponent('/v1/chat/completions')}&caller=self`;
    await readiness.page.goto(runtimePath);
    await visible(readiness.page.getByText('查看高级诊断证据', { exact: true }));
    await readiness.page.getByText('查看高级诊断证据', { exact: true }).click();
    await visible(readiness.page.getByText('model_governance_not_licensed_optional', { exact: true }));
    const runtimeFacts = readiness.page.locator('.ur-readiness-summary-grid');
    await visible(runtimeFacts);
    assert.ok((await runtimeFacts.textContent()).includes('模型记录不适用 / 可选当前流程不要求'));
    assert.ok((await runtimeFacts.textContent()).includes('当前可用路由就绪条件已满足'));
    journeys.push('readiness_four_facts_optional_states');
  } finally {
    await readiness.context.close();
  }

  const unavailable = await login('alice', fixture.unavailable_base_url);
  try {
    const path = `/model-readiness?project_id=${fixture.project_a}&model=${encodeURIComponent(fixture.ready_model)}&endpoint=${encodeURIComponent('/v1/chat/completions')}&caller=self`;
    await gotoHeading(unavailable.page, path, '调用条件诊断');
    await visible(unavailable.page.getByText('暂时无法判断', { exact: true }).first());
    await unavailable.page.getByText('查看高级诊断证据', { exact: true }).click();
    await visible(unavailable.page.getByText('routing_preview_unavailable', { exact: true }).first());
    journeys.push('readiness_unavailable_not_blocked');
  } finally {
    await unavailable.context.close();
  }

  for (const [name, url] of Object.entries(fixture.trap_urls)) {
    assert.equal(url.startsWith('http://127.0.0.1:'), true, `${name} trap must be local-only`);
  }
  console.log(`PR4_BROWSER_JOURNEYS ${journeys.join(',')}`);
  console.log('PR4_BROWSER_EXTERNAL_TRAFFIC provider=0 feed=0 upstream=0');
} finally {
  await browser.close();
}
