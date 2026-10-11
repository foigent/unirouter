import '../i18n/operations';
import { useEffect, useRef, useState } from 'react';
import { Button, Input, Select, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconCoinMoney, IconDownload, IconRefresh, IconSearch, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, MoneyText, PageHeader, StatusTag, Surface } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { keyTypeOptions, requestStatusOptions, usageAnalyticsGroupOptions, usageScopeOptions, usageSourceOptions } from '../utils/displayLabels';
import { formatCompactNumber, formatLatencyMs, formatPercent } from '../utils/formatNumber';
import { localizeOptions, translate as t, translateLabel, useI18n } from '../i18n';

const { Text } = Typography;

interface AnalyticsRow {
  key: string;
  label?: string;
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_micros: string;
  avg_latency_ms: number;
  estimated_ratio: number;
}

interface AnalyticsSummary {
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_micros: string;
  avg_latency_ms: number;
  estimated_tokens: number;
  upstream_tokens: number;
  estimated_ratio: number;
  csv_export_scope: string;
  prompt_retention: string;
  response_retention: string;
}

function toRFC3339(value: string) {
  if (!value) return '';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? '' : parsed.toISOString();
}

function retentionLabel(value?: string) {
  if (value === 'not_stored_by_default') return t("默认不存储明文");
  return value || t("默认不存储明文");
}

export default function UsageAnalyticsPage() {
  useI18n();
  const user = useAuthStore((state) => state.user);
  const platform = isPlatformAdminRole(organizationRoleForUser(user));
  const customerContext = resolveCustomerContext(user);
  const [rows, setRows] = useState<AnalyticsRow[]>([]);
  const [summary, setSummary] = useState<AnalyticsSummary | null>(null);
  const [groupBy, setGroupBy] = useState('overall');
  const [status, setStatus] = useState('');
  const [usageSource, setUsageSource] = useState('');
  const [projectId, setProjectId] = useState('');
  const [keyId, setKeyId] = useState('');
  const [ownerUserId, setOwnerUserId] = useState('');
  const [keyType, setKeyType] = useState('');
  const [scope, setScope] = useState('');
  const [model, setModel] = useState('');
  const [alias, setAlias] = useState('');
  const [provider, setProvider] = useState('');
  const [channel, setChannel] = useState('');
  const [startTime, setStartTime] = useState('');
  const [endTime, setEndTime] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const targetOrgID = platform ? effectiveCustomerOrgID(user) : undefined;
  const customerContextReady = !platform || customerContext.state === 'ready';

  const filters = {
    group_by: groupBy,
    ...(targetOrgID ? { org_id: targetOrgID } : {}),
    project_id: projectId,
    key_id: keyId,
    owner_user_id: ownerUserId,
    key_type: keyType,
    scope,
    status,
    usage_source: usageSource,
    model,
    alias,
    provider,
    channel,
    start_time: toRFC3339(startTime),
    end_time: toRFC3339(endTime),
  };

  const load = () => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setRows([]);
    setSummary(null);
    api.getUsageAnalytics(filters)
      .then((data: any) => {
        if (version !== requestVersion.current) return;
        setRows(data?.data || []);
        setSummary(data?.summary || null);
      })
      .catch(() => {
        if (version !== requestVersion.current) return;
        setLoadError('无法加载当前筛选范围的用量与费用数据。');
        Toast.error(t("用量分析加载失败"));
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  const updateAnalyticsFilter = (update: () => void) => {
    requestVersion.current += 1;
    setRows([]);
    setSummary(null);
    setLoadError('');
    update();
  };

  useEffect(() => {
    if (!customerContextReady) return;
    load();
    return () => { requestVersion.current += 1; };
  }, [customerContextReady, groupBy, projectId, keyId, ownerUserId, keyType, scope, status, usageSource, model, alias, provider, channel, startTime, endTime, targetOrgID]);

  const downloadCSV = async () => {
    try {
      const csv = await api.getUsageAnalytics({ ...filters, format: 'csv' }) as string;
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'uniroute-usage-analytics.csv';
      link.click();
      URL.revokeObjectURL(url);
    } catch (err: any) {
      Toast.error(t('CSV 导出失败{error}', { error: err?.message ? `: ${err.message}` : '' }));
    }
  };

  const columns = [
    { title: translateLabel(usageAnalyticsGroupOptions.find(option => option.value === groupBy)?.label || '分组'), dataIndex: 'label', key: 'label', render: (value: string, row: AnalyticsRow) => <Text>{value || row.key}</Text> },
    { title: t("请求数"), dataIndex: 'total_requests', key: 'total_requests', render: (value: number) => formatCompactNumber(value) },
    { title: t("成功 / 失败"), key: 'status_mix', render: (_: unknown, row: AnalyticsRow) => `${row.success_requests || 0} / ${row.failed_requests || 0}` },
    { title: 'Token', dataIndex: 'total_tokens', key: 'total_tokens', render: (value: number) => formatCompactNumber(value) },
    { title: t("费用"), dataIndex: 'cost_micros', key: 'cost_micros', render: (value: string) => <MoneyText micros={value || '0'} precision={6} tone="cost" /> },
    { title: t('平均延迟'), dataIndex: 'avg_latency_ms', key: 'avg_latency_ms', render: (value: number) => formatLatencyMs(value) },
    { title: t("估算占比"), dataIndex: 'estimated_ratio', key: 'estimated_ratio', render: (value: number) => <StatusTag status={(value || 0) > 0.2 ? 'warning' : 'ok'}>{formatPercent(value)}</StatusTag> },
  ];

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state as Exclude<typeof customerContext.state, 'ready'>);
    return (
      <div className="ur-page ur-page-wide">
        <PageHeader eyebrow={t("监控与用量")} title={translateLabel(blocker.title)} description={t("用量和费用必须归属于唯一客户；系统不会把部署技术组织的数据当作客户用量。")} />
        <Surface><EmptyState title={translateLabel(blocker.title)} description={translateLabel(blocker.description)} minHeight={260} /></Surface>
      </div>
    );
  }

  return (
    <div className="ur-page ur-page-wide ur-usage-workspace">
      <PageHeader
        eyebrow={t("监控与用量")}
        title={t("用量分析")}
        description={t('按模型、项目和凭证定位用量与费用，核对计量来源。')}
        actions={<Button icon={<IconDownload />} onClick={downloadCSV}>{t("导出 CSV")}</Button>}
      />
      {!loadError && <div className="ur-kpi-grid" aria-busy={loading}>
        <MetricTile label={t('请求数')} value={summary ? formatCompactNumber(summary.total_requests) : '—'} icon={<IconSearch />} tone="blue" meta={summary ? t('成功 {success} / 失败 {failed}', { success: summary.success_requests ?? '—', failed: summary.failed_requests ?? '—' }) : t('加载中')} />
        <MetricTile label="Token" value={summary ? formatCompactNumber(summary.total_tokens) : '—'} icon={<IconServer />} tone="violet" meta={summary ? t('估算来源 {estimated} / 上游 {upstream}', { estimated: formatCompactNumber(summary.estimated_tokens), upstream: formatCompactNumber(summary.upstream_tokens) }) : '—'} />
        <MetricTile label={t('费用')} value={summary?.cost_micros != null ? <MoneyText micros={summary.cost_micros} precision={6} strong tone="cost" /> : '—'} icon={<IconCoinMoney />} tone="green" meta={summary ? t('平均延迟 {latency}', { latency: summary.total_requests ? formatLatencyMs(summary.avg_latency_ms) : '—' }) : '—'} />
        <MetricTile label={t('估算占比')} value={summary?.total_tokens ? formatPercent(summary.estimated_ratio) : '—'} icon={<IconSearch />} tone={(summary?.estimated_ratio || 0) > 0.2 ? 'amber' : 'slate'} meta={summary?.csv_export_scope === 'filtered_query' ? t('当前筛选口径 / 总览已包含全部匹配数据') : summary?.csv_export_scope || t('当前筛选口径')} />
      </div>}

      <Surface
        title={t("筛选与分组")}
        actions={<Button icon={<IconRefresh />} onClick={load} loading={loading}>{t('刷新')}</Button>}
      >
        <div className="ur-inline-controls">
          {platform && <Text type="tertiary">{t('客户范围：{name}', { name: customerContext.orgName || t('组织 #{id}', { id: targetOrgID || '—' }) })}</Text>}
          <Select aria-labelledby="usage-group-label" insetLabel={t("分组")} insetLabelId="usage-group-label" value={groupBy} optionList={localizeOptions(usageAnalyticsGroupOptions)} onChange={(value) => updateAnalyticsFilter(() => setGroupBy(String(value || 'overall')))} style={{ width: 220 }} />
          <Input aria-label={t('模型筛选')} prefix={<IconSearch />} value={model} onChange={(value) => updateAnalyticsFilter(() => setModel(value))} placeholder={t('模型')} showClear style={{ width: 210 }} />
          <label className="ur-date-filter"><span>{t('开始时间')}</span><Input aria-label={t('用量开始时间')} value={startTime} onChange={(value) => updateAnalyticsFilter(() => setStartTime(value))} type="datetime-local" showClear /></label>
          <label className="ur-date-filter"><span>{t('结束时间')}</span><Input aria-label={t('用量结束时间')} value={endTime} onChange={(value) => updateAnalyticsFilter(() => setEndTime(value))} type="datetime-local" showClear /></label>
        </div>
        <details className="ur-advanced-filters">
          <summary>{t('高级筛选')}{[scope, projectId, keyId, ownerUserId, keyType, status, usageSource, alias, provider, channel].filter(Boolean).length > 0 && <span className="ur-filter-count">{[scope, projectId, keyId, ownerUserId, keyType, status, usageSource, alias, provider, channel].filter(Boolean).length}</span>}</summary>
          <div className="ur-inline-controls">
          <Select
            aria-labelledby="usage-scope-label"
            insetLabel={t("范围")}
            insetLabelId="usage-scope-label"
            value={scope || undefined}
            placeholder={t("用量范围")}
            optionList={localizeOptions(usageScopeOptions)}
            onChange={(value) => updateAnalyticsFilter(() => setScope(String(value || '')))}
            style={{ width: 160 }}
          />
          <Input aria-label={t("项目 ID 筛选")} prefix={<IconSearch />} value={projectId} onChange={(value) => updateAnalyticsFilter(() => setProjectId(value))} placeholder={t("项目 ID")} showClear style={{ width: 140 }} />
          <Input aria-label={t("访问凭证 ID 筛选")} prefix={<IconSearch />} value={keyId} onChange={(value) => updateAnalyticsFilter(() => setKeyId(value))} placeholder={t("访问凭证 ID")} showClear style={{ width: 160 }} />
          <Input aria-label={t("凭证所有者用户 ID 筛选")} prefix={<IconSearch />} value={ownerUserId} onChange={(value) => updateAnalyticsFilter(() => setOwnerUserId(value))} placeholder={t("所有者用户 ID")} showClear style={{ width: 170 }} />
          <Select
            aria-labelledby="usage-key-type-label"
            insetLabel={t("凭证类型")}
            insetLabelId="usage-key-type-label"
            value={keyType || undefined}
            placeholder={t("凭证类型")}
            optionList={localizeOptions(keyTypeOptions)}
            onChange={(value) => updateAnalyticsFilter(() => setKeyType(String(value || '')))}
            style={{ width: 160 }}
          />
          <Select
            aria-labelledby="usage-status-label"
            insetLabel={t("请求状态")}
            insetLabelId="usage-status-label"
            value={status || undefined}
            placeholder={t("请求状态")}
            optionList={localizeOptions(requestStatusOptions)}
            onChange={(value) => updateAnalyticsFilter(() => setStatus(String(value || '')))}
            style={{ width: 150 }}
          />
          <Select
            aria-labelledby="usage-source-label"
            insetLabel={t("计量来源")}
            insetLabelId="usage-source-label"
            value={usageSource || undefined}
            placeholder={t("计量来源")}
            optionList={localizeOptions(usageSourceOptions)}
            onChange={(value) => updateAnalyticsFilter(() => setUsageSource(String(value || '')))}
            style={{ width: 190 }}
          />
          <Input aria-label={t("模型别名筛选")} prefix={<IconSearch />} value={alias} onChange={(value) => updateAnalyticsFilter(() => setAlias(value))} placeholder={t("模型别名")} showClear style={{ width: 180 }} />
          <Input aria-label={t("供应商筛选")} prefix={<IconServer />} value={provider} onChange={(value) => updateAnalyticsFilter(() => setProvider(value))} placeholder={t('供应商')} showClear style={{ width: 180 }} />
          <Input aria-label={t("上游通道筛选")} prefix={<IconServer />} value={channel} onChange={(value) => updateAnalyticsFilter(() => setChannel(value))} placeholder={t("上游通道")} showClear style={{ width: 180 }} />
          </div>
        </details>
      </Surface>

      {loadError ? (
        <Surface>
          <EmptyState
            title={t("用量分析加载失败")}
            description={translateLabel(loadError)}
            actions={<Button type="primary" onClick={load}>{t("重新加载用量分析")}</Button>}
          />
        </Surface>
      ) : <>
      <Surface title={t('拆解结果')} description={t('分组明细按请求数排序，最多显示前 50 组；汇总与 CSV 包含全部匹配数据。')}>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={rows}
            rowKey="key"
            loading={loading}
            pagination={false}
            size="small"
            empty={<EmptyState title={t('暂无匹配用量')} description={t('调整筛选条件后重试，或等待新的请求记录。')} />}
          />
        </div>
      </Surface>
      </>}
      <details className="ur-workspace-notes">
        <summary>{t('数据口径与隐私')}</summary>
        <p>{t('当前分组：{group}。请求内容：{prompt} / 响应内容：{response}', { group: translateLabel(usageAnalyticsGroupOptions.find(option => option.value === groupBy)?.label || '总览（不分组）'), prompt: retentionLabel(summary?.prompt_retention), response: retentionLabel(summary?.response_retention) })}</p>
        <p>{t('未设置筛选表示当前权限范围内全部数据。供应商是 provider family 或 adapter 维度；上游通道是一个具体配置实例，包含 endpoint、凭证、权重/优先级和可用模型。')}</p>
        <p>{t('当前可按项目、访问凭证和 Key Owner 等已证实维度分析；Key Owner 不等于实际调用人。部门、经理链和成本中心维度必须等待企业目录投影与请求时归属快照，当前不做猜测归因。')}</p>
      </details>
    </div>
  );
}
