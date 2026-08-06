import { useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, ButtonGroup, Input, Select, Spin, Table, Toast, Typography } from '@douyinfe/semi-ui';
import {
  IconAlertTriangle,
  IconBarChartVStroked,
  IconCoinMoney,
  IconFlowChartStroked,
  IconRefresh,
  IconSearch,
  IconServer,
  IconSetting,
} from '@douyinfe/semi-icons';
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip as ChartTooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, MoneyText, PageHeader, StatusTag, Surface, microsToChartYuan } from '../ui';
import { formatCompactNumber, formatPercent } from '../utils/formatNumber';
import { loadAllPages } from '../utils/loadAllPages';

const { Text, Title } = Typography;

type GroupKey = 'overall' | 'project' | 'model' | 'provider' | 'channel';
type TrendMode = 'daily' | 'cumulative';

interface CostOptimizationSummary {
  metric_version: string;
  currency: string;
  amount_scale: number;
  amount_authority: string;
  candidate_requests: number;
  eligible_requests: number;
  cache_used_requests: number;
  cache_read_requests: number;
  cache_write_requests: number;
  prompt_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  ordinary_input_baseline_cost_micros: string;
  actual_cost_micros: string;
  net_savings_micros: string;
  cache_read_savings_micros: string;
  cache_creation_premium_micros: string;
  rounding_adjustment_micros: string;
  evidence_coverage_ratio: number;
  cache_read_token_ratio: number;
  read_to_creation_token_ratio: number | null;
  response_cache_runtime: string;
  compression_runtime: string;
  response_cache_shadow_samples: number;
  response_cache_eligible_requests: number;
  response_cache_hits: number;
  response_cache_avoided_cost_micros: string;
  compression_shadow_samples: number;
  compression_eligible_requests: number;
  compression_applied_requests: number;
  compression_estimated_saved_tokens: number;
  supplier_cogs_authority: string;
}

interface CostOptimizationTrend {
  day: string;
  candidate_requests: number;
  eligible_requests: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  net_savings_micros: string;
  cache_read_savings_micros: string;
  cache_creation_premium_micros: string;
  evidence_coverage_ratio: number;
}

interface CostOptimizationBreakdown {
  key: string;
  candidate_requests: number;
  eligible_requests: number;
  cache_used_requests: number;
  prompt_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  ordinary_input_baseline_cost_micros: string;
  actual_cost_micros: string;
  net_savings_micros: string;
  cache_read_savings_micros: string;
  cache_creation_premium_micros: string;
  rounding_adjustment_micros: string;
  evidence_coverage_ratio: number;
  cache_read_token_ratio: number;
  read_to_creation_token_ratio: number | null;
}

interface CostOptimizationOpportunity {
  kind: string;
  severity: string;
  group_key: string;
  group_label: string;
  title: string;
  rationale: string;
  next_action: string;
  evidence: Record<string, unknown>;
}

const groupOptions = [
  { label: '总体', value: 'overall' },
  { label: '模型', value: 'model' },
  { label: '项目', value: 'project' },
  { label: '供应商', value: 'provider' },
  { label: '上游通道', value: 'channel' },
];

function toRFC3339(value: string) {
  if (!value) return '';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? '' : parsed.toISOString();
}

function localDateTimeInput(date: Date) {
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function defaultStartTime() {
  const start = new Date();
  start.setDate(start.getDate() - 29);
  start.setHours(0, 0, 0, 0);
  return localDateTimeInput(start);
}

function moneySign(value?: string) {
  try {
    const parsed = BigInt(value || '0');
    return parsed === 0n ? 0 : parsed > 0n ? 1 : -1;
  } catch {
    return 0;
  }
}

function ratioLabel(value: number | null | undefined) {
  return value === null || value === undefined ? '无缓存写入基数' : `${value.toFixed(2)}×`;
}

function driverStatusLabel(value?: string) {
  if (value === 'runtime_unavailable') return '运行时密钥未配置';
  if (value === 'ready_default_off') return '运行时就绪 / 默认关闭';
  if (value === 'shadow_active') return 'Shadow 评估中';
  if (value === 'canary_active') return '受控 Canary';
  if (value === 'validated') return '已验证 / 待 Shadow';
  if (value === 'draft') return '草稿';
  if (value === 'rolled_back') return '已回滚';
  if (value === 'default_off') return '默认关闭';
  return value || '未声明';
}

function driverStatusTone(value?: string) {
  if (value === 'canary_active') return 'warning';
  if (value === 'shadow_active' || value === 'validated') return 'processing';
  if (value === 'runtime_unavailable' || value === 'rolled_back') return 'disabled';
  return 'ok';
}

export default function CostOptimizationPage() {
  const navigate = useNavigate();
  const user = useAuthStore(state => state.user);
  const platform = isPlatformAdminRole(organizationRoleForUser(user));
  const [organizations, setOrganizations] = useState<Array<{ id: number; name: string }>>([]);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(platform ? user?.org_id : undefined);
  const [groupBy, setGroupBy] = useState<GroupKey>('model');
  const [projectID, setProjectID] = useState('');
  const [model, setModel] = useState('');
  const [provider, setProvider] = useState('');
  const [channel, setChannel] = useState('');
  const [startTime, setStartTime] = useState(defaultStartTime);
  const [endTime, setEndTime] = useState('');
  const [trendMode, setTrendMode] = useState<TrendMode>('daily');
  const [summary, setSummary] = useState<CostOptimizationSummary | null>(null);
  const [trend, setTrend] = useState<CostOptimizationTrend[]>([]);
  const [breakdown, setBreakdown] = useState<CostOptimizationBreakdown[]>([]);
  const [opportunities, setOpportunities] = useState<CostOptimizationOpportunity[]>([]);
  const [selectedKey, setSelectedKey] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const targetOrgID = platform ? selectedOrgID : undefined;

  const filters = useMemo(() => ({
    group_by: groupBy,
    ...(targetOrgID ? { org_id: targetOrgID } : {}),
    project_id: projectID,
    model,
    provider,
    channel,
    start_time: toRFC3339(startTime),
    end_time: toRFC3339(endTime),
  }), [channel, endTime, groupBy, model, projectID, provider, startTime, targetOrgID]);

  const load = () => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    Promise.all([
      api.getCostOptimizationSummary(filters),
      api.getCostOptimizationTrend(filters),
      api.getCostOptimizationBreakdown(filters),
      api.getCostOptimizationOpportunities(filters),
    ]).then(([summaryResponse, trendResponse, breakdownResponse, opportunityResponse]: any[]) => {
      if (version !== requestVersion.current) return;
      const nextBreakdown = (breakdownResponse?.data || []) as CostOptimizationBreakdown[];
      setSummary(summaryResponse?.summary || null);
      setTrend(trendResponse?.data || []);
      setBreakdown(nextBreakdown);
      setOpportunities(opportunityResponse?.data || []);
      setSelectedKey(current => nextBreakdown.some(row => row.key === current) ? current : nextBreakdown[0]?.key || '');
    }).catch(() => {
      if (version !== requestVersion.current) return;
      setSummary(null);
      setTrend([]);
      setBreakdown([]);
      setOpportunities([]);
      setSelectedKey('');
      setLoadError('无法加载当前范围的成本优化证据。现有请求与计费运行不受影响。');
      Toast.error('成本优化数据加载失败');
    }).finally(() => {
      if (version === requestVersion.current) setLoading(false);
    });
  };

  useEffect(() => {
    load();
    return () => { requestVersion.current += 1; };
  }, []);

  useEffect(() => {
    if (!platform) return;
    if (user?.org_id) setSelectedOrgID(current => current || user.org_id);
    loadAllPages<{ id: number; name: string }>((page, size) => api.getOrganizations(page, size))
      .then(setOrganizations)
      .catch(() => Toast.error('组织列表加载失败'));
  }, [platform, user?.org_id]);

  const selected = breakdown.find(row => row.key === selectedKey) || breakdown[0] || null;
  const selectedOpportunities = opportunities.filter(item => item.group_key === selected?.key);

  const chartData = useMemo(() => {
    let cumulativeNet = 0n;
    let cumulativeRead = 0n;
    let cumulativePremium = 0n;
    return trend.map(item => {
      const net = BigInt(item.net_savings_micros || '0');
      const read = BigInt(item.cache_read_savings_micros || '0');
      const premium = BigInt(item.cache_creation_premium_micros || '0');
      if (trendMode === 'cumulative') {
        cumulativeNet += net;
        cumulativeRead += read;
        cumulativePremium += premium;
      }
      return {
        ...item,
        net_yuan: microsToChartYuan(trendMode === 'cumulative' ? cumulativeNet : net),
        read_yuan: microsToChartYuan(trendMode === 'cumulative' ? cumulativeRead : read),
        premium_yuan: microsToChartYuan(trendMode === 'cumulative' ? cumulativePremium : premium),
      };
    });
  }, [trend, trendMode]);

  const openSelectedLogs = () => {
    if (!selected) return;
    const query = new URLSearchParams();
    if (targetOrgID) query.set('org_id', String(targetOrgID));
    if (projectID) query.set('project_id', projectID);
    if (model) query.set('model', model);
    if (provider) query.set('provider', provider);
    if (channel) query.set('channel', channel);
    if (groupBy === 'project' && selected.key !== '0') query.set('project_id', selected.key);
    if (groupBy === 'model') query.set('model', selected.key);
    if (groupBy === 'provider') query.set('provider', selected.key);
    if (groupBy === 'channel') query.set('channel', selected.key);
    navigate(`/logs?${query.toString()}`);
  };

  const columns = [
    {
      title: groupOptions.find(option => option.value === groupBy)?.label || '分组',
      dataIndex: 'key',
      key: 'key',
      render: (value: string) => <Text code>{value}</Text>,
    },
    {
      title: '净节省',
      dataIndex: 'net_savings_micros',
      key: 'net_savings_micros',
      render: (value: string) => <MoneyText micros={value || '0'} precision={6} strong tone={moneySign(value) < 0 ? 'warning' : 'income'} />,
    },
    {
      title: '读取节省',
      dataIndex: 'cache_read_savings_micros',
      key: 'cache_read_savings_micros',
      render: (value: string) => <MoneyText micros={value || '0'} precision={6} tone="income" />,
    },
    {
      title: '写入溢价',
      dataIndex: 'cache_creation_premium_micros',
      key: 'cache_creation_premium_micros',
      render: (value: string) => <MoneyText micros={value || '0'} precision={6} tone={moneySign(value) > 0 ? 'cost' : 'income'} />,
    },
    {
      title: '证据覆盖',
      dataIndex: 'evidence_coverage_ratio',
      key: 'evidence_coverage_ratio',
      render: (value: number) => <StatusTag status={(value || 0) >= 0.95 ? 'ok' : 'warning'}>{formatPercent(value)}</StatusTag>,
    },
    {
      title: '缓存读 / 写 Token',
      key: 'cache_tokens',
      render: (_: unknown, row: CostOptimizationBreakdown) => `${formatCompactNumber(row.cache_read_tokens)} / ${formatCompactNumber(row.cache_creation_tokens)}`,
    },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="成本与财务"
        title="成本优化"
        description="先用终态计量事实解释节省，再把策略变更留在模型治理。当前只读分析不会改变路由、计费、缓存、凭据或请求执行。"
        actions={(
          <>
            <Button icon={<IconSetting />} onClick={() => navigate('/model-governance')}>查看模型治理</Button>
            <Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新</Button>
          </>
        )}
      />

      <Banner
        type="info"
        description="Provider Prompt Cache、Exact Response Cache 和 Compression 分开计量：前两者的已实现客户节省来自终态客户计价证据，不等于供应商 COGS；Compression 的 Shadow 数字只表示预计 Token 变化，未进入 Canary 前不宣称真实金额节省。"
        style={{ marginBottom: 16 }}
      />

      <Surface title="范围与归因" description="默认查看最近 30 天。缺失供应商缓存分区或计价证据的成功请求计入覆盖缺口，不按零节省处理。">
        <div className="ur-inline-controls">
          {platform && (
            <Select
              aria-labelledby="cost-optimization-org-label"
              insetLabel="目标组织"
              insetLabelId="cost-optimization-org-label"
              placeholder="选择目标组织"
              value={selectedOrgID}
              optionList={organizations.map(org => ({ label: `${org.name}（ID ${org.id}）`, value: org.id }))}
              onChange={value => setSelectedOrgID(Number(value))}
              style={{ width: 260 }}
              disabled={loading}
            />
          )}
          <Select
            aria-labelledby="cost-optimization-group-label"
            insetLabel="拆解维度"
            insetLabelId="cost-optimization-group-label"
            value={groupBy}
            optionList={groupOptions}
            onChange={value => setGroupBy(String(value || 'model') as GroupKey)}
            style={{ width: 190 }}
          />
          <Input prefix={<IconSearch />} value={projectID} onChange={setProjectID} placeholder="项目 ID" showClear style={{ width: 140 }} />
          <Input prefix={<IconSearch />} value={model} onChange={setModel} placeholder="模型" showClear style={{ width: 180 }} />
          <Input prefix={<IconServer />} value={provider} onChange={setProvider} placeholder="供应商" showClear style={{ width: 170 }} />
          <Input prefix={<IconServer />} value={channel} onChange={setChannel} placeholder="上游通道" showClear style={{ width: 170 }} />
          <Input aria-label="开始时间" value={startTime} onChange={setStartTime} type="datetime-local" style={{ width: 190 }} />
          <Input aria-label="结束时间" value={endTime} onChange={setEndTime} type="datetime-local" showClear style={{ width: 190 }} />
          <Button type="primary" icon={<IconSearch />} onClick={load} loading={loading}>应用筛选</Button>
        </div>
      </Surface>

      <div className="ur-cost-driver-grid" aria-label="成本优化驱动状态">
        <div>
          <IconCoinMoney />
          <span><strong>Provider Prompt Cache</strong><Text type="tertiary">终态 Token 分区与冻结费率</Text></span>
          <StatusTag status="success">已计量</StatusTag>
        </div>
        <div>
          <IconServer />
          <span><strong>Exact Response Cache</strong><Text type="tertiary">精确隔离组件；不复用 Prompt Cache 定价</Text></span>
          <StatusTag status={driverStatusTone(summary?.response_cache_runtime)}>{driverStatusLabel(summary?.response_cache_runtime)}</StatusTag>
        </div>
        <div>
          <IconFlowChartStroked />
          <span><strong>Compression</strong><Text type="tertiary">先 Shadow 评估质量与 Token 变化</Text></span>
          <StatusTag status={driverStatusTone(summary?.compression_runtime)}>{driverStatusLabel(summary?.compression_runtime)}</StatusTag>
        </div>
      </div>

      {loadError ? (
        <Surface>
          <EmptyState title="成本优化证据加载失败" description={loadError} actions={<Button type="primary" onClick={load}>重新加载</Button>} />
        </Surface>
      ) : (
        <Spin spinning={loading}>
          <div className="ur-kpi-grid">
            <MetricTile
              label="Prompt Cache 净节省"
              value={<MoneyText micros={summary?.net_savings_micros || '0'} precision={6} strong tone={moneySign(summary?.net_savings_micros) < 0 ? 'warning' : 'income'} />}
              icon={<IconCoinMoney />}
              tone={moneySign(summary?.net_savings_micros) < 0 ? 'amber' : 'green'}
              meta={<>普通输入基线 <MoneyText micros={summary?.ordinary_input_baseline_cost_micros || '0'} precision={6} /></>}
            />
            <MetricTile
              label="缓存读取节省"
              value={<MoneyText micros={summary?.cache_read_savings_micros || '0'} precision={6} strong tone="income" />}
              icon={<IconBarChartVStroked />}
              tone="blue"
              meta={`读取 Token 占比 ${formatPercent(summary?.cache_read_token_ratio)}`}
            />
            <MetricTile
              label="缓存写入溢价"
              value={<MoneyText micros={summary?.cache_creation_premium_micros || '0'} precision={6} strong tone={moneySign(summary?.cache_creation_premium_micros) > 0 ? 'cost' : 'income'} />}
              icon={<IconServer />}
              tone="violet"
              meta={`读取 / 写入 ${ratioLabel(summary?.read_to_creation_token_ratio)}`}
            />
            <MetricTile
              label="证据覆盖率"
              value={formatPercent(summary?.evidence_coverage_ratio)}
              icon={<IconAlertTriangle />}
              tone={(summary?.evidence_coverage_ratio || 0) >= 0.95 ? 'slate' : 'amber'}
              meta={`${formatCompactNumber(summary?.eligible_requests)} / ${formatCompactNumber(summary?.candidate_requests)} 个成功上游请求`}
            />
          </div>

          <Surface
            title="Exact Cache 与 Compression 结果"
            description="Exact Cache 命中为当前请求零 Provider 调用、零客户收费；Compression Shadow 只保留元数据证据，不保存 Prompt 或 Response 明文。"
          >
            <div className="ur-kpi-grid">
              <MetricTile
                label="Exact Cache 已实现避免成本"
                value={<MoneyText micros={summary?.response_cache_avoided_cost_micros || '0'} precision={6} strong tone="income" />}
                icon={<IconCoinMoney />}
                tone="green"
                meta={`${formatCompactNumber(summary?.response_cache_hits)} 次已完成本地交付`}
              />
              <MetricTile
                label="Cache Shadow 证据"
                value={formatCompactNumber(summary?.response_cache_shadow_samples)}
                icon={<IconServer />}
                tone="blue"
                meta={`${formatCompactNumber(summary?.response_cache_eligible_requests)} 次满足精确身份与请求边界`}
              />
              <MetricTile
                label="Compression 预计节省 Token"
                value={formatCompactNumber(summary?.compression_estimated_saved_tokens)}
                icon={<IconFlowChartStroked />}
                tone="violet"
                meta={`${formatCompactNumber(summary?.compression_shadow_samples)} 个 Shadow 样本`}
              />
              <MetricTile
                label="Compression 实际应用"
                value={formatCompactNumber(summary?.compression_applied_requests)}
                icon={<IconBarChartVStroked />}
                tone="slate"
                meta={`${formatCompactNumber(summary?.compression_eligible_requests)} 次满足安全与质量门槛`}
              />
            </div>
          </Surface>

          <Surface
            title="节省趋势"
            description="净节省 = 普通输入计价基线 − 实际终态计价；读取节省、写入溢价与微元舍入调整共同解释净值。"
            actions={(
              <ButtonGroup aria-label="趋势口径">
                <Button type={trendMode === 'daily' ? 'primary' : 'tertiary'} onClick={() => setTrendMode('daily')}>每日</Button>
                <Button type={trendMode === 'cumulative' ? 'primary' : 'tertiary'} onClick={() => setTrendMode('cumulative')}>累计</Button>
              </ButtonGroup>
            )}
          >
            {chartData.length > 0 ? (
              <ResponsiveContainer width="100%" height={300}>
                <ComposedChart data={chartData} margin={{ top: 8, right: 16, left: 0, bottom: 4 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="var(--ur-border-soft)" />
                  <XAxis dataKey="day" tick={{ fontSize: 12 }} stroke="var(--ur-text-muted)" />
                  <YAxis tick={{ fontSize: 12 }} stroke="var(--ur-text-muted)" tickFormatter={value => `¥${Number(value).toFixed(2)}`} />
                  <ChartTooltip formatter={(value: number, name: string) => [`¥${Number(value).toFixed(6)}`, name]} />
                  <ReferenceLine y={0} stroke="var(--ur-text-muted)" />
                  <Area type="monotone" dataKey="net_yuan" name="净节省" stroke="#16875d" fill="rgba(22, 135, 93, 0.12)" isAnimationActive={false} />
                  <Line type="monotone" dataKey="read_yuan" name="读取节省" stroke="#3568e8" dot={false} isAnimationActive={false} />
                  <Line type="monotone" dataKey="premium_yuan" name="写入溢价" stroke="#a855f7" dot={false} isAnimationActive={false} />
                </ComposedChart>
              </ResponsiveContainer>
            ) : <EmptyState title="当前范围暂无可画趋势" description="无成功上游 Prompt 请求时不会生成虚构的零值趋势。" minHeight={280} />}
          </Surface>

          <div className="ur-context-explorer ur-cost-optimization-explorer">
            <section className="ur-explorer-panel" aria-label="成本优化拆解与候选">
              <div className="ur-detail-header">
                <div>
                  <Text type="tertiary">Savings by driver</Text>
                  <Title heading={5}>节省归因与复核候选</Title>
                </div>
                <StatusTag status="ok">只读</StatusTag>
              </div>
              {breakdown.length > 0 ? (
                <div className="ur-table-shell">
                  <Table
                    columns={columns}
                    dataSource={breakdown}
                    rowKey="key"
                    pagination={false}
                    size="small"
                    onRow={record => record ? ({
                      onClick: () => setSelectedKey(record.key),
                      onKeyDown: event => {
                        if (event.key === 'Enter' || event.key === ' ') setSelectedKey(record.key);
                      },
                      tabIndex: 0,
                      'aria-selected': selected?.key === record.key,
                      className: selected?.key === record.key ? 'is-selected' : '',
                      style: { cursor: 'pointer' },
                    }) : ({})}
                  />
                </div>
              ) : <EmptyState title="当前范围暂无可拆解证据" minHeight={220} />}

              <div className="ur-cost-opportunities" aria-label="确定性优化候选">
                <div className="ur-detail-header">
                  <div>
                    <Text type="tertiary">Deterministic review candidates</Text>
                    <Title heading={6}>待复核项</Title>
                  </div>
                  <StatusTag status={opportunities.length > 0 ? 'warning' : 'ok'}>{opportunities.length} 项</StatusTag>
                </div>
                {opportunities.length > 0 ? opportunities.map((item, index) => (
                  <button key={`${item.kind}-${item.group_key}-${index}`} type="button" className="ur-cost-opportunity" onClick={() => setSelectedKey(item.group_key)}>
                    <StatusTag status={item.severity === 'high' ? 'error' : item.severity === 'warning' ? 'warning' : 'pending'}>{item.severity === 'high' ? '高' : item.severity === 'warning' ? '警告' : '复核'}</StatusTag>
                    <span><strong>{item.title}</strong><Text type="tertiary">{item.group_label}：{item.group_key}</Text></span>
                  </button>
                )) : <Text type="tertiary">当前规则没有生成待复核项；这不代表尚未实现的响应缓存或压缩已产生零节省。</Text>}
              </div>
            </section>

            <aside className="ur-explorer-panel ur-explorer-detail" aria-label="成本优化证据检查器">
              <div className="ur-detail-header">
                <div>
                  <Text type="tertiary">Evidence Inspector</Text>
                  <Title heading={5}>{selected?.key || '口径与边界'}</Title>
                </div>
                <StatusTag status={selected ? 'active' : 'pending'}>{selected ? '已自动选中' : '等待证据'}</StatusTag>
              </div>
              {selected ? (
                <div className="ur-detail-stack">
                  <dl className="ur-financial-truth-detail">
                    <dt>净节省</dt><dd><MoneyText micros={selected.net_savings_micros || '0'} precision={6} strong tone={moneySign(selected.net_savings_micros) < 0 ? 'warning' : 'income'} /></dd>
                    <dt>普通输入基线</dt><dd><MoneyText micros={selected.ordinary_input_baseline_cost_micros || '0'} precision={6} /></dd>
                    <dt>实际终态计价</dt><dd><MoneyText micros={selected.actual_cost_micros || '0'} precision={6} tone="cost" /></dd>
                    <dt>读取节省</dt><dd><MoneyText micros={selected.cache_read_savings_micros || '0'} precision={6} tone="income" /></dd>
                    <dt>写入溢价</dt><dd><MoneyText micros={selected.cache_creation_premium_micros || '0'} precision={6} tone="cost" /></dd>
                    <dt>舍入调整</dt><dd><MoneyText micros={selected.rounding_adjustment_micros || '0'} precision={6} /></dd>
                    <dt>证据覆盖</dt><dd>{formatPercent(selected.evidence_coverage_ratio)}（{selected.eligible_requests}/{selected.candidate_requests}）</dd>
                    <dt>读取 / 写入 Token</dt><dd>{formatCompactNumber(selected.cache_read_tokens)} / {formatCompactNumber(selected.cache_creation_tokens)}</dd>
                  </dl>
                  <div className="ur-detail-card">
                    <strong className="ur-detail-card-title">公式与可信边界</strong>
                    <Text>普通输入基线使用请求时冻结的 input/output 费率重新计算；实际值使用同一终态请求记录的 cost_micros。只纳入成功、上游权威计量、缓存合同与计价版本完整且 Token 分区守恒的请求。</Text>
                    <Text type="tertiary">供应商 COGS 不进入客户计价节省；Exact Cache 只纳入完成本地交付的商业回执；Compression Shadow 仅展示预计 Token，不折算成已实现金额。</Text>
                  </div>
                  {selectedOpportunities.map((item, index) => (
                    <div className="ur-detail-card" key={`${item.kind}-${index}`}>
                      <strong className="ur-detail-card-title">{item.title}</strong>
                      <Text>{item.rationale}</Text>
                      <Text type="tertiary">建议：{item.next_action}</Text>
                    </div>
                  ))}
                  <div className="ur-financial-truth-actions">
                    <Button icon={<IconSearch />} type="primary" onClick={openSelectedLogs}>查看请求证据</Button>
                    <Button icon={<IconFlowChartStroked />} onClick={() => navigate('/reconciliation')}>分摊与对账</Button>
                    <Button onClick={() => navigate('/integrations?tab=xinsight')}>X-Insight 连接</Button>
                  </div>
                </div>
              ) : (
                <EmptyState
                  title="检查器已常驻"
                  description="有拆解结果时会自动选中第一项。当前不会要求额外点击才能看到口径、原因和下一步。"
                  minHeight={280}
                />
              )}
            </aside>
          </div>
        </Spin>
      )}
    </div>
  );
}
