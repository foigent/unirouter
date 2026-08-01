import { useState, useEffect } from 'react';
import { Banner, Button, Table, Select, TextArea, Toast, Tag, Typography } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconCoinMoney, IconRefresh, IconServer } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, formatMoney, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { usageSourceLabel, usageSourceOptions } from '../utils/displayLabels';
import { formatInteger, formatPercent, formatSignedInteger } from '../utils/formatNumber';
import { loadAllPages } from '../utils/loadAllPages';

interface ReconRecord {
  id: number;
  request_id: string;
  model: string;
  estimated_tokens: number;
  actual_tokens: number;
  delta: number;
  usage_source?: string;
  variance_tokens?: number;
  variance_ratio?: number;
  source_confidence?: number;
  uncached_prompt_tokens?: number;
  cache_read_tokens?: number;
  cache_creation_tokens?: number;
  cache_usage_schema?: string;
  cache_usage_status?: string;
  pricing_version?: string;
  cache_read_cost_micros?: string;
  cache_creation_cost_micros?: string;
  status: string;
  created_at: string;
}

interface ReconSummary {
  estimated_tokens: number;
  upstream_tokens: number;
  preflight_estimated_tokens: number;
  actual_tokens: number;
  variance_tokens: number;
  variance_ratio: number;
  average_source_confidence: number;
  estimated_ratio: number;
  estimated_ratio_alert_threshold: number;
  estimated_usage_alert: boolean;
  estimated_ratio_scope: string;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  cache_read_cost_micros: string;
  cache_creation_cost_micros: string;
  authoritative_cache_records: number;
  unreported_cache_records: number;
}

interface COGSAmount {
  value_minor: string;
  currency: string;
  scale: string;
  authority: string;
}

interface COGSMatch {
  status: string;
  evidence_level: string;
  line_id: string;
  request_id: string;
  attempt_id: string;
  upstream_request_id: string;
  project_ref: string;
  provider_cost?: COGSAmount;
  provider_estimate?: COGSAmount;
  provider_variance_minor?: string;
  customer_revenue?: COGSAmount;
  customer_debt_action: string;
}

interface COGSReport {
  report_version: string;
  contract_version: string;
  import_id: string;
  import_payload_hash: string;
  import_semantic_hash: string;
  source_snapshot_hash: string;
  provider: string;
  source_instance: string;
  currency: string;
  amount_scale: string;
  replay_status: string;
  customer_ledger_mutation: string;
  matches: COGSMatch[];
  summary: {
    pages: number;
    unique_supplier_lines: number;
    duplicate_noop_lines: number;
    source_attempts: number;
    exact_request_matches: number;
    supplier_only: number;
    aggregate_unattributed: number;
    ambiguous: number;
    source_only: number;
    supplier_evidence_pending: number;
    supplier_total_minor: string;
    matched_supplier_minor: string;
    unattributed_minor: string;
  };
}

const cogsStatusLabel: Record<string, string> = {
  exact_request_matched: '请求级精确匹配',
  supplier_only: '仅供应商证据',
  aggregate_unattributed: '聚合成本未归因',
  ambiguous: '候选不唯一',
  source_only: '仅 UniRouter 证据',
  supplier_evidence_pending: '等待供应商证据',
};

function exactAmountLabel(amount?: COGSAmount) {
  if (!amount) return '—';
  return `${amount.value_minor} / ${amount.scale} ${amount.currency}`;
}

function ProviderCOGSOfflineWorkspace() {
  const [evidence, setEvidence] = useState('');
  const [report, setReport] = useState<COGSReport | null>(null);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const selected = report?.matches[selectedIndex] || null;

  const preview = () => {
    if (!evidence.trim()) {
      setError('请粘贴或载入符合离线合同的 JSON 证据。');
      return;
    }
    setLoading(true);
    setError('');
    api.previewProviderCOGSOffline(evidence)
      .then((next: COGSReport) => {
        setReport(next);
        setSelectedIndex(0);
      })
      .catch((previewError: unknown) => {
        setReport(null);
        setSelectedIndex(0);
        setError(previewError instanceof ApiError
          ? `校验失败（${previewError.status}）：${previewError.message}`
          : '校验失败：离线证据不可用。');
      })
      .finally(() => setLoading(false));
  };

  const loadFile = (file?: File) => {
    if (!file) return;
    if (file.size > 4 * 1024 * 1024) {
      setError('文件超过 4 MiB 离线预览上限。');
      return;
    }
    file.text()
      .then(text => {
        setEvidence(text);
        setReport(null);
        setSelectedIndex(0);
        setError('');
      })
      .catch(() => setError('无法读取所选文件。'));
  };

  const columns = [
    {
      title: '状态',
      dataIndex: 'status',
      width: 150,
      render: (value: string) => (
        <StatusTag status={value === 'exact_request_matched' ? 'success' : value}>
          {cogsStatusLabel[value] || value}
        </StatusTag>
      ),
    },
    { title: '供应商行', dataIndex: 'line_id', width: 160, render: (value: string) => value || '—' },
    { title: 'UniRouter 请求', dataIndex: 'request_id', width: 180, render: (value: string) => value || '—' },
    {
      title: '供应商成本',
      dataIndex: 'provider_cost',
      width: 190,
      render: (value: COGSAmount | undefined) => exactAmountLabel(value),
    },
    {
      title: '证据级别',
      dataIndex: 'evidence_level',
      width: 180,
      render: (value: string) => <Tag>{value}</Tag>,
    },
  ];

  return (
    <Surface title="供应商成本离线预览">
      <Banner
        type="info"
        description="只在内存中校验最多 4 MiB 的导出证据：不保存、不连接供应商、不接收凭据、不修改客户账。只有唯一上游请求证据才允许请求级匹配；聚合账单永不自动摊到客户。"
        style={{ marginBottom: 16 }}
      />
      <div className="ur-context-explorer ur-financial-truth-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>离线证据</Typography.Title>
              <Typography.Text className="ur-panel-hint">
                合同 unirouter.provider-cogs-offline.v1 · 原始 JSON 不写入浏览器存储
              </Typography.Text>
            </div>
            <Tag color="blue">zero persistence</Tag>
          </div>
          <TextArea
            aria-label="供应商成本离线证据 JSON"
            value={evidence}
            onChange={value => {
              setEvidence(value);
              setReport(null);
              setSelectedIndex(0);
              setError('');
            }}
            autosize={{ minRows: 8, maxRows: 16 }}
            placeholder="粘贴 provider-cogs offline JSON，或从本机载入文件"
          />
          <div className="ur-financial-truth-actions">
            <label className="semi-button semi-button-tertiary semi-button-light" tabIndex={0}>
              载入 JSON 文件
              <input
                aria-label="载入供应商成本 JSON 文件"
                type="file"
                accept="application/json,.json"
                hidden
                onChange={event => loadFile(event.target.files?.[0])}
              />
            </label>
            <Button type="primary" loading={loading} onClick={preview}>严格校验并预览</Button>
            <Button
              disabled={!evidence && !report}
              onClick={() => {
                setEvidence('');
                setReport(null);
                setSelectedIndex(0);
                setError('');
              }}
            >
              清空本地内容
            </Button>
          </div>
          {error && <Banner type="danger" description={error} style={{ marginTop: 12 }} />}
          {report && (
            <>
              <div className="ur-kpi-grid ur-financial-truth-kpis">
                <MetricTile label="供应商总成本" value={`${report.summary.supplier_total_minor} / ${report.amount_scale} ${report.currency}`} tone="blue" />
                <MetricTile label="请求级精确匹配" value={formatInteger(report.summary.exact_request_matches)} tone="green" />
                <MetricTile label="聚合未归因" value={formatInteger(report.summary.aggregate_unattributed)} tone="amber" />
                <MetricTile label="冲突性候选" value={formatInteger(report.summary.ambiguous)} tone="red" />
              </div>
              <div className="ur-table-shell">
                <Table
                  rowKey={(row?: COGSMatch) => row
                    ? `${row.line_id || 'source'}:${row.request_id || ''}:${row.attempt_id || ''}`
                    : 'missing'}
                  columns={columns}
                  dataSource={report.matches}
                  pagination={false}
                  scroll={{ x: 860 }}
                  onRow={(row?: COGSMatch) => {
                    const index = row ? report.matches.indexOf(row) : 0;
                    return {
                      onClick: () => setSelectedIndex(Math.max(index, 0)),
                      'aria-selected': selectedIndex === Math.max(index, 0),
                    };
                  }}
                />
              </div>
            </>
          )}
        </main>
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="供应商成本证据检查器">
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">Financial Truth Inspector</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>证据与权限边界</Typography.Title>
            </div>
            <Tag color={report?.customer_ledger_mutation === 'none' ? 'green' : 'red'}>
              客户账：{report?.customer_ledger_mutation || '无操作'}
            </Tag>
          </div>
          {selected ? (
            <dl className="ur-financial-truth-detail">
              <dt>结论</dt><dd>{cogsStatusLabel[selected.status] || selected.status}</dd>
              <dt>证据级别</dt><dd>{selected.evidence_level}</dd>
              <dt>供应商行</dt><dd>{selected.line_id || '—'}</dd>
              <dt>UniRouter 请求 / 尝试</dt><dd>{selected.request_id || '—'} / {selected.attempt_id || '—'}</dd>
              <dt>上游请求</dt><dd>{selected.upstream_request_id || '—'}</dd>
              <dt>供应商成本</dt><dd>{exactAmountLabel(selected.provider_cost)}</dd>
              <dt>UniRouter 成本估算</dt><dd>{exactAmountLabel(selected.provider_estimate)}</dd>
              <dt>供应商差额</dt><dd>{selected.provider_variance_minor || '—'}</dd>
              <dt>客户收入证据</dt><dd>{exactAmountLabel(selected.customer_revenue)}</dd>
              <dt>客户欠款动作</dt><dd>{selected.customer_debt_action}</dd>
              <dt>语义证据哈希</dt><dd className="ur-mono-break">{report?.import_semantic_hash}</dd>
              <dt>Source Snapshot 哈希</dt><dd className="ur-mono-break">{report?.source_snapshot_hash}</dd>
            </dl>
          ) : (
            <EmptyState
              title="等待离线证据"
              description="检查器始终可见；校验成功后会自动选择第一条结果，无需再次点击打开。"
              minHeight={280}
            />
          )}
        </aside>
      </div>
    </Surface>
  );
}

export default function ReconciliationPage() {
  const user = useAuthStore((state) => state.user);
  const platform = isPlatformAdminRole(user?.role);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(platform ? user?.org_id : undefined);
  const [organizations, setOrganizations] = useState<Array<{ id: number; name: string }>>([]);
  const [records, setRecords] = useState<ReconRecord[]>([]);
  const [summary, setSummary] = useState<ReconSummary | null>(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [modelFilter, setModelFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [usageSourceFilter, setUsageSourceFilter] = useState('');
  const [modelNames, setModelNames] = useState<string[]>([]);
  const targetOrgID = platform ? selectedOrgID : undefined;

  const load = (p = page) => {
    setLoading(true);
    api.getReconciliation(p, pageSize, {
      ...(targetOrgID ? { org_id: targetOrgID } : {}),
      model: modelFilter,
      status: statusFilter,
      usage_source: usageSourceFilter,
    })
      .then((res: any) => {
        setRecords(res.data || []);
        setSummary(res.summary || null);
        setTotal(res.total || 0);
      })
      .catch(() => Toast.error({ content: '加载失败' }))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, [page, pageSize, modelFilter, statusFilter, usageSourceFilter, targetOrgID]);
  useEffect(() => { api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {}); }, []);
  useEffect(() => {
    if (!platform) return;
    if (user?.org_id) setSelectedOrgID(current => current || user.org_id);
    loadAllPages<{ id: number; name: string }>((page, size) => api.getOrganizations(page, size))
      .then(setOrganizations)
      .catch((err: any) => Toast.error(`组织列表加载失败${err?.message ? `：${err.message}` : ''}`));
  }, [platform, user?.org_id]);

  const changeOrganization = (value: unknown) => {
    const orgID = Number(value);
    if (!Number.isSafeInteger(orgID) || orgID <= 0) return;
    setSelectedOrgID(orgID);
    setPage(1);
    setRecords([]);
    setSummary(null);
    setTotal(0);
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 80 },
    { title: 'Request ID', dataIndex: 'request_id', key: 'request_id', ellipsis: true },
    { title: '模型', dataIndex: 'model', key: 'model', width: 150 },
    { title: '估算 Token', dataIndex: 'estimated_tokens', key: 'estimated_tokens', width: 120 },
    {
      title: '记账 Token', dataIndex: 'actual_tokens', key: 'actual_tokens', width: 150,
      render: (value: number, record: ReconRecord) => (
        <span>
          {formatInteger(value)}
          <Tag size="small" color={['upstream', 'api', ''].includes(record.usage_source || '') ? 'blue' : 'orange'} style={{ marginLeft: 6 }}>
            {['upstream', 'api', ''].includes(record.usage_source || '') ? '上游回传' : '本地计量'}
          </Tag>
        </span>
      ),
    },
    {
      title: '来源',
      dataIndex: 'usage_source',
      key: 'usage_source',
      width: 132,
      render: (v: string) => v ? <StatusTag status={v}>{usageSourceLabel(v)}</StatusTag> : '-',
    },
    {
      title: '供应商 Prompt Cache',
      dataIndex: 'cache_usage_status',
      key: 'cache_usage_status',
      width: 190,
      render: (_: string, record: ReconRecord) => record.cache_usage_schema
        ? <Tag color={(record.cache_read_tokens || 0) > 0 ? 'green' : 'blue'}>读 {formatInteger(record.cache_read_tokens)} / 写 {formatInteger(record.cache_creation_tokens)}</Tag>
        : <Tag>未回传</Tag>,
    },
    {
      title: '差值',
      dataIndex: 'delta',
      key: 'delta',
      width: 100,
      render: (v: number) => (
        <Tag color={v > 0 ? 'red' : v < 0 ? 'green' : 'grey'}>
          {v > 0 ? '+' : ''}{v}
        </Tag>
      ),
    },
    {
      title: '偏差率',
      dataIndex: 'variance_ratio',
      key: 'variance_ratio',
      width: 100,
      render: (v: number) => <Tag color={(v || 0) > 0 ? 'red' : (v || 0) < 0 ? 'green' : 'grey'}>{formatPercent(v)}</Tag>,
    },
    {
      title: '置信度',
      dataIndex: 'source_confidence',
      key: 'source_confidence',
      width: 100,
      render: (v: number) => formatPercent(v),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => (
        <StatusTag status={v === 'reconciled' ? 'success' : v === 'pending' ? 'pending' : 'failed'}>
          {v === 'reconciled' ? '正常' : v === 'error' ? '错误' : v}
        </StatusTag>
      ),
    },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="计费与资金"
        title="财务真值"
        description="分开呈现客户收入账、UniRouter 成本估算与供应商成本证据；任何估算、聚合账单或离线预览都不会自动变成客户欠款。"
      />
      <Surface title="筛选条件">
        <div style={{ display: 'flex', gap: 16, alignItems: 'center', flexWrap: 'wrap' }}>
          {platform && (
            <>
              <span>目标组织</span>
              <Select
                aria-label="目标组织"
                placeholder="选择目标组织"
                value={selectedOrgID}
                optionList={organizations.map(org => ({ label: `${org.name}（ID ${org.id}）`, value: org.id }))}
                onChange={changeOrganization}
                disabled={loading}
                style={{ width: 220 }}
              />
            </>
          )}
          <Select
            placeholder="模型筛选"
            style={{ width: 200 }}
            value={modelFilter || undefined}
            onChange={(v) => { setModelFilter((v as string) || ''); setPage(1); }}
            optionList={[
              { label: '全部模型', value: '' },
              ...modelNames.map(m => ({ label: m, value: m })),
            ]}
          />
          <Select
            placeholder="状态筛选"
            style={{ width: 150 }}
            value={statusFilter || undefined}
            onChange={(v) => { setStatusFilter((v as string) || ''); setPage(1); }}
            optionList={[
              { label: '全部', value: '' },
              { label: '正常', value: 'reconciled' },
              { label: '错误', value: 'error' },
            ]}
          />
          <Select
            placeholder="来源筛选"
            style={{ width: 190 }}
            value={usageSourceFilter || undefined}
            onChange={(v) => { setUsageSourceFilter((v as string) || ''); setPage(1); }}
            optionList={usageSourceOptions}
          />
          <Button icon={<IconRefresh />} onClick={() => load()}>刷新</Button>
        </div>
      </Surface>
      <div className="ur-kpi-grid">
        <MetricTile
          label="估算占比"
          value={formatPercent(summary?.estimated_ratio)}
          icon={<IconAlertTriangle />}
          tone={summary?.estimated_usage_alert ? 'red' : 'green'}
          meta={(
            <span>
              阈值 {formatPercent(summary?.estimated_ratio_alert_threshold)}
              {' '}
              <StatusTag status={summary?.estimated_usage_alert ? 'warning' : 'success'}>
                {summary?.estimated_usage_alert ? '已触发' : '正常'}
              </StatusTag>
            </span>
          )}
        />
        <MetricTile
          label="估算来源 Token"
          value={formatInteger(summary?.estimated_tokens)}
          icon={<IconServer />}
          tone="amber"
          meta={`口径 ${summary?.estimated_ratio_scope === 'filtered_query' ? '当前筛选口径' : summary?.estimated_ratio_scope || '当前筛选口径'}`}
        />
        <MetricTile
          label="上游回传 Token"
          value={formatInteger(summary?.upstream_tokens)}
          icon={<IconCoinMoney />}
          tone="blue"
          meta={`记账合计 ${formatInteger(summary?.actual_tokens)}`}
        />
        <MetricTile
          label="估算与记账总偏差"
          value={formatSignedInteger(summary?.variance_tokens)}
          icon={<IconAlertTriangle />}
          tone={(summary?.variance_tokens || 0) === 0 ? 'slate' : 'violet'}
          meta={`记账合计 − 请求前估算；偏差率 ${formatPercent(summary?.variance_ratio)} / 置信度 ${formatPercent(summary?.average_source_confidence)}`}
        />
        <MetricTile
          label="缓存读取 Token"
          value={formatInteger(summary?.cache_read_tokens)}
          icon={<IconCoinMoney />}
          tone="green"
          meta={`读取费用 ${formatMoney(summary?.cache_read_cost_micros || '0', 6)}；权威回传 ${formatInteger(summary?.authoritative_cache_records)} 条`}
        />
        <MetricTile
          label="缓存写入 Token"
          value={formatInteger(summary?.cache_creation_tokens)}
          icon={<IconServer />}
          tone="violet"
          meta={`写入费用 ${formatMoney(summary?.cache_creation_cost_micros || '0', 6)}；未回传 ${formatInteger(summary?.unreported_cache_records)} 条`}
        />
      </div>
      <Surface>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={records}
            loading={loading}
            rowKey="id"
            pagination={{
              currentPage: page,
              pageSize,
              pageSizeOpts: [20, 50, 100],
              showSizeChanger: true,
              total,
              onPageChange: (p: number) => setPage(p),
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); },
            }}
          />
        </div>
      </Surface>
      <ProviderCOGSOfflineWorkspace />
    </div>
  );
}
