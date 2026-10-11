import { Button, Table, Tag, Typography } from '@douyinfe/semi-ui';
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import type { DashboardStats, DashboardChannelHealth } from '../../api/dashboard';
import { authzContextForUser, isBillingRole, isPlatformAdminRole, isRouteAllowedForAuthz } from '../../navigation';
import { useAuthStore } from '../../store/auth';
import { EmptyState, LatencyText, MetricTile, MoneyText, StatusTag, Surface, formatMoney, microsToChartYuan, parseMicros } from '../../ui';
import { useI18n, translateLabel } from '../../i18n';
import { formatDateTime } from '../../utils/formatDate';
import ModelComposition from './ModelComposition';
import { modelBreakdown } from './modelBreakdown';

const { Text } = Typography;
const EMPTY_LOGS: DashboardStats['recent_logs'] = [];
const validAmount = (value?: string): value is string => typeof value === 'string' && /^-?\d+$/.test(value);
const recordedMoney = (value?: string) => validAmount(value) ? formatMoney(value) : '—';
export type DashboardAttention = { attention_level: string; open_critical: number; open_warning: number };
export type AttentionLoadState = 'idle' | 'loading' | 'ready' | 'error' | 'locked';

function deploymentLabel(ch: DashboardChannelHealth) {
  const labels: Record<string, string> = {
    disabled: '禁用', stale_release: '发布记录已过期', projection_unavailable: '路由状态不可判定',
    awaiting_release: '待启用', standby: '未承载当前路由', no_credential: '缺少凭证',
    circuit_unavailable: '熔断不可用', error: '异常', warning: '告警', degraded: '告警',
  };
  return translateLabel(labels[ch.status] || (ch.available ? '可路由' : '不可路由'));
}

export default function OverviewWorkspace({ stats, attention, attentionState }: {
  stats: DashboardStats; attention: DashboardAttention | null; attentionState: AttentionLoadState;
}) {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const user = useAuthStore(state => state.user);
  const authz = authzContextForUser(user);
  const platform = isPlatformAdminRole(authz.organizationRole);
  const canReadWallet = platform || isBillingRole(authz.organizationRole);
  const canOpen = (path: string) => isRouteAllowedForAuthz(path, authz);
  const [selected, setSelected] = useState<string | null>(() => [...(stats.model_distribution || [])].sort((a, b) => b.requests - a.requests)[0]?.model || null);
  const models = [...(stats.model_distribution || [])].sort((a, b) => b.requests - a.requests || a.model.localeCompare(b.model));
  const requests = modelBreakdown(stats.model_distribution === undefined ? undefined : models, 'requests');
  const costs = modelBreakdown(stats.model_distribution === undefined ? undefined : models, 'cost');
  const current = models.find(row => row.model === selected);
  const count = (value: number) => value.toLocaleString(locale);
  const duration = (value?: number) => value !== undefined && value > 0 ? `${Math.round(value).toLocaleString(locale)} ms` : '—';
  const rate = (value: number, samples: number) => samples > 0 ? `${(value * 100).toFixed(2)}%` : t('暂无样本');
  const channels = stats.channel_health || [];
  const walletKnown = validAmount(stats.wallet_balance_micros) && validAmount(stats.wallet_granted_micros);
  const wallet = walletKnown ? parseMicros(stats.wallet_balance_micros!) + parseMicros(stats.wallet_granted_micros!) : null;
  const threshold = validAmount(stats.wallet_threshold_micros) ? parseMicros(stats.wallet_threshold_micros) : null;
  const hasThreshold = threshold !== null && threshold > 0n;
  const lowBalance = wallet !== null && hasThreshold && wallet <= threshold;
  const trend = (stats.daily_trend || []).map(point => ({ ...point, cost_yuan: microsToChartYuan(point.cost_micros) }));
  const trendLabel = (value: string) => {
    if (!value.includes('T')) return value.slice(5, 10);
    return new Date(value).toLocaleString(locale, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
  };
  const share = (value: bigint, total: bigint) => total > 0n ? `${(Number(value * 10000n / total) / 100).toFixed(2)}%` : '—';

  return <div className="ur-overview-layout">
    <div className="ur-overview-main">
      <div className="ur-composition-grid">
        <ModelComposition data={requests} metric="requests" selected={current?.model || null} onSelect={setSelected} fallbackTotal={Number.isSafeInteger(stats.total_requests) ? BigInt(stats.total_requests) : undefined} />
        <ModelComposition data={costs} metric="cost" selected={current?.model || null} onSelect={setSelected} fallbackTotal={validAmount(stats.today_cost_micros) ? BigInt(stats.today_cost_micros) : undefined} />
      </div>
      <div className="ur-overview-kpis">
        <MetricTile label={t('全模型成功率')} value={rate(stats.success_rate, stats.total_requests)} tone={stats.total_requests === 0 ? 'slate' : stats.success_rate >= 0.99 ? 'green' : 'amber'} meta={t('窗口统计')} />
        <MetricTile label={t('全模型 P95 延迟')} value={duration(stats.p95_latency_ms)} meta={`Avg ${duration(stats.avg_latency_ms)} · P99 ${duration(stats.p99_latency_ms)}`} />
        {platform && <MetricTile label={t('当前可路由部署操作')} value={`${channels.filter(ch => ch.available === true).length}/${channels.length}`} meta={t('当前状态 · 不随时间筛选')} />}
      </div>
      <Surface title={t('请求与费用趋势')} description={t('共用时间范围 · 独立纵轴')}>
        <div className="ur-overview-trends">
          {(['requests', 'cost_yuan'] as const).map(metric => <div className="ur-overview-trend" key={metric}>
            <h3>{t(metric === 'requests' ? '请求数' : '费用 · CNY')}</h3>
            {trend.length === 0 ? <EmptyState title={t('暂无趋势数据')} minHeight={190} /> : <ResponsiveContainer width="100%" height={210}>
              <AreaChart data={trend} syncId="overview-window" margin={{ top: 12, right: 16, left: 0, bottom: 0 }} accessibilityLayer>
                <defs><linearGradient id={`overview-${metric}`} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor={metric === 'requests' ? '#5576df' : '#35a095'} stopOpacity={0.16} /><stop offset="100%" stopColor="#ffffff" stopOpacity={0} /></linearGradient></defs>
                <CartesianGrid vertical={false} stroke="var(--ur-border-soft)" strokeDasharray="3 3" />
                <XAxis dataKey="date" tickFormatter={trendLabel} tick={{ fontSize: 11 }} minTickGap={40} axisLine={false} tickLine={false} />
                <YAxis tick={{ fontSize: 11 }} width={64} allowDecimals={metric !== 'requests'} axisLine={false} tickLine={false} tickFormatter={value => Number(value).toLocaleString(locale, { notation: 'compact', maximumFractionDigits: 2 })} />
                <Tooltip labelFormatter={label => trendLabel(String(label))} formatter={(value, _name, item) => [metric === 'cost_yuan' ? formatMoney(item.payload.cost_micros) : Number(value).toLocaleString(locale), t(metric === 'cost_yuan' ? '费用' : '请求')]} />
                <Area type="linear" dataKey={metric} stroke={metric === 'requests' ? '#5576df' : '#35a095'} strokeWidth={2} fill={`url(#overview-${metric})`} dot={trend.length === 1} isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>}
          </div>)}
        </div>
      </Surface>
      <Surface title={t('模型运行质量')} description={t('当前窗口所有模型；标识保留原样。')}>
        <div className="ur-table-shell"><Table size="small" rowKey="model" dataSource={models} pagination={false} empty={t('当前没有模型统计')} scroll={{ y: 320 }} columns={[
          { title: t('模型'), dataIndex: 'model', render: (model: string) => <button type="button" className="ur-model-select" aria-pressed={current?.model === model} onClick={() => setSelected(model)}>{model}</button> },
          { title: t('请求'), dataIndex: 'requests', render: count },
          { title: t('费用'), dataIndex: 'cost_micros', render: recordedMoney },
          { title: t('成功率'), render: (_: unknown, row) => rate(row.success_rate, row.requests) },
          { title: t('P95'), dataIndex: 'p95_latency_ms', render: duration },
        ]} /></div>
      </Surface>
      {platform && <Surface title={t('模型部署可用状态')} description={t('当前路由可用性与所选窗口的请求结果分别展示。')}>
        {channels.length === 0 ? <EmptyState title={t('暂无模型部署数据')} /> : <div className="ur-deployment-health-list">
          {channels.map(ch => <button type="button" className="ur-deployment-health-row" key={`${ch.deployment_id}:${ch.model_operation_id}`} aria-label={t('查看模型部署 {name}', { name: ch.name })} onClick={() => navigate(`/channels?deployment_id=${ch.deployment_id}&operation_id=${encodeURIComponent(ch.model_operation_id)}`)}>
            <span className="ur-deployment-identity"><strong>{ch.name}</strong><small>{ch.provider} · {ch.model_operation_id}</small><small>{(ch.public_models || [ch.public_model]).join(', ')}</small></span>
            <span><small>{t('窗口成功率')}</small><strong>{rate(ch.success_count / Math.max(1, ch.success_count + ch.fail_count), ch.success_count + ch.fail_count)}</strong></span>
            <span><small>{t('窗口请求')}</small><strong>{count(ch.success_count + ch.fail_count)}</strong></span>
            <span><small>{t('窗口失败')}</small><strong>{count(ch.fail_count)}</strong></span>
            <StatusTag status={ch.available ? 'available' : !ch.enabled ? 'disabled' : 'warning'}>{deploymentLabel(ch)}</StatusTag>
          </button>)}
        </div>}
      </Surface>}
      <Surface title={t('近期执行请求')} description={t('独立显示最近请求，不代表所选窗口完整聚合。')}>
        <div className="ur-table-shell"><Table dataSource={stats.recent_logs ?? EMPTY_LOGS} rowKey="request_id" pagination={false} size="small" empty={t('调用尚未发生或当前没有可见记录。')} columns={[
          { title: 'Request ID', dataIndex: 'request_id', render: (id: string) => canOpen('/logs') ? <button className="ur-request-link" onClick={() => navigate(`/logs?request_id=${encodeURIComponent(id)}`)} title={id}>{id}</button> : <Text code>{id}</Text> },
          { title: t('模型'), dataIndex: 'model', render: (value: string) => <Tag size="small">{value}</Tag> },
          { title: t('路由身份'), render: (_: unknown, row) => row.route_identity === 'canonical_deployment_operation' && row.route_deployment_id && row.route_model_operation_id ? `${t('部署')} #${row.route_deployment_id} · ${row.route_model_operation_id}` : row.route_identity === 'legacy_history' && row.route_channel_id ? `${t('历史连接')}：${row.channel || `#${row.route_channel_id}`}` : t('未选择路由') },
          { title: t('状态'), dataIndex: 'status_code', render: (value: number) => <StatusTag status={value}>{value}</StatusTag> },
          { title: t('耗时'), dataIndex: 'latency_ms', render: (value: number) => <LatencyText value={value} /> },
          { title: t('记录费用'), dataIndex: 'cost_micros', render: (value?: string) => value === undefined ? '—' : <MoneyText micros={value} precision={6} /> },
          { title: t('时间'), dataIndex: 'created_at', render: (value: string) => formatDateTime(value) },
        ]} /></div>
      </Surface>
    </div>
    <aside className="ur-overview-inspector" aria-label={t('模型详情')}>
      <Surface title={t('模型详情')} actions={current && <Button size="small" theme="borderless" onClick={() => setSelected(null)}>{t('全部模型')}</Button>}>
        <label className="ur-model-picker">{t('选择模型')}<select value={current?.model || ''} onChange={event => setSelected(event.target.value || null)}><option value="">{t('全部模型')}</option>{models.map(model => <option key={model.model} value={model.model}>{model.model}</option>)}</select></label>
        {!current ? <EmptyState title={t('选择模型')} description={t('从构成图或模型列表选择，查看请求、费用和运行质量。')} minHeight={160} /> : <>
          <h3 className="ur-inspector-model">{current.model}</h3>
          <dl className="ur-fact-list">
            <div><dt>{t('请求数')}</dt><dd>{count(current.requests)}</dd></div>
            <div><dt>{t('请求占比')}</dt><dd>{Number.isSafeInteger(current.requests) ? share(BigInt(current.requests), requests.total) : '—'}</dd></div>
            <div><dt>{t('请求费用')}</dt><dd>{recordedMoney(current.cost_micros)}</dd></div>
            <div><dt>{t('费用占比')}</dt><dd>{costs.state === 'ready' && current.cost_micros !== undefined ? share(parseMicros(current.cost_micros), costs.total) : '—'}</dd></div>
            <div><dt>{t('成功 / 失败')}</dt><dd>{count(current.success_count)} / {count(current.fail_count)}</dd></div>
            <div><dt>{t('成功率')}</dt><dd>{rate(current.success_rate, current.requests)}</dd></div>
            <div><dt>{t('平均耗时')}</dt><dd>{duration(current.avg_latency_ms)}</dd></div>
            <div><dt>{t('P95 耗时')}</dt><dd>{duration(current.p95_latency_ms)}</dd></div>
          </dl>
        </>}
        <div className="ur-inspector-actions">
          {canOpen('/logs') && <Button block onClick={() => navigate(current ? `/logs?model=${encodeURIComponent(current.model)}` : '/logs')}>{t('查看执行请求')}</Button>}
          {canOpen('/usage-analytics') && <Button block onClick={() => navigate('/usage-analytics')}>{t('查看用量分析')}</Button>}
        </div>
      </Surface>
      {(canReadWallet || platform) && <Surface title={t('结算与通知')}>
        {canReadWallet && <>
          <span className="ur-fact-caption">{t('结算可用额度')}</span><strong className="ur-settlement-value">{wallet === null ? '—' : formatMoney(wallet)}</strong>
          <dl className="ur-fact-list">
            <div><dt>{t('请求预留')}</dt><dd>{recordedMoney(stats.wallet_frozen_micros)}</dd></div>
            <div><dt>{t('低余额阈值')}</dt><dd>{threshold === null ? '—' : hasThreshold ? formatMoney(threshold) : t('阈值未配置')}</dd></div>
          </dl>
          {wallet !== null && hasThreshold && <StatusTag status={lowBalance ? 'warning' : 'available'}>{t(lowBalance ? '已触发低余额' : '余额正常')}</StatusTag>}
          {canOpen('/wallet') && <Button block className="ur-inspector-action" onClick={() => navigate('/wallet')}>{t('查看结算账户')}</Button>}
        </>}
        {platform && <div className="ur-overview-notifications">
          <span className="ur-fact-caption">{t('未确认通知')}</span>
          <strong className="ur-settlement-value">{attentionState === 'ready' && attention ? count(attention.open_critical + attention.open_warning) : t(attentionState === 'loading' ? '读取中' : '读取失败')}</strong>
          {attentionState === 'ready' && attention && <p>{t('严重 {critical} · 预警 {warning}', { critical: attention.open_critical, warning: attention.open_warning })}</p>}
          <div className="ur-inspector-actions">
            {canOpen('/notifications') && attentionState !== 'locked' && <Button block onClick={() => navigate('/notifications')}>{t('处理通知')}</Button>}
            {canOpen('/status') && <Button block onClick={() => navigate('/status')}>{t('查看运行状态')}</Button>}
          </div>
        </div>}
      </Surface>}
    </aside>
  </div>;
}
