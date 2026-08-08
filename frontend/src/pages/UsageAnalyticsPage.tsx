import { useEffect, useRef, useState } from 'react';
import { Banner, Button, Input, Select, Table, Toast, Typography } from '@douyinfe/semi-ui';
import { IconCoinMoney, IconDownload, IconRefresh, IconSearch, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, MoneyText, PageHeader, StatusTag, Surface } from '../ui';
import { keyTypeOptions, requestStatusOptions, usageAnalyticsGroupOptions, usageScopeOptions, usageSourceOptions } from '../utils/displayLabels';
import { formatCompactNumber, formatLatencyMs, formatPercent } from '../utils/formatNumber';
import { loadAllPages } from '../utils/loadAllPages';

const { Text } = Typography;

interface AnalyticsRow {
  key: string;
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
  if (value === 'not_stored_by_default') return '默认不存储明文';
  return value || '默认不存储明文';
}

export default function UsageAnalyticsPage() {
  const user = useAuthStore((state) => state.user);
  const platform = isPlatformAdminRole(organizationRoleForUser(user));
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(platform ? user?.org_id : undefined);
  const [organizations, setOrganizations] = useState<Array<{ id: number; name: string }>>([]);
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
  const targetOrgID = platform ? selectedOrgID : undefined;

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
        Toast.error('用量分析加载失败');
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
    load();
    return () => { requestVersion.current += 1; };
  }, [groupBy, projectId, keyId, ownerUserId, keyType, scope, status, usageSource, model, alias, provider, channel, startTime, endTime, targetOrgID]);

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
    requestVersion.current += 1;
    setRows([]);
    setSummary(null);
    setProjectId('');
    setKeyId('');
    setOwnerUserId('');
  };

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
      Toast.error(`CSV 导出失败${err?.message ? `：${err.message}` : ''}`);
    }
  };

  const columns = [
    { title: usageAnalyticsGroupOptions.find(option => option.value === groupBy)?.label || '分组', dataIndex: 'key', key: 'key', render: (value: string) => <Text code>{value}</Text> },
    { title: '请求数', dataIndex: 'total_requests', key: 'total_requests', render: (value: number) => formatCompactNumber(value) },
    { title: '成功 / 失败', key: 'status_mix', render: (_: unknown, row: AnalyticsRow) => `${row.success_requests || 0} / ${row.failed_requests || 0}` },
    { title: 'Token', dataIndex: 'total_tokens', key: 'total_tokens', render: (value: number) => formatCompactNumber(value) },
    { title: '费用', dataIndex: 'cost_micros', key: 'cost_micros', render: (value: string) => <MoneyText micros={value || '0'} precision={6} tone="cost" /> },
    { title: '平均延迟', dataIndex: 'avg_latency_ms', key: 'avg_latency_ms', render: (value: number) => formatLatencyMs(value) },
    { title: '估算占比', dataIndex: 'estimated_ratio', key: 'estimated_ratio', render: (value: number) => <StatusTag status={(value || 0) > 0.2 ? 'warning' : 'ok'}>{formatPercent(value)}</StatusTag> },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="监控与用量"
        title="用量分析"
          description="按总览、项目、访问凭证、Key Owner、凭证类型、模型、供应商、上游通道、请求状态和计量来源查看当前权限范围内的请求量、Token、费用与估算占比。"
        actions={<Button icon={<IconDownload />} onClick={downloadCSV}>导出 CSV</Button>}
      />
      <Banner
        type="info"
        description="当前可按项目、访问凭证和 Key Owner 等已证实维度分析；Key Owner 不等于实际调用人。部门、经理链和成本中心维度必须等待企业目录投影与请求时归属快照，当前不做猜测归因。"
        style={{ marginBottom: 16 }}
      />

      <Surface
        title="筛选与分组"
        description="未设置筛选表示当前权限范围内全部数据。供应商是 provider family 或 adapter 维度；上游通道是一个具体配置实例，包含 endpoint、凭证、权重/优先级和可用模型。"
      >
        <div className="ur-inline-controls">
          {platform && (
            <>
              <Text type="tertiary">目标组织</Text>
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
          <Select value={groupBy} optionList={usageAnalyticsGroupOptions} onChange={(value) => updateAnalyticsFilter(() => setGroupBy(String(value || 'overall')))} style={{ width: 190 }} />
          <Select
            value={scope || undefined}
            placeholder="用量范围"
            optionList={usageScopeOptions}
            onChange={(value) => updateAnalyticsFilter(() => setScope(String(value || '')))}
            style={{ width: 160 }}
          />
          <Input prefix={<IconSearch />} value={projectId} onChange={(value) => updateAnalyticsFilter(() => setProjectId(value))} placeholder="项目 ID" showClear style={{ width: 140 }} />
          <Input prefix={<IconSearch />} value={keyId} onChange={(value) => updateAnalyticsFilter(() => setKeyId(value))} placeholder="访问凭证 ID" showClear style={{ width: 160 }} />
          <Input prefix={<IconSearch />} value={ownerUserId} onChange={(value) => updateAnalyticsFilter(() => setOwnerUserId(value))} placeholder="Owner 用户 ID" showClear style={{ width: 170 }} />
          <Select
            value={keyType || undefined}
            placeholder="凭证类型"
            optionList={keyTypeOptions}
            onChange={(value) => updateAnalyticsFilter(() => setKeyType(String(value || '')))}
            style={{ width: 160 }}
          />
          <Select
            value={status || undefined}
            placeholder="请求状态"
            optionList={requestStatusOptions}
            onChange={(value) => updateAnalyticsFilter(() => setStatus(String(value || '')))}
            style={{ width: 150 }}
          />
          <Select
            value={usageSource || undefined}
            placeholder="计量来源"
            optionList={usageSourceOptions}
            onChange={(value) => updateAnalyticsFilter(() => setUsageSource(String(value || '')))}
            style={{ width: 190 }}
          />
          <Input prefix={<IconSearch />} value={model} onChange={(value) => updateAnalyticsFilter(() => setModel(value))} placeholder="模型" showClear style={{ width: 180 }} />
          <Input prefix={<IconSearch />} value={alias} onChange={(value) => updateAnalyticsFilter(() => setAlias(value))} placeholder="模型别名" showClear style={{ width: 180 }} />
          <Input prefix={<IconServer />} value={provider} onChange={(value) => updateAnalyticsFilter(() => setProvider(value))} placeholder="供应商" showClear style={{ width: 180 }} />
          <Input prefix={<IconServer />} value={channel} onChange={(value) => updateAnalyticsFilter(() => setChannel(value))} placeholder="上游通道" showClear style={{ width: 180 }} />
          <Input value={startTime} onChange={(value) => updateAnalyticsFilter(() => setStartTime(value))} type="datetime-local" placeholder="开始时间" showClear style={{ width: 190 }} />
          <Input value={endTime} onChange={(value) => updateAnalyticsFilter(() => setEndTime(value))} type="datetime-local" placeholder="结束时间" showClear style={{ width: 190 }} />
          <Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新</Button>
        </div>
      </Surface>

      {loadError ? (
        <Surface>
          <EmptyState
            title="用量分析加载失败"
            description={loadError}
            actions={<Button type="primary" onClick={load}>重新加载用量分析</Button>}
          />
        </Surface>
      ) : <>
      <div className="ur-kpi-grid">
        <MetricTile label="请求数" value={formatCompactNumber(summary?.total_requests)} icon={<IconSearch />} tone="blue" meta={`成功 ${summary?.success_requests || 0} / 失败 ${summary?.failed_requests || 0}`} />
        <MetricTile label="Token" value={formatCompactNumber(summary?.total_tokens)} icon={<IconServer />} tone="violet" meta={`估算来源 ${formatCompactNumber(summary?.estimated_tokens)} / 上游 ${formatCompactNumber(summary?.upstream_tokens)}`} />
        <MetricTile label="费用" value={<MoneyText micros={summary?.cost_micros || '0'} precision={6} strong tone="cost" />} icon={<IconCoinMoney />} tone="green" meta={`平均延迟 ${formatLatencyMs(summary?.avg_latency_ms)}`} />
        <MetricTile label="估算占比" value={formatPercent(summary?.estimated_ratio)} icon={<IconSearch />} tone={(summary?.estimated_ratio || 0) > 0.2 ? 'amber' : 'slate'} meta={summary?.csv_export_scope === 'filtered_query' ? '当前筛选口径 / 总览已包含全部匹配数据' : summary?.csv_export_scope || '当前筛选口径'} />
      </div>

      <Surface title="拆解结果" description={`当前分组：${usageAnalyticsGroupOptions.find(option => option.value === groupBy)?.label || '总览（不分组）'}。请求内容：${retentionLabel(summary?.prompt_retention)} / 响应内容：${retentionLabel(summary?.response_retention)}`}>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={rows}
            rowKey="key"
            loading={loading}
            pagination={false}
            size="small"
          />
        </div>
      </Surface>
      </>}
    </div>
  );
}
