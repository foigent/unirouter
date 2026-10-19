import '../i18n/integrations';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Banner, Button, InputNumber, Switch, Typography } from '@douyinfe/semi-ui';
import { translate as t } from '../i18n';
import { ApiError } from '../api/transport';
import { xInsightCommunicationApi, type CommunicationState } from '../api/xInsightCommunication';

type Props = { projectId: number; expansionEnabled: boolean };
export default function XInsightCommunicationControls(props: Props) {
  // Project changes remount all local state, retry identities and in-flight UI ownership.
  return <XInsightCommunicationPanel key={props.projectId} {...props} />;
}
function XInsightCommunicationPanel({ projectId, expansionEnabled }: Props) {
  const [state, setState] = useState<CommunicationState>();
  const [cadence, setCadence] = useState(60);
  const [error, setError] = useState('');
  const [unavailable, setUnavailable] = useState(false);
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const retry = useRef<{ key: string; state: CommunicationState }>();
  const refresh = useCallback(async () => {
    if (!alive.current) return;
    try { const next = await xInsightCommunicationApi.get(projectId); if (!alive.current) return; setState(next); setCadence(next.cadence_ms / 1000); setUnavailable(false); setError(''); }
    catch (e) { if (!alive.current) return; if (e instanceof ApiError && [404, 405].includes(e.status)) { setUnavailable(true); setState(undefined); } else setError(e instanceof Error ? e.message : t('无法读取通讯状态')); }
  }, [projectId]);
  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => {
    if (!state || !['running', 'manual_running', 'pausing', 'completion_unknown'].includes(state.effective)) return;
    // Reads UR's local projection only. No XI status probes or automatic resume.
    const timer = window.setInterval(() => { void refresh(); }, 5000);
    return () => window.clearInterval(timer);
  }, [state?.effective, refresh]);
  async function update(automatic: boolean) {
    if (!state) return; setSaving(true); setError('');
    try { setState(await xInsightCommunicationApi.update(projectId, state, automatic, Math.round(cadence * 1000))); }
    catch (e) { setError(e instanceof Error ? e.message : t('通讯设置未确认，请刷新状态')); }
    finally { setSaving(false); }
  }
  async function once() {
    if (!state || (state.automatic_enabled && !retry.current)) return; setRunning(true); setError('');
    const attempt = retry.current || { key: crypto.randomUUID(), state }; retry.current = attempt;
    try { const result = await xInsightCommunicationApi.once(projectId, attempt.state, attempt.key); if (result.state !== 'running') retry.current = undefined; await refresh(); }
    catch (e) { setError(e instanceof Error ? e.message : t('执行结果未确认，请重试同一请求')); }
    finally { setRunning(false); }
  }
  const active = state && ['running', 'manual_running', 'pausing', 'completion_unknown'].includes(state.effective);
  const labels = { runtime_disabled: t('服务器通讯运行器未启用'), paused: t('已停止自动通讯'), automatic: t('自动通讯已启用'), running: t('自动通讯执行中'), manual_running: t('正在执行一次'), pausing: t('正在停止，等待在途操作完成'), completion_unknown: t('完成情况未知，禁止开始新操作') };
  return <section aria-label={t('UR 出站通讯')} style={{ display: 'grid', gap: 10 }}>
    <Typography.Title heading={5}>{t('UR 出站通讯')}</Typography.Title>
    <Typography.Text>{t('此开关只控制 UR 发出的通讯。XI 需在其本地页面单独暂停；Feed 可用性保持独立。')}</Typography.Text>
    {unavailable && <Banner type="info" description={t('尚无连接，或此服务器不支持通讯控制。请完成连接或升级后刷新；不能据此认定双方已停止。')} />}
    {error && <Banner type="warning" description={error} />}
    {state && <>
      <Typography.Text>{labels[state.effective]}</Typography.Text>
      <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
        <Switch aria-label={t('自动通讯')} checked={state.automatic_enabled} disabled={saving || (!state.automatic_enabled && !expansionEnabled)} onChange={value => { void update(value); }} />
        <Typography.Text>{t('自动通讯')}</Typography.Text>
        <InputNumber aria-label={t('自动通讯间隔（秒）')} min={0.25} max={3600} step={1} value={cadence} onChange={value => setCadence(Number(value))} />
        <Button disabled={saving || (!expansionEnabled && state.automatic_enabled)} onClick={() => { void update(state.automatic_enabled); }}>{t('保存间隔')}</Button>
        <Button loading={running} disabled={!expansionEnabled || state.effective === 'runtime_disabled' || (state.automatic_enabled && !retry.current) || (Boolean(active) && !retry.current)} onClick={() => { void once(); }}>{retry.current ? t('重读或重试原请求') : t('执行一次')}</Button>
        {active && <Button disabled={saving} onClick={() => { void update(false); }}>{t('完成当前操作后停止')}</Button>}
      </div>
      <Typography.Text>{t('执行一次最多 30 秒、8 个操作单元、24 次请求；不会保持后台通讯。结果不代表 XI 数据已追平。')}</Typography.Text>
      <Typography.Text>{t('最近成功的控制通讯')}: {state.last_success_at || t('未知')} · {t('对端状态')}: {state.peer_observed_at ? `${t('仅上次观测')} ${state.peer_observed_at}` : t('未知')}</Typography.Text>
      {state.last_run && <Typography.Text>{t('最近一次执行')}: {state.last_run.state} · {state.last_run.units}/8 · {state.last_run.requests}/24</Typography.Text>}
    </>}
    <Button onClick={() => { void refresh(); }}>{t('刷新本地通讯状态')}</Button>
  </section>;
}
