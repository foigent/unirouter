import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Each test starts without registered domain catalogs. Importing every catalog
// up front would conceal missing dependencies on a direct page visit.
describe('page-scoped UI catalogs', () => {
  beforeEach(() => { vi.resetModules(); localStorage.clear(); });
  afterEach(() => { localStorage.clear(); });

  it('registers idempotently and rejects a conflicting batch atomically', async () => {
    const { registerMessages, englishMessage } = await import('./registry');
    registerMessages({ stable: 'Original' });
    expect(() => registerMessages({ stable: 'Original' })).not.toThrow();
    expect(() => registerMessages({ untouched: 'New', stable: 'Conflict' })).toThrow('Conflicting UI translation');
    expect(englishMessage('stable')).toBe('Original');
    expect(englishMessage('untouched')).toBeUndefined();
    expect(englishMessage('constructor')).toBeUndefined();
  });

  it.each([
    ['monitoring', () => import('../pages/MediaJobsPage'), '异步模型任务'],
    ['access', () => import('../pages/ApplicationsV2Page'), '应用与 Agent'],
    ['people', () => import('../pages/ProjectManagementPage'), '项目名称'],
    ['security', () => import('../pages/AgentTrustPage'), '信任证据检查器'],
    ['audit filters', () => import('../pages/AuditLogsPage'), '高级筛选'],
    ['finance', () => import('../pages/ModelBillingPage'), '新建 Rate Card'],
    ['integrations', () => import('../pages/DataExchangePage'), '正文保留期限'],
    ['developer', () => import('../pages/ApiDocsPage'), 'API 接口目录'],
    ['developer errors', () => import('../pages/DeveloperConsolePage'), '创建失败{v0}'],
  ] as const)('loads %s translations on a direct visit without prior pages', async (_domain, page, key) => {
    localStorage.setItem('unirouter.ui.locale', 'en-US');
    const { translateLabel } = await import('./index');
    const { englishMessage } = await import('./registry');
    await page();
    expect(englishMessage(key)).toBeTruthy();
    expect(translateLabel(key)).not.toBe(key);
    expect(translateLabel('客户提供的模型名称')).toBe('客户提供的模型名称');
  });

  it.each(['models-first', 'logs-first'] as const)('supports English direct visits and stable labels: %s', async order => {
    localStorage.setItem('unirouter.ui.locale', 'en-US');
    const { translate } = await import('./index');
    const { englishMessage } = await import('./registry');
    expect(englishMessage('模型执行目标')).toBeUndefined();
    expect(englishMessage('请求 / 时间')).toBeUndefined();
    // Shared labels must work before either page has been visited.
    expect(translate('上游回传')).toBe('Upstream reported');
    expect(translate('成功 / 失败')).toBe('Succeeded / failed');
    const modelPage = () => import('../pages/ModelEndpointsPage');
    const logPage = () => import('../pages/LogsPage');
    if (order === 'models-first') { await modelPage(); await logPage(); }
    else { await logPage(); await modelPage(); }
    expect(translate('模型执行目标')).toBe('Execution targets');
    expect(translate('请求 / 时间')).toBe('Request / time');
    expect(translate('成功 / 失败')).toBe('Succeeded / failed');
    expect(translate('更新时间')).toBe('Updated');
    expect(translate('读取失败')).toBe('Unavailable');
  });
});
