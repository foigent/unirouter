import { formatDateTime } from '../utils/formatDate';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, DatePicker, Input, Select, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { auditSourceLabel, auditSourceOptions } from '../utils/displayLabels';
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
  if (row.actor_id === 0 && row.source === 'system') return '系统';
  return scopedIdentity(row.actor_name, row.actor_id, '用户');
}

export default function AuditLogsPage() {
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
      Toast.error('视图名称需为 1 至 40 个字符，并且浏览器存储必须可用。');
      return;
    }
    const name = savedViewName.trim();
    setSavedViews(next.views);
    setSelectedSavedView(name);
    setSavedViewName(name);
    Toast.success('筛选视图已保存在此浏览器');
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
      Toast.error('无法删除此浏览器中的视图');
      return;
    }
    setSavedViews(next.views);
    setSelectedSavedView(undefined);
    setSavedViewName('');
    Toast.success('已删除此浏览器中的筛选视图');
  };

  const selectedLog = useMemo(
    () => logs.find(log => log.id === selectedLogID) || null,
    [logs, selectedLogID],
  );
  const appliedFilterCount = Object.values(filters).filter(Boolean).length;

  const columns = [
    { title: '时间', dataIndex: 'created_at', width: 170, render: (v: string) => formatDateTime(v) },
    {
      title: '范围', width: 220,
      render: (_: unknown, row: AuditLog) => row.project_id
        ? `${scopedIdentity(row.org_name, row.org_id, '组织')} / ${scopedIdentity(row.project_name, row.project_id, '项目')}`
        : scopedIdentity(row.org_name, row.org_id, '组织'),
    },
    { title: '操作者', dataIndex: 'actor_id', width: 170, render: (_: number, row: AuditLog) => actorIdentity(row) },
    {
      title: '动作', dataIndex: 'action',
      width: 180,
      render: (v: string) => <Tag color={actionColor(v)}>{v}</Tag>,
    },
    { title: '目标', width: 190, render: (_: unknown, row: AuditLog) => `${row.resource || '-'}${row.resource_id ? ` #${row.resource_id}` : ''}` },
    { title: '来源', dataIndex: 'source', width: 150, render: (v: string) => v ? <Tag>{auditSourceLabel(v)}</Tag> : '-' },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问与安全"
        title="审计日志"
        description="查看登录、配置变更、入账、额度调整和结算等关键后台动作。"
      />
      <Surface title="筛选与保存视图" description="先编辑筛选草稿，再应用查询；命名视图只保存在当前浏览器，不上传审计内容。">
        <div className="ur-audit-filter-grid">
          <Input aria-label="项目 ID 筛选" prefix="项目" placeholder="项目 ID" value={draftFilters.project_id || ''} onChange={(v) => updateFilter('project_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label="操作者 ID 筛选" prefix="操作者" placeholder="操作者 ID" value={draftFilters.actor_id || ''} onChange={(v) => updateFilter('actor_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label="审计动作筛选" placeholder="动作" value={draftFilters.action || ''} onChange={(v) => updateFilter('action', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label="目标类型筛选" placeholder="目标类型" value={draftFilters.resource || ''} onChange={(v) => updateFilter('resource', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label="目标 ID 筛选" placeholder="目标 ID" value={draftFilters.resource_id || ''} onChange={(v) => updateFilter('resource_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label="操作 ID 筛选" placeholder="操作 ID（Request ID）" value={draftFilters.request_id || ''} onChange={(v) => updateFilter('request_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Input aria-label="客户端请求 ID 筛选" placeholder="客户端请求 ID" value={draftFilters.client_request_id || ''} onChange={(v) => updateFilter('client_request_id', v)} onEnterPress={() => commitFilters(draftFilters)} />
          <Select aria-labelledby="audit-source-filter-label" insetLabel="来源" insetLabelId="audit-source-filter-label" placeholder="选择来源" value={draftFilters.source || undefined} onChange={(v) => updateFilter('source', v as string)} style={{ width: '100%' }} showClear>
            {auditSourceOptions.map(option => <Select.Option key={option.value} value={option.value}>{option.label}</Select.Option>)}
          </Select>
          <DatePicker
            aria-labelledby="audit-time-range-label"
            insetLabel={<span id="audit-time-range-label">时间范围</span>}
            type="dateTimeRange"
            format="yyyy-MM-dd HH:mm:ss"
            placeholder={['开始时间', '结束时间']}
            showClear
            className="ur-audit-date-range"
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
        <div className="ur-audit-filter-actions">
          <Space wrap>
            <Button size="small" onClick={() => setRecentRange(15 * 60 * 1000)}>最近 15 分钟</Button>
            <Button size="small" onClick={() => setRecentRange(60 * 60 * 1000)}>最近 1 小时</Button>
            <Button size="small" onClick={() => setRecentRange(24 * 60 * 60 * 1000)}>最近 24 小时</Button>
            <Button size="small" onClick={() => setRecentRange(7 * 24 * 60 * 60 * 1000)}>最近 7 天</Button>
          </Space>
          <Space>
            <Tag color={appliedFilterCount ? 'blue' : 'grey'}>{appliedFilterCount ? `已应用 ${appliedFilterCount} 项` : '未筛选'}</Tag>
            <Button aria-label="应用筛选" type="primary" icon={<IconSearch />} onClick={() => commitFilters(draftFilters)}>应用筛选</Button>
            <Button aria-label="重置筛选" icon={<IconRefresh />} onClick={clearFilters}>重置</Button>
          </Space>
        </div>
        <div className="ur-saved-view-bar">
          <div role="group" aria-label="已保存的审计视图">
            <Select
              aria-labelledby="audit-saved-view-label"
              insetLabel="视图"
              insetLabelId="audit-saved-view-label"
              placeholder="已保存视图（此浏览器）"
              value={selectedSavedView}
              optionList={savedViews.map(view => ({ label: view.name, value: view.name }))}
              onChange={value => applySavedView(String(value))}
              showClear
              onClear={() => setSelectedSavedView(undefined)}
              className="ur-saved-view-select"
            />
          </div>
          <Input
            aria-label="审计视图名称"
            placeholder="视图名称"
            value={savedViewName}
            maxLength={40}
            onChange={setSavedViewName}
            onEnterPress={saveCurrentView}
          />
          <Button onClick={saveCurrentView}>保存 / 覆盖</Button>
          <Button disabled={!selectedSavedView} onClick={deleteSavedView}>删除</Button>
        </div>
      </Surface>

      <div className="ur-context-explorer ur-audit-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>审计记录</Typography.Title>
              <Text className="ur-panel-hint">默认选择首条记录 · 详情始终在右侧</Text>
            </div>
            <Space>
              <Tag size="small">{loading && logs.length === 0 ? '读取中' : `${total} 条事件`}</Tag>
              <Button
                size="small"
                icon={<IconRefresh />}
                loading={loading}
                onClick={() => setReloadVersion(version => version + 1)}
              >
                刷新
              </Button>
            </Space>
          </div>
          <div className="ur-table-shell">
            {loading && logs.length === 0 ? (
              <EmptyState title="正在加载审计记录" description="正在读取当前筛选范围的审计事实，请稍候。" minHeight={220} />
            ) : <Table
              columns={columns}
              dataSource={logs}
              loading={loading}
              rowKey="id"
              scroll={{ x: 1080, y: 620 }}
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
                  title="审计记录加载失败"
                  description={loadError}
                  actions={<Button type="primary" onClick={() => setReloadVersion(version => version + 1)}>重新加载审计记录</Button>}
                  minHeight={220}
                />
              ) : <EmptyState title="暂无审计记录" minHeight={220} />}
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

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="审计事件检查器">
          <div className="ur-detail-header">
            <div>
              <Text className="ur-panel-hint">审计事件</Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>审计事件检查器</Typography.Title>
            </div>
            <Tag size="small" color={selectedLog ? 'blue' : undefined}>{selectedLog ? `AUD-${selectedLog.id}` : '等待事件'}</Tag>
          </div>

          {!selectedLog ? (
            <div className="ur-receipt-empty" role="status">
              <Typography.Title heading={5}>{loading ? '正在加载审计事实' : loadError ? '等待可验证记录' : '暂无审计事件'}</Typography.Title>
              <Text type="tertiary">
                {loadError
                  ? '加载失败时不会保留上一筛选范围的旧详情。'
                  : '有记录时会自动选择当前页第一项，无需再次点击打开详情。'}
              </Text>
            </div>
          ) : (
            <div className="ur-detail-stack">
              <div className="ur-detail-hero">
                <div>
                  <Text type="tertiary" size="small">事件</Text>
                  <div><Text strong>{selectedLog.action}</Text></div>
                  <Text code size="small">audit:{selectedLog.id}</Text>
                </div>
                <StatusTag status={selectedLog.source || 'unknown'}>
                  {auditSourceLabel(selectedLog.source || '') || selectedLog.source || 'unknown'}
                </StatusTag>
              </div>

              <section className="ur-detail-section">
                <Text strong>主体与范围</Text>
                <div className="ur-audit-fact-grid">
                  <span><Text type="tertiary">发生时间</Text><code>{formatDateTime(selectedLog.created_at)}</code></span>
                  <span><Text type="tertiary">操作者</Text><code>{actorIdentity(selectedLog)}</code></span>
                  <span><Text type="tertiary">组织</Text><code>{scopedIdentity(selectedLog.org_name, selectedLog.org_id, '组织')}</code></span>
                  <span><Text type="tertiary">项目</Text><code>{selectedLog.project_id ? scopedIdentity(selectedLog.project_name, selectedLog.project_id, '项目') : '组织级'}</code></span>
                  <span><Text type="tertiary">操作对象</Text><code>{`${selectedLog.resource || '-'}${selectedLog.resource_id ? ` #${selectedLog.resource_id}` : ''}`}</code></span>
                </div>
              </section>

              <section className="ur-detail-section">
                <Text strong>调查关联</Text>
                <div className="ur-audit-fact-grid">
                  <span><Text type="tertiary">操作 ID</Text><code>{selectedLog.request_id || '-'}</code></span>
                  <span><Text type="tertiary">客户端请求 ID</Text><code>{selectedLog.client_request_id || '-'}</code></span>
                  <span><Text type="tertiary">IP</Text><code>{selectedLog.ip || '-'}</code></span>
                  <span><Text type="tertiary">User-Agent</Text><code>{selectedLog.user_agent || '-'}</code></span>
                </div>
              </section>

              {([
                ['详情', selectedLog.detail],
                ['变更前', selectedLog.before],
                ['变更后', selectedLog.after],
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
