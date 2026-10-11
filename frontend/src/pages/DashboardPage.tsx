import { useState, useEffect, useCallback, useRef } from 'react';
import { Button, Spin } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { dashboardApi, type DashboardStats } from '../api/dashboard';
import { EmptyState, PageHeader, Surface } from '../ui';
import { useAuthStore } from '../store/auth';
import { authzContextForUser, hasLicensedFeature, isBillingRole, isOrgViewerRole, isPlatformAdminRole } from '../navigation';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { useI18n, translateLabel } from '../i18n';
import OverviewWorkspace, { type DashboardAttention, type AttentionLoadState } from '../features/dashboard/OverviewWorkspace';

type TimeRange = '24h' | '7d' | '30d';
const days: Record<TimeRange, number> = { '24h': 1, '7d': 7, '30d': 30 };

export default function DashboardPage() {
  const { t, locale } = useI18n();
  const user = useAuthStore(state => state.user);
  const organizationRole = authzContextForUser(user).organizationRole;
  const customerContext = resolveCustomerContext(user);
  const selectedOrgID = effectiveCustomerOrgID(user);
  const platform = isPlatformAdminRole(organizationRole);
  const scope = JSON.stringify([selectedOrgID, organizationRole, user?.id]);
  const [result, setResult] = useState<{ stats: DashboardStats; scope: string } | null>(null);
  const stats = result?.scope === scope ? result.stats : null;
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [range, setRange] = useState<TimeRange>('24h');
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [attention, setAttention] = useState<{ data: DashboardAttention; scope: string } | null>(null);
  const [attentionState, setAttentionState] = useState<AttentionLoadState>('idle');
  const requestVersion = useRef(0);
  const attentionRequestVersion = useRef(0);
  const notificationsLicensed = hasLicensedFeature(user?.license_access, 'notifications');

  const fetchData = useCallback(async () => {
    const version = ++requestVersion.current;
    if (customerContext.state !== 'ready' || !selectedOrgID) {
      setResult(null);
      setLoading(false);
      setLoadError(false);
      return;
    }
    setLoading(true);
    setLoadError(false);
    try {
      const now = new Date();
      const start = new Date(now.getTime() - days[range] * 86400000).toISOString();
      const data = await dashboardApi.getStats(start, now.toISOString(), platform ? selectedOrgID : undefined);
      if (version !== requestVersion.current) return;
      setResult({ stats: data, scope });
      setLastUpdated(new Date());
    } catch (error) {
      if (version !== requestVersion.current) return;
      console.error('Failed to load dashboard stats', error);
      setResult(null);
      setLoadError(true);
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  }, [customerContext.state, platform, range, selectedOrgID, scope]);

  const fetchAttention = useCallback(async () => {
    const version = ++attentionRequestVersion.current;
    setAttention(null);
    if (customerContext.state !== 'ready' || !selectedOrgID || !platform) {
      setAttentionState('idle');
      return;
    }
    if (!notificationsLicensed) {
      setAttentionState('locked');
      return;
    }
    setAttentionState('loading');
    try {
      const data = await api.getInternalStatus({ org_id: selectedOrgID });
      if (!data) throw new Error('Notification summary missing');
      if (version !== attentionRequestVersion.current) return;
      setAttention({ data, scope });
      setAttentionState('ready');
    } catch {
      if (version !== attentionRequestVersion.current) return;
      setAttentionState('error');
    }
  }, [customerContext.state, notificationsLicensed, platform, selectedOrgID, scope]);

  useEffect(() => { void fetchData(); return () => { requestVersion.current++; }; }, [fetchData]);
  useEffect(() => { void fetchAttention(); return () => { attentionRequestVersion.current++; }; }, [fetchAttention]);
  const refresh = () => { void fetchData(); void fetchAttention(); };
  const title = t(platform ? '客户运营总览' : isBillingRole(organizationRole) ? '财务与用量总览' : isOrgViewerRole(organizationRole) ? '组织只读总览' : '组织运营总览');
  // Only the synthetic operator label is translated. Customer names are facts.
  const syntheticScope = user?.identity_scope === 'deployment_operator_and_customer' && platform && user.org_id === selectedOrgID;
  const scopeName = syntheticScope ? t('当前部署') : customerContext.orgName || (selectedOrgID ? t('组织 #{id}', { id: selectedOrgID }) : t('未确认客户组织'));

  if (customerContext.state !== 'ready') {
    const blocker = customerContextBlockerCopy(customerContext.state);
    return <div className="ur-page ur-page-wide">
      <PageHeader eyebrow={t('概览')} title={title} description={t('本页只展示当前部署唯一客户组织的运行与用量数据。')} />
      <Surface><EmptyState title={translateLabel(blocker.title)} description={translateLabel(blocker.description)} minHeight={280} /></Surface>
    </div>;
  }

  return <div className="ur-page ur-page-wide ur-overview" aria-busy={loading}>
    <PageHeader eyebrow={t('概览')} title={title} description={t('统一观察模型使用、运行质量与请求费用。')} actions={<>
      <div className="ur-range-selector" role="group" aria-label={t('时间范围')}>
        {(['24h', '7d', '30d'] as const).map(value => <button type="button" key={value} aria-pressed={range === value} onClick={() => setRange(value)}>{t(value === '24h' ? '最近 24 小时' : value === '7d' ? '最近 7 天' : '最近 30 天')}</button>)}
      </div>
      <Button icon={<IconRefresh />} aria-label={t('刷新总览')} onClick={refresh} disabled={loading} />
    </>} />
    <div className="ur-overview-scope"><span>{t('数据范围：{name}', { name: scopeName })}</span>{lastUpdated && stats && <span>{t('更新时间')} {lastUpdated.toLocaleTimeString(locale)}</span>}</div>
    {loadError ? <Surface><EmptyState title={t('总览数据加载失败')} description={t('无法读取当前权限范围的总览数据。请检查网络或服务状态后重试。')} actions={<Button onClick={refresh} aria-label={t('重新加载总览')}>{t('重新加载')}</Button>} /></Surface>
      : <Spin spinning={loading} size="large"><div className="ur-overview-loading">{stats && <OverviewWorkspace key={scope} stats={stats} attention={attention?.scope === scope ? attention.data : null} attentionState={attentionState} />}</div></Spin>}
  </div>;
}
