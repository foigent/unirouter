import { formatDateTime } from '../utils/formatDate';
import { useEffect, useRef, useState } from 'react';
import { Button, DatePicker, Input, Select, SideSheet, Space, Table, Tag } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PageHeader, Surface } from '../ui';
import { auditSourceLabel, auditSourceOptions } from '../utils/displayLabels';

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
  org_id?: string;
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
  const [logs, setLogs] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [filters, setFilters] = useState<AuditFilters>({});
  const [selectedLog, setSelectedLog] = useState<any | null>(null);
  const requestVersion = useRef(0);

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setLogs([]);
    setTotal(0);
    setSelectedLog(null);
    api.getAuditLogs(p, size, filters)
      .then((d: any) => {
        if (version !== requestVersion.current) return;
        setLogs(d.data || []);
        setTotal(d.total || 0);
      })
      .catch(() => {
        if (version === requestVersion.current) setLoadError('无法加载当前筛选条件下的审计记录。');
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  useEffect(() => {
    load(page, pageSize);
    return () => { requestVersion.current += 1; };
  }, [page, pageSize, filters]);

  const updateFilter = (key: keyof AuditFilters, value: string | undefined) => {
    requestVersion.current += 1;
    setLogs([]);
    setTotal(0);
    setLoadError('');
    setFilters(prev => ({ ...prev, [key]: value || undefined }));
    setPage(1);
  };

  const clearFilters = () => {
    requestVersion.current += 1;
    setLogs([]);
    setTotal(0);
    setLoadError('');
    setFilters({});
    setPage(1);
  };

  const columns = [
    { title: '时间', dataIndex: 'created_at', width: 170, render: (v: string) => formatDateTime(v) },
    {
      title: '范围', width: 220,
      render: (_: unknown, row: any) => row.project_id
        ? `${scopedIdentity(row.org_name, row.org_id, '组织')} / ${scopedIdentity(row.project_name, row.project_id, '项目')}`
        : scopedIdentity(row.org_name, row.org_id, '组织'),
    },
    { title: '操作者', dataIndex: 'actor_id', width: 170, render: (_: number, row: any) => actorIdentity(row) },
    {
      title: '动作', dataIndex: 'action',
      width: 180,
      render: (v: string) => <Tag color={actionColor(v)}>{v}</Tag>,
    },
    { title: '目标', width: 190, render: (_: unknown, row: any) => `${row.resource || '-'}${row.resource_id ? ` #${row.resource_id}` : ''}` },
    { title: '来源', dataIndex: 'source', width: 150, render: (v: string) => v ? <Tag>{auditSourceLabel(v)}</Tag> : '-' },
    { title: '详情', width: 88, render: (_: unknown, row: any) => <Button size="small" onClick={() => setSelectedLog(row)}>查看</Button> },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问与安全"
        title="审计日志"
        description="查看登录、配置变更、入账、额度调整和结算等关键后台动作。"
      />
      <Surface title="筛选条件">
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12 }}>
          <Input prefix="组织" placeholder="组织 ID" value={filters.org_id || ''} onChange={(v) => updateFilter('org_id', v)} />
          <Input prefix="项目" placeholder="项目 ID" value={filters.project_id || ''} onChange={(v) => updateFilter('project_id', v)} />
          <Input prefix="操作者" placeholder="操作者 ID" value={filters.actor_id || ''} onChange={(v) => updateFilter('actor_id', v)} />
          <Input placeholder="动作" value={filters.action || ''} onChange={(v) => updateFilter('action', v)} />
          <Input placeholder="目标类型" value={filters.resource || ''} onChange={(v) => updateFilter('resource', v)} />
          <Input placeholder="目标 ID" value={filters.resource_id || ''} onChange={(v) => updateFilter('resource_id', v)} />
          <Input placeholder="操作 ID（Request ID）" value={filters.request_id || ''} onChange={(v) => updateFilter('request_id', v)} />
          <Input placeholder="客户端请求 ID" value={filters.client_request_id || ''} onChange={(v) => updateFilter('client_request_id', v)} />
          <Select placeholder="来源" value={filters.source || undefined} onChange={(v) => updateFilter('source', v as string)} style={{ width: '100%' }} showClear>
            {auditSourceOptions.map(option => <Select.Option key={option.value} value={option.value}>{option.label}</Select.Option>)}
          </Select>
          <DatePicker
            type="dateTimeRange"
            format="yyyy-MM-dd HH:mm:ss"
            placeholder={['开始时间', '结束时间']}
            showClear
            style={{ width: '100%', gridColumn: 'span 2' }}
            value={filters.start_time && filters.end_time ? [new Date(filters.start_time), new Date(filters.end_time)] : undefined}
            onChange={(value) => {
              requestVersion.current += 1;
              setLogs([]);
              setTotal(0);
              setLoadError('');
              const range = Array.isArray(value) ? value : [];
              setFilters(prev => ({
                ...prev,
                start_time: dateValueToISO(range[0] as Date | string | number | null | undefined),
                end_time: dateValueToISO(range[1] as Date | string | number | null | undefined),
              }));
              setPage(1);
            }}
          />
        </div>
        <Space style={{ marginTop: 12 }}>
          <Button icon={<IconSearch />} onClick={() => load(1, pageSize)}>筛选</Button>
          <Button icon={<IconRefresh />} onClick={clearFilters}>重置</Button>
        </Space>
      </Surface>
      <Surface title="审计记录" description="列表保留定位所需的核心字段；操作 ID、客户端请求 ID、IP、User-Agent 和变更 JSON 请在详情中查看。">
        <div className="ur-table-shell">
          <Table columns={columns} dataSource={logs} loading={loading} rowKey="id"
            scroll={{ x: 1050, y: 620 }}
            empty={loadError ? (
              <EmptyState
                title="审计记录加载失败"
                description={loadError}
                actions={<Button type="primary" onClick={() => load(page, pageSize)}>重新加载审计记录</Button>}
                minHeight={220}
              />
            ) : <EmptyState title="暂无审计记录" minHeight={220} />}
            pagination={{ currentPage: page, pageSize, pageSizeOpts: [20, 50, 100], showSizeChanger: true, total, onPageChange: (p) => setPage(p), onPageSizeChange: (s) => { setPageSize(s); setPage(1); } }} />
        </div>
      </Surface>

      <SideSheet
        title={selectedLog ? `审计详情 #${selectedLog.id}` : '审计详情'}
        visible={!!selectedLog}
        onCancel={() => setSelectedLog(null)}
        width={620}
      >
        {selectedLog && (
          <div style={{ display: 'grid', gap: 16 }}>
            <section>
              <strong>请求与来源</strong>
              <div style={{ display: 'grid', gap: 8, marginTop: 8 }}>
                <span>操作 ID：<code>{selectedLog.request_id || '-'}</code></span>
                <span>客户端请求 ID：<code>{selectedLog.client_request_id || '-'}</code></span>
                <span>来源：{auditSourceLabel(selectedLog.source) || '-'}</span>
                <span>IP：<code>{selectedLog.ip || '-'}</code></span>
                <span>User-Agent：<code style={{ overflowWrap: 'anywhere' }}>{selectedLog.user_agent || '-'}</code></span>
              </div>
            </section>
            {[
              ['详情', selectedLog.detail],
              ['变更前', selectedLog.before],
              ['变更后', selectedLog.after],
            ].map(([label, value]) => (
              <section key={String(label)}>
                <strong>{label}</strong>
                <pre style={{ marginTop: 8, padding: 12, borderRadius: 8, background: 'var(--semi-color-fill-0)', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 280, overflow: 'auto' }}>
                  {JSON.stringify(value || {}, null, 2)}
                </pre>
              </section>
            ))}
          </div>
        )}
      </SideSheet>
    </div>
  );
}
