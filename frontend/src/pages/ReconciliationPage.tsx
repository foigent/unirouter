import '../i18n/finance';
import type { MessageKey } from '../i18n/catalog';
import { translate as t, useI18n, labelFor, localizeOptions } from '../i18n';
import { useState, useEffect, useRef } from 'react';
import { Banner, Button, Table, Select, TextArea, Toast, Tag, Typography } from '@douyinfe/semi-ui';
import { IconAlertTriangle, IconCoinMoney, IconRefresh, IconServer } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { isBillingRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, formatMoney, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { usageSourceLabel, usageSourceOptions } from '../utils/displayLabels';
import { formatInteger, formatPercent, formatSignedInteger } from '../utils/formatNumber';
import { EnterpriseAllocationWorkspace } from './EnterpriseFinanceWorkspaces';
import { ProviderCostResolutionWorkspace } from '../features/reconciliation/ProviderCostResolutionWorkspace';

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
  useI18n();
  const [evidence, setEvidence] = useState('');
  const [report, setReport] = useState<COGSReport | null>(null);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<{key: MessageKey; values?: Record<string, string | number>} | null>(null);
  const selected = report?.matches[selectedIndex] || null;

  const preview = () => {
    if (!evidence.trim()) {
      setError({key: '请粘贴或载入符合离线合同的 JSON 证据。'});
      return;
    }
    setLoading(true);
    setError(null);
    api.previewProviderCOGSOffline(evidence)
      .then((next: COGSReport) => {
        setReport(next);
        setSelectedIndex(0);
      })
      .catch((previewError: unknown) => {
        setReport(null);
        setSelectedIndex(0);
        setError(previewError instanceof ApiError ? {key: "校验失败（{v0}）：{v1}", values: { v0: String(previewError.status), v1: String(previewError.message) }} : {key: "校验失败：离线证据不可用。"});
      })
      .finally(() => setLoading(false));
  };

  const loadFile = (file?: File) => {
    if (!file) return;
    if (file.size > 4 * 1024 * 1024) {
      setError({key: '文件超过 4 MiB 离线预览上限。'});
      return;
    }
    file.text()
      .then(text => {
        setEvidence(text);
        setReport(null);
        setSelectedIndex(0);
        setError(null);
      })
      .catch(() => setError({key: '无法读取所选文件。'}));
  };

  const columns = [
    {
      title: t("状态"),
      dataIndex: 'status',
      width: 150,
      render: (value: string) => (
        <StatusTag status={value === 'exact_request_matched' ? 'success' : value}>
          {labelFor(cogsStatusLabel, value)}
        </StatusTag>
      ),
    },
    { title: t("供应商行"), dataIndex: 'line_id', width: 160, render: (value: string) => value || '—' },
    { title: t("UniRouter 请求"), dataIndex: 'request_id', width: 180, render: (value: string) => value || '—' },
    {
      title: t("供应商成本"),
      dataIndex: 'provider_cost',
      width: 190,
      render: (value: COGSAmount | undefined) => exactAmountLabel(value),
    },
    {
      title: t("证据级别"),
      dataIndex: 'evidence_level',
      width: 180,
      render: (value: string) => <Tag>{value}</Tag>,
    },
  ];

  return (
    <Surface title={t("导入供应商账单证据（高级）")}>
      <Banner
        type="info"
        description={t("当供应商没有实时账单连接器时，可把从供应商后台导出的 JSON 在这里做一次本地对账预览。内容只在当前浏览器内存中校验：不上传保存、不连接供应商、不接收凭据、不修改客户账。普通运营不需要使用本工具。")}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      <div className="ur-context-explorer ur-financial-truth-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("供应商账单文件内容（JSON）")}</Typography.Title>
              <Typography.Text className="ur-panel-hint">

                {t("合同 unirouter.provider-cogs-offline.v1 · 原始 JSON 不写入浏览器存储")}
              </Typography.Text>
            </div>
            <Tag color="blue">{t("不持久化")}</Tag>
          </div>
          <TextArea
            aria-label={t("供应商成本离线证据 JSON")}
            value={evidence}
            onChange={value => {
              setEvidence(value);
              setReport(null);
              setSelectedIndex(0);
              setError(null);
            }}
            autosize={{ minRows: 8, maxRows: 16 }}
            placeholder={t("粘贴符合当前合同的供应商账单 JSON，或从本机载入文件")}
          />
          <div className="ur-financial-truth-actions">
            <label className="semi-button semi-button-tertiary semi-button-light" tabIndex={0}>

              {t("载入 JSON 文件")}
              <input
                aria-label={t("载入供应商成本 JSON 文件")}
                type="file"
                accept="application/json,.json"
                hidden
                onChange={event => loadFile(event.target.files?.[0])}
              />
            </label>
            <Button type="primary" loading={loading} onClick={preview}>{t("严格校验并预览")}</Button>
            <Button
              disabled={!evidence && !report}
              onClick={() => {
                setEvidence('');
                setReport(null);
                setSelectedIndex(0);
                setError(null);
              }}
            >

              {t("清空本地内容")}
            </Button>
          </div>
          {error && <Banner type="danger" description={error ? t(error.key, error.values) : undefined} style={{ marginTop: 12 }} />}
          {report && (
            <>
              <div className="ur-kpi-grid ur-financial-truth-kpis">
                <MetricTile label={t("供应商总成本")} value={`${report.summary.supplier_total_minor} / ${report.amount_scale} ${report.currency}`} tone="blue" />
                <MetricTile label={t("请求级精确匹配")} value={formatInteger(report.summary.exact_request_matches)} tone="green" />
                <MetricTile label={t("聚合未归因")} value={formatInteger(report.summary.aggregate_unattributed)} tone="amber" />
                <MetricTile label={t("冲突性候选")} value={formatInteger(report.summary.ambiguous)} tone="red" />
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
        <aside className="ur-explorer-panel ur-explorer-detail" aria-label={t("供应商成本证据检查器")}>
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">{t("财务事实检查器")}</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("证据与权限边界")}</Typography.Title>
            </div>
            <Tag color={report?.customer_ledger_mutation === 'none' ? 'green' : 'red'}>

              {t("客户账：")}{report?.customer_ledger_mutation || t("无操作")}
            </Tag>
          </div>
          {selected ? (
            <dl className="ur-financial-truth-detail">
              <dt>{t("结论")}</dt><dd>{labelFor(cogsStatusLabel, selected.status)}</dd>
              <dt>{t("证据级别")}</dt><dd>{selected.evidence_level}</dd>
              <dt>{t("供应商行")}</dt><dd>{selected.line_id || '—'}</dd>
              <dt>{t("UniRouter 请求 / 尝试")}</dt><dd>{selected.request_id || '—'} / {selected.attempt_id || '—'}</dd>
              <dt>{t("上游请求")}</dt><dd>{selected.upstream_request_id || '—'}</dd>
              <dt>{t("供应商成本")}</dt><dd>{exactAmountLabel(selected.provider_cost)}</dd>
              <dt>{t("UniRouter 成本估算")}</dt><dd>{exactAmountLabel(selected.provider_estimate)}</dd>
              <dt>{t("供应商差额")}</dt><dd>{selected.provider_variance_minor || '—'}</dd>
              <dt>{t("客户收入证据")}</dt><dd>{exactAmountLabel(selected.customer_revenue)}</dd>
              <dt>{t("客户欠款动作")}</dt><dd>{selected.customer_debt_action}</dd>
              <dt>{t("语义证据哈希")}</dt><dd className="ur-mono-break">{report?.import_semantic_hash}</dd>
              <dt>{t("来源快照哈希")}</dt><dd className="ur-mono-break">{report?.source_snapshot_hash}</dd>
            </dl>
          ) : (
            <EmptyState
              title={t("等待离线证据")}
              description={t("检查器始终可见；校验成功后会自动选择第一条结果，无需再次点击打开。")}
              minHeight={280}
            />
          )}
        </aside>
      </div>
    </Surface>
  );
}

export default function ReconciliationPage() {
  useI18n();
  const user = useAuthStore((state) => state.user);
  const organizationRole = organizationRoleForUser(user);
  const platform = isPlatformAdminRole(organizationRole);
	const canManageEnterpriseFinance = platform || isBillingRole(organizationRole);
  const customerContext = resolveCustomerContext(user);
  const customerContextReady = customerContext.state === 'ready';
  const [records, setRecords] = useState<ReconRecord[]>([]);
  const [summary, setSummary] = useState<ReconSummary | null>(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const requestVersion = useRef(0);
  const [modelFilter, setModelFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [usageSourceFilter, setUsageSourceFilter] = useState('');
  const [modelNames, setModelNames] = useState<string[]>([]);
  const targetOrgID = platform ? effectiveCustomerOrgID(user) : undefined;

  const load = (p = page) => {
    if (!customerContextReady) return;
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError(false);
    api.getReconciliation(p, pageSize, {
      ...(targetOrgID ? { org_id: targetOrgID } : {}),
      model: modelFilter,
      status: statusFilter,
      usage_source: usageSourceFilter,
    })
      .then((res: any) => {
        if (version !== requestVersion.current) return;
        setRecords(res.data || []);
        setSummary(res.summary || null);
        setTotal(res.total || 0);
      })
      .catch(() => {
        if (version !== requestVersion.current) return;
        setRecords([]); setSummary(null); setTotal(0); setLoadError(true);
        Toast.error({ content: t('加载失败') });
      })
      .finally(() => { if (version === requestVersion.current) setLoading(false); });
  };

  useEffect(() => { load(); return () => { requestVersion.current += 1; }; }, [customerContextReady, page, pageSize, modelFilter, statusFilter, usageSourceFilter, targetOrgID]);
  useEffect(() => {
    if (!customerContextReady) return;
    api.getModelNames().then((d: any) => setModelNames(d.data || [])).catch(() => {});
  }, [customerContextReady]);

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 80 },
    { title: t("操作 ID"), dataIndex: 'request_id', key: 'request_id', ellipsis: true },
    { title: t("模型"), dataIndex: 'model', key: 'model', width: 150 },
    { title: t("估算 Token"), dataIndex: 'estimated_tokens', key: 'estimated_tokens', width: 120 },
    {
      title: t("记账 Token"), dataIndex: 'actual_tokens', key: 'actual_tokens', width: 150,
      render: (value: number, record: ReconRecord) => (
        <span>
          {formatInteger(value)}
          <Tag size="small" color={['upstream', 'api', ''].includes(record.usage_source || '') ? 'blue' : 'orange'} style={{ marginLeft: 6 }}>
            {['upstream', 'api', ''].includes(record.usage_source || '') ? t("上游回传") : t("本地计量")}
          </Tag>
        </span>
      ),
    },
    {
      title: t("来源"),
      dataIndex: 'usage_source',
      key: 'usage_source',
      width: 132,
      render: (v: string) => v ? <StatusTag status={v}>{usageSourceLabel(v)}</StatusTag> : '-',
    },
    {
      title: t("供应商 Prompt Cache"),
      dataIndex: 'cache_usage_status',
      key: 'cache_usage_status',
      width: 190,
      render: (_: string, record: ReconRecord) => record.cache_usage_schema
        ? <Tag color={(record.cache_read_tokens || 0) > 0 ? 'green' : 'blue'}>{t("读")} {formatInteger(record.cache_read_tokens)}  {t("/ 写")} {formatInteger(record.cache_creation_tokens)}</Tag>
        : <Tag>{t("未回传")}</Tag>,
    },
    {
      title: t("差值"),
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
      title: t("偏差率"),
      dataIndex: 'variance_ratio',
      key: 'variance_ratio',
      width: 100,
      render: (v: number) => <Tag color={(v || 0) > 0 ? 'red' : (v || 0) < 0 ? 'green' : 'grey'}>{formatPercent(v)}</Tag>,
    },
    {
      title: t("置信度"),
      dataIndex: 'source_confidence',
      key: 'source_confidence',
      width: 100,
      render: (v: number) => formatPercent(v),
    },
    {
      title: t("状态"),
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => (
        <StatusTag status={v === 'reconciled' ? 'success' : v === 'pending' ? 'pending' : 'failed'}>
          {v === 'reconciled' ? t("正常") : v === 'error' ? t("错误") : v}
        </StatusTag>
      ),
    },
    { title: t("时间"), dataIndex: 'created_at', key: 'created_at', width: 180 },
  ];

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state as Exclude<typeof customerContext.state, 'ready'>);
    return (
      <div className="ur-page ur-finance-workspace">
        <PageHeader eyebrow={t("成本与财务")} title={t("分摊与对账")} description={t("对账数据必须绑定当前部署唯一客户组织。")} />
        <Surface><EmptyState title={blocker.title} description={blocker.description} minHeight={320} /></Surface>
      </div>
    );
  }

  return (
    <div className="ur-page ur-finance-workspace">
      <PageHeader
        eyebrow={t("成本与财务")}
        title={t("分摊与对账")}
        description={t("核对请求结算、成本差异与企业分摊；估算和预览不产生财务入账。")}
      />
      <details className="ur-technical-details">
        <summary>{t('结算口径与功能边界')}</summary>
        <p>{t("核对客户结算账、UniRouter 成本估算与供应商成本证据；任何估算、聚合账单或离线预览都不会自动变成应收、应付或总账事实。")}</p>
        <p>{t("当前已实现请求级对账、企业分摊运行、关账门禁、调整/冲销流水和离线供应商证据。ERP/总账导出仍未配置；跨期异常关联和深度调查继续由 X-Insight 承担。")}</p>
      </details>
      <Surface title={t("筛选条件")}>
        <div style={{ display: 'flex', gap: 16, alignItems: 'center', flexWrap: 'wrap' }}>
          {platform && <Typography.Text type="tertiary">{t("客户范围：")}{customerContext.orgName || t("组织 #{v0}", { v0: String(targetOrgID) })}</Typography.Text>}
          <Select
            aria-labelledby="reconciliation-model-label"
            insetLabel={t("模型")}
            insetLabelId="reconciliation-model-label"
            placeholder={t("模型筛选")}
            style={{ width: 200 }}
            value={modelFilter || undefined}
            onChange={(v) => { setModelFilter((v as string) || ''); setPage(1); }}
            optionList={[
              { label: t("全部模型"), value: '' },
              ...modelNames.map(m => ({ label: m, value: m })),
            ]}
          />
          <Select
            aria-labelledby="reconciliation-status-label"
            insetLabel={t("状态")}
            insetLabelId="reconciliation-status-label"
            placeholder={t("状态筛选")}
            style={{ width: 150 }}
            value={statusFilter || undefined}
            onChange={(v) => { setStatusFilter((v as string) || ''); setPage(1); }}
            optionList={[
              { label: t("全部"), value: '' },
              { label: t("正常"), value: 'reconciled' },
              { label: t("错误"), value: 'error' },
            ]}
          />
          <Select
            aria-labelledby="reconciliation-source-label"
            insetLabel={t("计量来源")}
            insetLabelId="reconciliation-source-label"
            placeholder={t("来源筛选")}
            style={{ width: 190 }}
            value={usageSourceFilter || undefined}
            onChange={(v) => { setUsageSourceFilter((v as string) || ''); setPage(1); }}
            optionList={localizeOptions(usageSourceOptions)}
          />
          <Button icon={<IconRefresh />} onClick={() => load()}>{t("刷新")}</Button>
        </div>
      </Surface>
      {loading || loadError || !summary ? (
        <Surface><EmptyState title={loading ? t('正在加载数据') : t('汇总暂不可用')} description={loading ? t('正在读取当前范围，请稍候。') : t('当前响应未提供汇总，不能按零解读。')} minHeight={180} /></Surface>
      ) : (
      <div className="ur-kpi-grid">
        <MetricTile
          label={t("估算占比")}
          value={formatPercent(summary?.estimated_ratio)}
          icon={<IconAlertTriangle />}
          tone={summary?.estimated_usage_alert ? 'red' : 'green'}
          meta={(
            <span>

              {t("阈值")} {formatPercent(summary?.estimated_ratio_alert_threshold)}
              {' '}
              <StatusTag status={summary?.estimated_usage_alert ? 'warning' : 'success'}>
                {summary?.estimated_usage_alert ? t("已触发") : t("正常")}
              </StatusTag>
            </span>
          )}
        />
        <MetricTile
          label={t("估算来源 Token")}
          value={formatInteger(summary?.estimated_tokens)}
          icon={<IconServer />}
          tone="amber"
          meta={t("口径 {v0}", { v0: String(summary?.estimated_ratio_scope === 'filtered_query' ? t("当前筛选口径") : summary?.estimated_ratio_scope || t("当前筛选口径")) })}
        />
        <MetricTile
          label={t("上游回传 Token")}
          value={formatInteger(summary?.upstream_tokens)}
          icon={<IconCoinMoney />}
          tone="blue"
          meta={t("记账合计 {v0}", { v0: String(formatInteger(summary?.actual_tokens)) })}
        />
        <MetricTile
          label={t("估算与记账总偏差")}
          value={formatSignedInteger(summary?.variance_tokens)}
          icon={<IconAlertTriangle />}
          tone={(summary?.variance_tokens || 0) === 0 ? 'slate' : 'violet'}
          meta={t("记账合计 − 请求前估算；偏差率 {v0} / 置信度 {v1}", { v0: String(formatPercent(summary?.variance_ratio)), v1: String(formatPercent(summary?.average_source_confidence)) })}
        />
        <MetricTile
          label={t("缓存读取 Token")}
          value={formatInteger(summary?.cache_read_tokens)}
          icon={<IconCoinMoney />}
          tone="green"
          meta={t("读取费用 {v0}；权威回传 {v1} 条", { v0: String(formatMoney(summary?.cache_read_cost_micros || '0', 6)), v1: String(formatInteger(summary?.authoritative_cache_records)) })}
        />
        <MetricTile
          label={t("缓存写入 Token")}
          value={formatInteger(summary?.cache_creation_tokens)}
          icon={<IconServer />}
          tone="violet"
          meta={t("写入费用 {v0}；未回传 {v1} 条", { v0: String(formatMoney(summary?.cache_creation_cost_micros || '0', 6)), v1: String(formatInteger(summary?.unreported_cache_records)) })}
        />
      </div>
      )}
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
      {platform && targetOrgID && <ProviderCostResolutionWorkspace orgId={targetOrgID} />}
      <EnterpriseAllocationWorkspace orgId={targetOrgID} canManage={canManageEnterpriseFinance} />
      <details className="ur-advanced-disclosure">
        <summary>{t("高级：导入供应商账单证据")}</summary>
        <Typography.Paragraph type="tertiary">{t("仅当你已经从供应商后台导出兼容 JSON，且需要核对供应商成本时使用。展开不会读取文件或发送请求。")}</Typography.Paragraph>
        <ProviderCOGSOfflineWorkspace />
      </details>
    </div>
  );
}
