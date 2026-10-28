import '../i18n/security';
import { translate as t, useI18n, translateLabel, localizeOptions } from '../i18n';
import { formatDateTime } from '../utils/formatDate';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, DatePicker, Input, Select, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { auditSourceLabel, auditSourceOptions } from '../utils/displayLabels';
import { useAuthStore } from '../store/auth';
import { customerOrganizationDisplayName } from '../utils/customerContext';
import { projectDisplayName } from '../utils/projectDisplay';
import {
  deleteLocalView,
  loadSavedViews,
  readPositiveQueryParam,
  replaceControlledQuery,
  saveLocalView,
  type SavedView,
} from '../utils/viewState';

const { Text } = Typography;

type TagColor = 'amber' | 'blue' | 'cyan' | 'green' | 'grey' | 'indigo' | 'light-blue' | 'light-green' | 'lime' | 'orange' | 'pink' | 'purple' | 'red' | 'teal' | 'violet' | 'yellow' | 'white';
const ACTION_COLORS: Record<string, TagColor> = {
  create: 'green', update: 'blue', delete: 'red',
  login: 'purple', topup: 'cyan', grant: 'green',
  budget_alert: 'orange', freeze: 'yellow', settle: 'blue',
  change_password: 'orange',
};

function actionColor(action: string): TagColor {
  const suffix = action?.split('.')?.pop() || '';
  return ACTION_COLORS[action] || ACTION_COLORS[suffix] || 'grey';
}

interface AuditFilters {
  project_id?: string;
  actor_id?: string;
  actor_kind?: string;
  actor_ref?: string;
  action?: string;
  resource?: string;
  resource_id?: string;
  request_id?: string;
  client_request_id?: string;
  source?: string;
  start_time?: string;
  end_time?: string;
}

interface AuditLog {
  id: number;
  org_id?: number;
  org_name?: string;
  project_id?: number | null;
  project_name?: string;
  actor_id?: number;
  actor_name?: string;
  actor_kind?: 'human' | 'service' | 'system';
  actor_ref?: string;
  actor?: { kind: 'human' | 'service' | 'system'; ref: string; display_name: string };
  user_id?: number | null;
  action: string;
  resource?: string;
  resource_id?: string;
  request_id?: string;
  client_request_id?: string;
  source?: string;
  ip?: string;
  user_agent?: string;
  detail?: unknown;
  before?: unknown;
  after?: unknown;
  created_at: string;
}

const auditFilterKeys = [
  'project_id',
  'actor_id',
  'actor_kind',
  'actor_ref',
  'action',
  'resource',
  'resource_id',
  'request_id',
  'client_request_id',
  'source',
  'start_time',
  'end_time',
] as const satisfies readonly (keyof AuditFilters)[];

const auditQueryKeys = [
  ...auditFilterKeys,
  'page',
  'page_size',
  'audit_id',
] as const;

const auditSavedViewStorageKey = 'unirouter.audit.saved-views.v1';

function normalizeAuditFilters(value: unknown): AuditFilters | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const source = value as Record<string, unknown>;
  const normalized: AuditFilters = {};
  auditFilterKeys.forEach((key) => {
    const candidate = source[key];
    if (typeof candidate === 'string' && candidate.trim()) {
      normalized[key] = candidate.trim();
    }
  });
  return normalized;
}

function readInitialAuditState() {
  const params = new URLSearchParams(window.location.search);
  const rawFilters: Record<string, string> = {};
  auditFilterKeys.forEach((key) => {
    const value = params.get(key);
    if (value) rawFilters[key] = value;
  });
  const filters = normalizeAuditFilters(rawFilters) || {};
  const pageSize = readPositiveQueryParam(params, 'page_size');
  return {
    filters,
    page: readPositiveQueryParam(params, 'page') || 1,
    pageSize: pageSize && [20, 50, 100].includes(pageSize) ? pageSize : 20,
    auditID: readPositiveQueryParam(params, 'audit_id'),
  };
}

function syncAuditURL(filters: AuditFilters, page: number, pageSize: number, auditID?: number) {
  replaceControlledQuery(auditQueryKeys, {
    ...filters,
    page: page === 1 ? undefined : page,
    page_size: pageSize === 20 ? undefined : pageSize,
    audit_id: auditID,
  });
}

function formattedJSON(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

function dateValueToISO(value: Date | string | number | null | undefined) {
  if (value === undefined || value === null || value === '') return undefined;
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

function scopedIdentity(name: string | undefined, id: number | null | undefined, fallback: string) {
  if (!id) return '-';
  return name ? `${name} (#${id})` : `${fallback} #${id}`;
}

function actorIdentity(row: any) {
  if (row.actor?.kind === 'service') return `${row.actor.display_name || 'X-Insight service'} (${row.actor.ref})`;
  if (row.actor?.kind === 'system') return row.actor.display_name || t("系统");
  if (row.actor?.kind === 'human') return scopedIdentity(row.actor.display_name, row.user_id, t("用户"));
  if (row.actor_id === 0 && row.source === 'system') return t("系统");
  return scopedIdentity(row.actor_name, row.actor_id, t("用户"));
}

function projectIdentity(name: string | undefined, id: number | null | undefined) {
  if (!id) return '-';
  const displayName = projectDisplayName({ id, name, slug: name === 'Default Project' ? 'default' : undefined });
  return `${displayName} (#${id})`;
}

export default function AuditLogsPage() {
  const { locale } = useI18n();
  const user = useAuthStore((state) => state.user);
  const initialState = useRef(readInitialAuditState()).current;
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(initialState.page);
  const [pageSize, setPageSize] = useState(initialState.pageSize);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [draftFilters, setDraftFilters] = useState<AuditFilters>(initialState.filters);
  const [filters, setFilters] = useState<AuditFilters>(initialState.filters);
  const [selectedLogID, setSelectedLogID] = useState<number | undefined>(initialState.auditID);
  const selectedLogIDRef = useRef<number | undefined>(initialState.auditID);
  const [reloadVersion, setReloadVersion] = useState(0);
  const [savedViews, setSavedViews] = useState<SavedView<AuditFilters>[]>(() => (
    loadSavedViews(auditSavedViewStorageKey, normalizeAuditFilters)
  ));
  const [savedViewName, setSavedViewName] = useState('');
  const [selectedSavedView, setSelectedSavedView] = useState<string>();
  const requestVersion = useRef(0);

  useEffect(() => {
    const version = ++requestVersion.current;
    const desiredLogID = selectedLogIDRef.current;
    setLoading(true);
    setLoadError('');
    setLogs([]);
    setTotal(0);
    setSelectedLogID(undefined);
    api.getAuditLogs(page, pageSize, filters)
      .then((d: any) => {
        if (version !== requestVersion.current) return;
        const nextLogs = (d.data || []) as AuditLog[];
        const preferred = nextLogs.find(log => log.id === desiredLogID) || nextLogs[0];
        setLogs(nextLogs);
        setTotal(d.total || 0);
        selectedLogIDRef.current = preferred?.id;
        setSelectedLogID(preferred?.id);
        syncAuditURL(filters, page, pageSize, preferred?.id);
      })
      .catch(() => {
        if (version !== requestVersion.current) return;
        selectedLogIDRef.current = undefined;
        setSelectedLogID(undefined);
        setLoadError('无法加载当前筛选条件下的审计记录。');
        syncAuditURL(filters, page, pageSize);
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
    return () => { requestVersion.current += 1; };
  }, [filters, page, pageSize, reloadVersion]);

  const updateFilter = (key: keyof AuditFilters, value: string | undefined) => {
    setDraftFilters(prev => ({ ...prev, [key]: value || undefined }));
  };

  const commitFilters = (nextFilters: AuditFilters) => {
    const normalized = normalizeAuditFilters(nextFilters) || {};
    selectedLogIDRef.current = undefined;
    setSelectedLogID(undefined);
    setFilters(normalized);
    setDraftFilters(normalized);
    setPage(1);
    syncAuditURL(normalized, 1, pageSize);
  };

  const clearFilters = () => {
    setSelectedSavedView(undefined);
    commitFilters({});
  };

  const selectLog = (log: AuditLog) => {
    selectedLogIDRef.current = log.id;
    setSelectedLogID(log.id);
    syncAuditURL(filters, page, pageSize, log.id);
  };

  const changePage = (nextPage: number) => {
    selectedLogIDRef.current = undefined;
    setSelectedLogID(undefined);
    setPage(nextPage);
    syncAuditURL(filters, nextPage, pageSize);
  };

  const changePageSize = (nextPageSize: number) => {
    selectedLogIDRef.current = undefined;
    setSelectedLogID(undefined);
    setPageSize(nextPageSize);
    setPage(1);
    syncAuditURL(filters, 1, nextPageSize);
  };

  const setRecentRange = (milliseconds: number) => {
    const end = new Date();
    const start = new Date(end.getTime() - milliseconds);
    setDraftFilters(prev => ({
      ...prev,
      start_time: start.toISOString(),
      end_time: end.toISOString(),
    }));
  };

  const saveCurrentView = () => {
    const next = saveLocalView(
      auditSavedViewStorageKey,
      savedViews,
      savedViewName,
      normalizeAuditFilters(draftFilters) || {},
    );
    if (!next.saved) {
      Toast.error(t("视图名称需为 1 至 40 个字符，并且浏览器存储必须可用。"));
      return;
    }
    const name = savedViewName.trim();
    setSavedViews(next.views);
    setSelectedSavedView(name);
    setSavedViewName(name);
    Toast.success(t("筛选视图已保存在此浏览器"));
  };

  const applySavedView = (name: string) => {
    const view = savedViews.find(item => item.name === name);
    if (!view) return;
    setSelectedSavedView(name);
    setSavedViewName(name);
    commitFilters(view.state);
  };

  const deleteSavedView = () => {
    if (!selectedSavedView) return;
    const next = deleteLocalView(auditSavedViewStorageKey, savedViews, selectedSavedView);
    if (!next.deleted) {
      Toast.error(t("无法删除此浏览器中的视图"));
      return;
    }
    setSavedViews(next.views);
    setSelectedSavedView(undefined);
    setSavedViewName('');
    Toast.success(t("已删除此浏览器中的筛选视图"));
  };

  const selectedLog = useMemo(
    () => logs.find(log => log.id === selectedLogID) || null,
    [logs, selectedLogID, locale],
  );
  const appliedFilterCount = Object.values(filters).filter(Boolean).length;
  const organizationIdentity = (row: AuditLog) => scopedIdentity(
    customerOrganizationDisplayName(user, row.org_id, row.org_name, t("当前客户")),
    row.org_id,
    t("组织"),
  );

  const columns = [
    { title: t('动作'), key: 'event', width: 200, render: (_: unknown, row: AuditLog) => <div className="ur-event-copy"><Tag color={actionColor(row.action)}><span title={row.action}>{row.action}</span></Tag><small>{formatDateTime(row.created_at)}</small></div> },
    { title: t('范围'), key: 'scope', width: 180, render: (_: unknown, row: AuditLog) => <div className="ur-event-copy"><span>{organizationIdentity(row)}</span>{row.project_id ? <small>{projectIdentity(row.project_name, row.project_id)}</small> : null}</div> },
    { title: t('操作者'), key: 'actor', width: 170, render: (_: unknown, row: AuditLog) => <div className="ur-event-copy"><span>{actorIdentity(row)}</span><small>{row.source ? auditSourceLabel(row.source) : '—'}</small></div> },
    { title: t('目标'), key: 'target', width: 160, render: (_: unknown, row: AuditLog) => <div className="ur-event-copy"><span>{row.resource || '—'}</span>{row.resource_id ? <small>#{row.resource_id}</small> : null}</div> },
  ];

  return (
    <div className="ur-page ur-security-workspace">
      <PageHeader
        eyebrow={t("访问与安全")}
        title={t("审计日志")}
        description={t("查看登录、配置变更、入账、额度调整和结算等关键后台动作。")}
      />
      <Surface title={t("筛选与保存视图")} description={t("先编辑筛选草稿，再应用查询；命名视图只保存在当前浏览器，不上传审计内容。")}>
        <div className="ur-audit-filter-grid">
          <Input aria-label={t("项目 ID 筛选")} prefix={t("项目")} placeholder={t("项目 ID")} value={draftFilters.project_id || ''} onChange={(v) => updateFilter('project_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label={t("操作者 ID 筛选")} prefix={t("操作者")} placeholder={t("操作者 ID")} value={draftFilters.actor_id || ''} onChange={(v) => updateFilter('actor_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label={t("审计动作筛选")} placeholder={t("动作")} value={draftFilters.action || ''} onChange={(v) => updateFilter('action', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Select aria-labelledby="audit-source-filter-label" insetLabel={t("来源")} insetLabelId="audit-source-filter-label" placeholder={t("选择来源")} value={draftFilters.source || undefined} onChange={(v) => updateFilter('source', v as string)} style={{ width: '100%' }} showClear>
            {localizeOptions(auditSourceOptions).map(option => <Select.Option key={option.value} value={option.value}>{option.label}</Select.Option>)}
          </Select>
          <DatePicker
            aria-labelledby="audit-time-range-label"
            insetLabel={<span id="audit-time-range-label">{t("时间范围")}</span>}
            type="dateTimeRange"
            format="yyyy-MM-dd HH:mm:ss"
            placeholder={[t("开始时间"), t("结束时间")]}
            showClear
            className="ur-audit-date-range"
            dropdownClassName="ur-audit-calendar"
            style={{ width: '100%' }}
            value={draftFilters.start_time && draftFilters.end_time ? [new Date(draftFilters.start_time), new Date(draftFilters.end_time)] : undefined}
            onChange={(value) => {
              const range = Array.isArray(value) ? value : [];
              setDraftFilters(prev => ({
                ...prev,
                start_time: dateValueToISO(range[0] as Date | string | number | null | undefined),
                end_time: dateValueToISO(range[1] as Date | string | number | null | undefined),
              }));
            }}
          />
        </div>
        <details className="ur-advanced-filters">
          <summary>{t('高级筛选')}</summary>
          <div className="ur-audit-filter-grid">
          <Select aria-label={t("操作者类型筛选")} placeholder={t("操作者类型")} value={draftFilters.actor_kind || undefined} onChange={(v) => updateFilter('actor_kind', v as string)} style={{ width: '100%' }} showClear>
            <Select.Option value="human">{t("UR 用户")}</Select.Option><Select.Option value="service">{t("服务")}</Select.Option><Select.Option value="system">{t("系统")}</Select.Option>
          </Select>
          <Input aria-label={t("操作者引用筛选")} placeholder={t("操作者引用")} value={draftFilters.actor_ref || ''} onChange={(v) => updateFilter('actor_ref', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label={t("目标类型筛选")} placeholder={t("目标类型")} value={draftFilters.resource || ''} onChange={(v) => updateFilter('resource', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label={t("目标 ID 筛选")} placeholder={t("目标 ID")} value={draftFilters.resource_id || ''} onChange={(v) => updateFilter('resource_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label={t("操作 ID 筛选")} placeholder={t("操作 ID（Request ID）")} value={draftFilters.request_id || ''} onChange={(v) => updateFilter('request_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label={t("客户端请求 ID 筛选")} placeholder={t("客户端请求 ID")} value={draftFilters.client_request_id || ''} onChange={(v) => updateFilter('client_request_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          </div>
        </details>
        <div className="ur-audit-filter-actions">
          <Space wrap>
            <Button size="small" onClick={() => setRecentRange(15 * 60 * 1000)}>{t("最近 15 分钟")}</Button>
            <Button size="small" onClick={() => setRecentRange(60 * 60 * 1000)}>{t("最近 1 小时")}</Button>
            <Button size="small" onClick={() => setRecentRange(24 * 60 * 60 * 1000)}>{t("最近 24 小时")}</Button>
            <Button size="small" onClick={() => setRecentRange(7 * 24 * 60 * 60 * 1000)}>{t("最近 7 天")}</Button>
          </Space>
          <Space>
            <Tag color={appliedFilterCount ? 'blue' : 'grey'}>{appliedFilterCount ? t('已应用 {count} 项', {count: appliedFilterCount}) : t("未筛选")}</Tag>
            <Button aria-label={t("应用筛选")} type="primary" icon={<IconSearch />} onClick={() => commitFilters(draftFilters)}>{t("应用筛选")}</Button>
            <Button aria-label={t("重置筛选")} icon={<IconRefresh />} onClick={clearFilters}>{t("重置")}</Button>
          </Space>
        </div>
        <div className="ur-saved-view-bar">
          <div role="group" aria-label={t("已保存的审计视图")}>
            <Select
              aria-labelledby="audit-saved-view-label"
              insetLabel={t("视图")}
              insetLabelId="audit-saved-view-label"
              placeholder={t("已保存视图（此浏览器）")}
              value={selectedSavedView}
              optionList={savedViews.map(view => ({ label: view.name, value: view.name }))}
              onChange={value => applySavedView(String(value))}
              showClear
              onClear={() => setSelectedSavedView(undefined)}
              className="ur-saved-view-select"
            />
          </div>
          <Input
            aria-label={t("审计视图名称")}
            placeholder={t("视图名称")}
            value={savedViewName}
            maxLength={40}
            onChange={setSavedViewName}
            onEnterPress={saveCurrentView}
          />
          <Button onClick={saveCurrentView}>{t("保存 / 覆盖")}</Button>
          <Button disabled={!selectedSavedView} onClick={deleteSavedView}>{t("删除")}</Button>
        </div>
      </Surface>

      <div className="ur-context-explorer ur-audit-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("审计记录")}</Typography.Title>
              <Text className="ur-panel-hint">{t("默认选择首条记录 · 详情始终在右侧")}</Text>
            </div>
            <Space>
              <Tag size="small">{loading && logs.length === 0 ? t("读取中") : t('{count} 条事件', {count: total})}</Tag>
              <Button
                size="small"
                icon={<IconRefresh />}
                loading={loading}
                onClick={() => setReloadVersion(version => version + 1)}
              >

                {t("刷新")}
              </Button>
            </Space>
          </div>
          <div className="ur-table-shell">
            {loading && logs.length === 0 ? (
              <EmptyState title={t("正在加载审计记录")} description={t("正在读取当前筛选范围的审计事实，请稍候。")} minHeight={220} />
            ) : <Table
              columns={columns}
              dataSource={logs}
              loading={loading}
              rowKey="id"
              scroll={{ x: 710, y: 620 }}
              onRow={(record) => ({
                onClick: () => record && selectLog(record as AuditLog),
                onKeyDown: (event) => {
                  if (record && (event.key === 'Enter' || event.key === ' ')) {
                    event.preventDefault();
                    selectLog(record as AuditLog);
                  }
                },
                tabIndex: 0,
                'aria-selected': selectedLogID === (record as AuditLog)?.id,
                style: {
                  cursor: 'pointer',
                  background: selectedLogID === (record as AuditLog)?.id ? 'var(--ur-bg-subtle)' : undefined,
                },
              })}
              empty={loadError ? (
                <EmptyState
                  title={t("审计记录加载失败")}
                  description={translateLabel(loadError)}
                  actions={<Button type="primary" onClick={() => setReloadVersion(version => version + 1)}>{t("重新加载审计记录")}</Button>}
                  minHeight={220}
                />
              ) : <EmptyState title={t("暂无审计记录")} minHeight={220} />}
              pagination={{
                currentPage: page,
                pageSize,
                pageSizeOpts: [20, 50, 100],
                showSizeChanger: true,
                total,
                onPageChange: changePage,
                onPageSizeChange: changePageSize,
              }}
            />}
          </div>
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label={t("审计事件检查器")}>
          <div className="ur-detail-header">
            <div>
              <Text className="ur-panel-hint">{t("审计事件")}</Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("审计事件检查器")}</Typography.Title>
            </div>
            <Tag size="small" color={selectedLog ? 'blue' : undefined}>{selectedLog ? `AUD-${selectedLog.id}` : t("等待事件")}</Tag>
          </div>

          {!selectedLog ? (
            <div className="ur-receipt-empty" role="status">
              <Typography.Title heading={5}>{loading ? t("正在加载审计事实") : loadError ? t("等待可验证记录") : t("暂无审计事件")}</Typography.Title>
              <Text type="tertiary">
                {loadError
                  ? t("加载失败时不会保留上一筛选范围的旧详情。")
                  : t("有记录时会自动选择当前页第一项，无需再次点击打开详情。")}
              </Text>
            </div>
          ) : (
            <div className="ur-detail-stack">
              <div className="ur-detail-hero">
                <div>
                  <Text type="tertiary" size="small">{t("事件")}</Text>
                  <div><Text strong copyable>{selectedLog.action}</Text></div>
                  <Text code size="small">audit:{selectedLog.id}</Text>
                </div>
                <StatusTag status={selectedLog.source || 'unknown'}>
                  {auditSourceLabel(selectedLog.source || '') || selectedLog.source || 'unknown'}
                </StatusTag>
              </div>

              <section className="ur-detail-section">
                <Text strong>{t("主体与范围")}</Text>
                <div className="ur-audit-fact-grid">
                  <span><Text type="tertiary">{t("发生时间")}</Text><code>{formatDateTime(selectedLog.created_at)}</code></span>
                  <span><Text type="tertiary">{t("操作者")}</Text><code>{actorIdentity(selectedLog)}</code></span>
                  <span><Text type="tertiary">{t("组织")}</Text><code>{organizationIdentity(selectedLog)}</code></span>
                  <span><Text type="tertiary">{t("项目")}</Text><code>{selectedLog.project_id ? projectIdentity(selectedLog.project_name, selectedLog.project_id) : t("组织级")}</code></span>
                  <span><Text type="tertiary">{t("操作对象")}</Text><code>{`${selectedLog.resource || '-'}${selectedLog.resource_id ? ` #${selectedLog.resource_id}` : ''}`}</code></span>
                </div>
              </section>

              <section className="ur-detail-section">
                <Text strong>{t("调查关联")}</Text>
                <div className="ur-audit-fact-grid">
                  <span><Text type="tertiary">{t("操作 ID")}</Text><code>{selectedLog.request_id || '-'}</code></span>
                  <span><Text type="tertiary">{t("客户端请求 ID")}</Text><code>{selectedLog.client_request_id || '-'}</code></span>
                  <span><Text type="tertiary">IP</Text><code>{selectedLog.ip || '-'}</code></span>
                  <span><Text type="tertiary">User-Agent</Text><code>{selectedLog.user_agent || '-'}</code></span>
                </div>
              </section>

              {([
                [t("详情"), selectedLog.detail],
                [t("变更前"), selectedLog.before],
                [t("变更后"), selectedLog.after],
              ] as Array<[string, unknown]>).map(([label, value]) => (
                <section className="ur-detail-section" key={String(label)}>
                  <Text strong>{label}</Text>
                  <pre className="ur-json-evidence">{formattedJSON(value)}</pre>
                </section>
              ))}
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
