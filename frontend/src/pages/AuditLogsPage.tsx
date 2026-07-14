import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Button, DatePicker, Input, Select, SideSheet, Space, Table, Tag } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { PageHeader, Surface } from '../ui';
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

export default function AuditLogsPage() {
  const [logs, setLogs] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [filters, setFilters] = useState<AuditFilters>({});
  const [selectedLog, setSelectedLog] = useState<any | null>(null);

  const load = (p = page, size = pageSize) => {
    setLoading(true);
    api.getAuditLogs(p, size, filters)
      .then((d: any) => { setLogs(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(page, pageSize); }, [page, pageSize, filters]);

  const updateFilter = (key: keyof AuditFilters, value: string | undefined) => {
    setFilters(prev => ({ ...prev, [key]: value || undefined }));
    setPage(1);
  };

  const clearFilters = () => {
    setFilters({});
    setPage(1);
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '组织', dataIndex: 'org_id', width: 170, render: (v: number, row: any) => scopedIdentity(row.org_name, v, '组织') },
    { title: '项目', dataIndex: 'project_id', width: 170, render: (v: number | null, row: any) => scopedIdentity(row.project_name, v, '项目') },
    { title: '操作者', dataIndex: 'actor_id', width: 170, render: (v: number, row: any) => scopedIdentity(row.actor_name, v, '用户') },
    {
      title: '动作', dataIndex: 'action',
      width: 180,
      render: (v: string) => <Tag color={actionColor(v)}>{v}</Tag>,
    },
    { title: '目标类型', dataIndex: 'resource', width: 130 },
    { title: '目标ID', dataIndex: 'resource_id', width: 150, render: (v: string) => v || '-' },
    { title: '操作 ID', dataIndex: 'request_id', width: 190, render: (v: string) => v || '-' },
    { title: '客户端请求 ID', dataIndex: 'client_request_id', width: 170, render: (v: string) => v || '-' },
    { title: '来源', dataIndex: 'source', width: 150, render: (v: string) => v ? <Tag>{auditSourceLabel(v)}</Tag> : '-' },
    { title: '详情', width: 88, render: (_: unknown, row: any) => <Button size="small" onClick={() => setSelectedLog(row)}>查看</Button> },
    { title: '时间', dataIndex: 'created_at', width: 180, render: (v: string) => formatDateTime(v) },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问控制"
        title="审计日志"
        description="查看登录、配置变更、入账、额度调整和结算等关键后台动作。"
      />
      <Surface>
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
      <Surface>
        <div className="ur-table-shell">
          <Table columns={columns} dataSource={logs} loading={loading} rowKey="id"
            scroll={{ x: 1550, y: 620 }}
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
