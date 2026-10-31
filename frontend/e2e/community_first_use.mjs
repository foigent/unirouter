import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { setTimeout as wait } from 'node:timers/promises';
import { chromium } from 'playwright';

const f = JSON.parse(process.env.COMMUNITY_FIRST_USE_FIXTURE || '{}');
assert.ok(f.base_url?.startsWith('http://127.0.0.1:'));
const evidence = process.env.FIRST_USE_EVIDENCE_DIR;
if (evidence) await mkdir(evidence, { recursive: true });
const browser = await chromium.launch({ headless: true });
const result = { journeys: [], requests: [], external_requests: [], page_errors: [], mutations: [] };
let current;
const step = (name) => { result.journeys.push(name); console.log(`FIRST_USE_STEP ${name}`); };
const visible = async (locator) => { await locator.waitFor({ state: 'visible', timeout: 15000 }); return locator; };
const modal = (page, text) => page.locator('.semi-modal:visible').filter({ hasText: text }).last();
const sheet = (page) => page.locator('.semi-sidesheet:visible').last();
const responseFor = (page, method, path) => page.waitForResponse(r => r.request().method() === method && new URL(r.url()).pathname === path);
const api = async (page, method, path, body) => page.evaluate(async ({ method, path, body }) => {
  const response = await fetch(path, { method, headers: { Authorization: `Bearer ${localStorage.getItem('token')}`, 'Content-Type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  return { status: response.status, data: await response.json() };
}, { method, path, body });
const select = async (page, root, label, option) => {
  const field = root.locator('.semi-form-field').filter({ hasText: label }).first();
  await field.locator('.semi-select').click();
  await page.locator('.semi-select-option:visible').filter({ hasText: option }).last().getByText(option,{exact:false}).last().click();
};
async function waitForRequestLog(page, requestID) {
  const deadline=Date.now()+15000;
  let response;
  while(Date.now()<deadline){
    response=await api(page,'GET',`/api/v1/logs?project_id=${f.project_a}&request_id=${requestID}&page=1&page_size=20`);
    assert.equal(response.status,200);
    if(response.data.data?.some(row=>row.request_id===requestID))return response.data.data.find(row=>row.request_id===requestID);
    await wait(100);
  }
  result.missing_log={request_id:requestID,response};assert.fail(`real async log missing for ${requestID}`);
}
async function login(username) {
  const context = await browser.newContext({ baseURL: f.base_url, serviceWorkers: 'block', viewport: { width: 1440, height: 1000 } });
  await context.route('**/*', async route => {
    const u = new URL(route.request().url());
    if (u.origin === f.base_url || ['data:', 'blob:', 'about:'].includes(u.protocol)) return route.continue();
    result.external_requests.push(`${u.protocol}//${u.host}${u.pathname}`); return route.abort('blockedbyclient');
  });
  const page = await context.newPage(); current = page;
  page.on('framenavigated', frame => { if(frame === page.mainFrame()) current = page; });
  page.on('pageerror', error => result.page_errors.push(error.message));
  page.on('response', response => {if(['POST','PUT','DELETE'].includes(response.request().method())) result.mutations.push({method:response.request().method(),path:new URL(response.url()).pathname,status:response.status()});});
  await page.goto('/login');
  assert.equal(await page.locator('html').getAttribute('lang'), 'en-US', 'English is the first-use default');
  await page.getByRole('button', { name: '中文', exact: true }).click();
  await page.getByLabel('用户名').fill(username);
  await page.locator('input[type=password]').fill(f.password);
  const response = responseFor(page, 'POST', '/api/v1/auth/login');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  assert.equal((await response).status(), 200);
  await page.waitForURL(u => u.pathname !== '/login');
  const dimensions = await page.evaluate(() => [innerWidth, innerHeight]);
  assert.deepEqual(dimensions, [1440, 1000]);
  return page;
}
async function fillNewMember(page, username) {
  await page.locator('button:visible').filter({ hasText: '新增本地用户' }).first().click();
  const form = await visible(sheet(page));
  await select(page, form, '创建后加入项目', 'Community Alpha');
  await select(page, form, '组织角色', '普通成员');
  await form.getByLabel('用户名', { exact: true }).fill(username);
  await form.locator('input[type=password]').fill(f.password);
  return form;
}
async function ratePage(page) {
  await page.goto('/model-prices?tab=enterprise');
  await visible(page.getByRole('heading', { name: '费率与计价', exact: true }));
  await visible(page.getByText('当前已启用 Rate Card 严格模式', { exact: false }));
}
async function rateTransition(page, action, title, confirm, expectedStatus = 200) {
  const wait = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(`/rate-cards/${result.rate_card_ref}/${action}`) && r.request().method() === 'POST');
  await visible(modal(page, title));
  await modal(page, title).locator('button:visible').filter({ hasText: confirm }).last().click();
  const response = await wait;
  assert.equal(response.status(), expectedStatus, `${action}: ${await response.text()}`);
  return response.json();
}
try {
  const admin = await login(f.usernames.org_admin);
  await admin.goto('/organization-members?section=people');
  await visible(admin.locator('button:visible').filter({ hasText: '新增本地用户' }).first());
  let form = await fillNewMember(admin, 'community-cancelled');
  let createRequests = 0;
  const countCreate = request => { if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/users') createRequests++; };
  admin.on('request', countCreate);
  await form.getByRole('button', { name: '取消', exact: true }).click();
  await form.waitFor({ state: 'hidden' }); assert.equal(createRequests, 0);
  admin.off('request', countCreate);
  step('member_create_cancel_no_request');
  form = await fillNewMember(admin, 'community-new-member');
  const create = responseFor(admin, 'POST', '/api/v1/users');
  const membership = admin.waitForResponse(r => /\/projects\/\d+\/members\/\d+$/.test(new URL(r.url()).pathname) && r.request().method() === 'PUT');
  const completedCreate = Promise.all([create, membership]); completedCreate.catch(() => {});
  await form.getByRole('button', { name: '创建', exact: true }).click();
  const [created, failedMembership] = await completedCreate; assert.equal(created.status(), 201); result.member_id = (await created.json()).id;
  assert.ok(failedMembership.status() >= 400);
  await visible(admin.getByText('用户已创建，但项目访问未授予', { exact: false }));
  assert.equal(await admin.getByText('用户已创建并获得项目访问', { exact: false }).count(), 0);
  result.membership_failure_status = failedMembership.status();
  step('member_created_membership_failure_honest_warning');
  const row = await visible(admin.locator('tr').filter({ hasText: 'community-new-member' }));
  await row.getByRole('button', { name: '授予项目访问 community-new-member', exact: true }).click();
  form = await visible(sheet(admin));
  await select(admin, form, '项目', 'Community Alpha');
  await select(admin, form, '项目角色', '项目使用者（可创建凭证并调用）');
  await admin.waitForFunction(() => ![...document.querySelectorAll('.semi-sidesheet button')].some(b => b.textContent === '授予项目访问' && b.classList.contains('semi-button-loading')));
  const grant = responseFor(admin, 'PUT', `/api/v1/projects/${f.project_a}/members/${result.member_id}`);
  await form.getByRole('button', { name: '授予项目访问', exact: true }).click(); assert.equal((await grant).status(), 200);
  step('new_member_project_access_retry_persisted');

  const member = await login('community-new-member');
  const projects = await api(member, 'GET', '/api/v1/projects?page=1&page_size=100&status=active');
  assert.equal(projects.status, 200); assert.deepEqual(projects.data.data.map(p => p.id), [f.project_a]);
  assert.equal((await api(member, 'GET', `/api/v1/projects/${f.project_b}`)).status, 403);
  assert.equal((await api(member, 'PUT', `/api/v1/projects/${f.project_a}/budget`, { budget_cents: 1, budget_period: 'monthly' })).status, 404);
  assert.equal((await api(member, 'POST', '/api/v1/enterprise-finance/rate-cards', {})).status, 403);
  step('new_member_login_project_isolation_and_finance_denial');
  await member.goto(`/access?project_id=${f.project_a}`);
  await member.locator('button').filter({ hasText: '提交访问请求' }).first().click();
  const requestDialog = await visible(modal(member, '提交访问请求'));
  const personal = member.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/access-requests' && r.request().method() === 'POST');
  await requestDialog.locator('button:visible').filter({ hasText: '提交' }).last().click();
  const personalResponse = await personal; assert.equal(personalResponse.status(), 201);
  const access = await personalResponse.json(); assert.equal(access.request.status, 'approved'); assert.equal(access.grant.status, 'active'); result.grant_ref = access.grant.grant_ref;
  await member.getByRole('button', { name: '管理 API Key', exact: true }).first().click();
  await member.locator('button').filter({ hasText: '创建 API Key' }).first().click();
  const keyDialog = await visible(modal(member, '创建 API Key'));
  await keyDialog.getByLabel('Key 名称').fill('community-first-use-member');
  const issued = member.waitForResponse(r => /\/access-grants\/[^/]+\/credentials$/.test(new URL(r.url()).pathname) && r.request().method() === 'POST');
  await keyDialog.locator('button:visible').filter({ hasText: '创建' }).last().click();
  const issueResponse = await issued; assert.ok([200, 201].includes(issueResponse.status()));
  const credential = await issueResponse.json(); assert.equal(credential.secret_available, true); const key = credential.key;
  result.credential_ref = credential.credential_ref;
  await visible(member.getByText(key, { exact: true }));
  await modal(member, '一次性 API Key').locator('button:visible').filter({ hasText: '我已安全保存' }).last().click();
  assert.equal(await member.getByText(key, { exact: true }).count(), 0);
  assert.equal(await member.evaluate(secret => JSON.stringify(localStorage).includes(secret), key), false);
  step('new_member_normal_personal_grant_and_one_time_credential');

  const maker = await login(f.usernames.maker); await ratePage(maker);
  await maker.getByRole('button', { name: '新建 Rate Card', exact: true }).click();
  let rateModal = await visible(modal(maker, '新建企业 Rate Card'));
  await rateModal.locator('button:visible').filter({ hasText: '取消' }).last().click();
  assert.equal((await api(maker, 'GET', `/api/v1/enterprise-finance/rate-cards?org_id=${f.org_id}`)).data.data.length, 0);
  await maker.getByRole('button', { name: '新建 Rate Card', exact: true }).click();
  rateModal = await visible(modal(maker, '新建企业 Rate Card'));
  await rateModal.getByLabel('Rate Card 供应商', { exact: true }).fill('deepseek');
  await rateModal.getByLabel('Rate Card 模型', { exact: true }).fill(f.ready_model);
  for (const [label, value] of [['输入 Token','1000'],['输出 Token','2000'],['缓存读取 Token','1000'],['缓存创建 / 写入 Token','1000']]) await rateModal.getByLabel(`Rate Card ${label}每百万价格`, { exact: true }).fill(value);
  await rateModal.getByLabel('Rate Card 生效日期').fill('2026-01-01');
  const draftPromise = responseFor(maker, 'POST', '/api/v1/enterprise-finance/rate-cards');
  await rateModal.locator('.semi-modal-footer button').last().click();
  const draftResponse = await draftPromise; assert.equal(draftResponse.status(), 201);
  const draft = await draftResponse.json(); result.rate_card_ref = draft.card.rate_card_ref;
  step('rate_card_ui_draft_cancel_and_persist');
  await maker.locator('tr').filter({ hasText: result.rate_card_ref }).getByRole('button', { name: '复核', exact: true }).click();
  const sameActor = maker.waitForResponse(r => r.url().includes(`/rate-cards/${result.rate_card_ref}/approve`) && r.request().method() === 'POST');
  await modal(maker, '复核 Rate Card 完整证据').locator('button:visible').filter({ hasText: '确认批准此版本' }).last().click();
  assert.equal((await sameActor).status(), 409);
  step('rate_card_maker_checker_refusal');
  const checker = await login(f.usernames.checker); await ratePage(checker);
  await checker.locator('tr').filter({ hasText: result.rate_card_ref }).getByRole('button', { name: '复核', exact: true }).click();
  await modal(checker, '复核 Rate Card 完整证据').locator('button:visible').filter({ hasText: '取消' }).last().click();
  const unapproved = await api(checker, 'GET', `/api/v1/enterprise-finance/rate-cards?org_id=${f.org_id}`);assert.equal(unapproved.data.data[0].card.state,'draft');
  await checker.locator('tr').filter({ hasText: result.rate_card_ref }).getByRole('button', { name: '复核', exact: true }).click();
  await rateTransition(checker,'approve','复核 Rate Card 完整证据','确认批准此版本');
  const stale = await api(checker, 'POST', `/api/v1/enterprise-finance/rate-cards/${result.rate_card_ref}/activate`, { org_id: f.org_id, expected_version: 1 }); assert.equal(stale.status,409);
  await checker.locator('tr').filter({ hasText: result.rate_card_ref }).getByRole('button', { name: '激活', exact: true }).click();
  await modal(checker, '确认激活 Rate Card').locator('button:visible').filter({ hasText: '取消' }).last().click();
  await checker.locator('tr').filter({ hasText: result.rate_card_ref }).getByRole('button', { name: '激活', exact: true }).click();
  await rateTransition(checker,'activate','确认激活 Rate Card','确认激活此版本');
  step('rate_card_independent_review_activation_cancel_conflict_and_authority');
  await maker.goto('/wallet?tab=budgets');
  await maker.locator('tr').filter({ hasText: 'Community Alpha' }).locator('button:visible').filter({hasText:'调整预算'}).click();
  form = await visible(sheet(maker));await form.getByLabel('预算金额（元）', { exact: true }).fill('0.01');await select(maker,form,'预算周期','每月');
  const budget = responseFor(maker,'PUT',`/api/v1/projects/${f.project_a}/budget`);await form.getByRole('button',{name:'保存预算',exact:true}).click();assert.equal((await budget).status(),200);
  step('project_budget_real_ui_save_alert_contract');

  await member.goto(`/developer?project_id=${f.project_a}#project-debug`);
  const debug = await visible(member.locator('#project-debug'));
  await member.waitForFunction(() => {const b=[...document.querySelectorAll('#project-debug button')].find(b=>b.textContent.includes('运行'));return b&&!b.disabled&&!b.classList.contains('semi-button-disabled');});
  await debug.getByPlaceholder('粘贴项目访问凭证').fill(key);
  await debug.getByPlaceholder('输入本次测试提示词').fill('Synthetic local first use only.');
  await debug.getByLabel('max_tokens（可选）').fill('64');
  const thinking = debug.getByRole('combobox',{name:'thinking.type（可选）',exact:true});
  await thinking.click();
  const optionsID = await thinking.getAttribute('aria-controls');assert.ok(optionsID);
  await member.getByText('disabled（关闭）',{exact:true}).last().click();
  await visible(thinking.getByText('disabled（关闭）',{exact:true}));
  console.log(`FIRST_USE_THINKING_CONTROL ${await thinking.innerText()}`);
  const preflight = await member.evaluate(async ({key,model}) => {
    const response=await fetch('/v1/chat/completions/preflight',{method:'POST',headers:{Authorization:`Bearer ${key}`,'Content-Type':'application/json'},body:JSON.stringify({model,stream:false,thinking:{type:'disabled'},max_tokens:64,messages:[{role:'user',content:'Synthetic local first use only.'}]})});return {status:response.status,body:await response.json()};
  },{key,model:f.ready_model}); result.preflight=preflight;
  assert.equal(preflight.status,200);assert.equal(preflight.body.locally_admissible,true); console.log(`FIRST_USE_PREFLIGHT ${JSON.stringify(preflight.body)}`);
  for (const stream of [false, true]) {
    if (stream) { await debug.locator('.semi-select').filter({ hasText: '非流式' }).click();await member.getByText('流式',{exact:true}).last().click();await visible(debug.locator('.semi-select').filter({hasText:'流式'}).filter({hasNotText:'非流式'}).first()); }
    const call = responseFor(member, 'POST','/v1/chat/completions');
    await debug.locator('button:visible').filter({hasText:'运行'}).first().click();const response=await call;
    const operation = response.headers()['x-request-id'];
    const sent=response.request().postDataJSON();result.last_request_shape={model:sent.model,stream:sent.stream,max_tokens:sent.max_tokens,thinking:sent.thinking};console.log(`FIRST_USE_REQUEST_SHAPE ${JSON.stringify(result.last_request_shape)}`);assert.deepEqual(sent.thinking,{type:'disabled'});assert.equal(sent.stream,stream);
    await waitForRequestLog(member,operation);
    assert.equal(response.status(),200,await response.text());
    assert.match(operation,/^op_/);await visible(debug.getByText(operation,{exact:true}));
    await visible(debug.getByText(stream?'community-stream-ok':'community-ok',{exact:true}));
    await visible(debug.getByText('15 tokens',{exact:true}));
    const text=await response.text();assert.ok(text.includes(operation));if(stream){assert.ok(text.includes('[DONE]'));assert.ok(text.includes('"billing_status":"settled"'));}
    result.requests.push({request_id:operation,stream,status:response.status()});
  }
  await debug.getByPlaceholder('粘贴项目访问凭证').fill('');
  step('canonical_gateway_success_and_stream_terminal_same_requests');
  // Reuse the same two requests; the native frozen quote/journal assertions
  // in Go bind the Rate Card and actual prices. The separate enterprise report
  // snapshot API is deliberately not a gateway settlement authority.
  for(const request of result.requests){
    const log=await waitForRequestLog(member,request.request_id);
    assert.equal(log.request_id,request.request_id);assert.equal(log.total_tokens,15);
    const detail=await api(member,'GET',`/api/v1/logs/${log.id}`);assert.equal(detail.status,200);
    request.log_id=log.id;request.log_detail=detail.data;
  }
  step('request_ids_reused_real_logs_and_native_pricing_receipts');

  await admin.goto('/limit-policies');await admin.locator('button:visible').filter({hasText:'新增策略'}).first().click();form=await visible(sheet(admin));
  await form.getByLabel('策略名称',{exact:true}).fill('community-first-use-hard-limit');await select(admin,form,'项目（可选）','Community Alpha');await form.getByLabel('TPM（每分钟 Token）',{exact:true}).fill('1');const modeField=form.locator('.semi-form-field').filter({hasText:'运行状态'}).first();await modeField.locator('.semi-select').click();await admin.getByText('正式拦截',{exact:true}).last().click();await visible(modeField.getByText('正式拦截',{exact:true}));
  const policyWait=responseFor(admin,'POST','/api/v1/limit-policies');await form.getByRole('button',{name:'创建策略',exact:true}).click();const policyResponse=await policyWait;assert.equal(policyResponse.status(),201);const sentPolicy=policyResponse.request().postDataJSON();assert.equal(sentPolicy.mode,'enforce');assert.equal(sentPolicy.project_id,f.project_a);assert.equal(sentPolicy.tpm_limit,1);const policy=await policyResponse.json();assert.equal(policy.runtime_snapshot_applied,true);assert.equal(policy.mode,'enforce');assert.equal(policy.project_id,f.project_a);assert.equal(policy.tpm_limit,1);result.limit_policy=policy;
  step('enforce_shared_limit_ui_save_runtime_reload');
  await debug.getByPlaceholder('粘贴项目访问凭证').fill(key);
  const refusal=responseFor(member,'POST','/v1/chat/completions');await debug.locator('button:visible').filter({hasText:'运行'}).first().click();const rejected=await refusal;assert.equal(rejected.status(),429);const rejection=await rejected.json();const rejectedID=rejected.headers()['x-request-id'];assert.match(rejectedID,/^op_/);assert.equal(rejection.error?.code,'limit_policy.tpm_exceeded');result.requests.push({request_id:rejectedID,stream:true,status:429,error:rejection.error?.code});
  await visible(debug.getByText('HTTP 429',{exact:true}));
  await visible(debug.getByText(rejectedID,{exact:true}));
  await debug.getByPlaceholder('粘贴项目访问凭证').fill('');
  step('explicit_refusal_request_id_visible_no_provider_dispatch');
  if(evidence)await debug.screenshot({path:join(evidence,'request-refusal.png')});
  assert.deepEqual(result.external_requests,[]);assert.deepEqual(result.page_errors,[]);
  result.status='passed';
} catch(error) {
  result.status='failed';result.error=String(error.stack||error).replace(/ur-\d{4}-[0-9a-f]{32}/g,'[synthetic credential redacted]');
  if(current){result.page_url=current.url();result.buttons=await current.locator('button:visible').evaluateAll(nodes=>nodes.map(n=>({text:n.innerText,aria:n.getAttribute('aria-label')})));result.page_text=(await current.locator('body').innerText()).replace(/ur-\d{4}-[0-9a-f]{32}/g,'[synthetic credential redacted]').slice(-7000);}
  console.error(result.error);process.exitCode=1;
} finally {
  if(evidence)await writeFile(join(evidence,'browser-result.json'),JSON.stringify(result,null,2)+'\n');
  console.log(`COMMUNITY_FIRST_USE_RESULT ${JSON.stringify({status:result.status,journeys:result.journeys,request_ids:result.requests.map(r=>r.request_id),external:result.external_requests.length})}`);
  await browser.close();
}
