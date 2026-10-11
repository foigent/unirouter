import '../i18n/integrations';
import { formatDateTime } from '../utils/formatDate';
import { translate as t, useI18n, labelFor, translateLabel } from '../i18n';
import { useEffect, useLayoutEffect, useState } from 'react';
import { Button, Banner, Spin, Typography } from '@douyinfe/semi-ui';
import { xInsightSetupApi, type CredentialRecoveryPreview, type CredentialRecoveryAccepted, type SetupStatus } from '../api/xInsightSetup';
import { useAuthStore } from '../store/auth';
import { finishXInsightLoginContinuation, isXInsightRecoveryLocation } from '../routing/xInsightLoginContinuation';

const { Title, Text } = Typography;
const credentialLabel: Record<string, string> = {
  healthy: '有效', rotation_due: '即将到期', rotation_pending: '轮换待接续',
  committed_unpromoted: '对端已确认，等待本端接续', expired_recovery_required: '已过期，需人工恢复',
  revoked: '已撤销，不能恢复', credential_unavailable: '暂不可用',
};
function localTime(value: string) {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : formatDateTime(value);
}

export default function XInsightCredentialRecoveryPage() {
  useI18n();
  const user = useAuthStore(s => s.user);
  const [fragment] = useState(() => typeof window === 'undefined' ? '' : window.location.hash);
  const fields = new URLSearchParams(fragment.replace(/^#/, ''));
  const valid = isXInsightRecoveryLocation('/connectors/x-insight/recover', '', fragment);
  const installation = fields.get('installation_id') || '';
  const recoveryId = fields.get('recovery_id') || '';
  const xiOrigin = fields.get('xinsight_origin') || '';
  const ticket = fields.get('recovery_ticket') || '';
  const [preview, setPreview] = useState<CredentialRecoveryPreview>();
  const [accepted, setAccepted] = useState<CredentialRecoveryAccepted>();
  const [status, setStatus] = useState<SetupStatus>();
  const [org, setOrg] = useState<number>();
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useLayoutEffect(() => {
    if (fragment) window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);
    finishXInsightLoginContinuation(fragment);
  }, [fragment]);

  useEffect(() => {
    setOrg(undefined); setPreview(undefined); setAccepted(undefined); setStatus(undefined); setError('');
    if (!valid || !user?.id) return;
    let active = true;
    xInsightSetupApi.recoveryScope(installation).then(scope => {
      if (!active) return;
      if (scope.installation_id !== installation || !Number.isSafeInteger(scope.org_id) || scope.org_id <= 0) throw new Error(t("原连接组织不一致"));
      setOrg(scope.org_id);
      return xInsightSetupApi.recoveryPreview(scope.org_id, installation, ticket);
    }).then(value => {
      if (!value) return;
      if (!active) return;
      if (value.installation_id !== installation || value.recovery_id !== recoveryId || value.xinsight_origin !== xiOrigin) {
        setError('恢复材料与原连接不一致。请从 X-Insight 重新发起。'); return;
      }
      setPreview(value);
    }).catch(() => { if (active) setError('无法核对原连接、管理员权限或恢复票据。已撤销的连接不能恢复；请从 X-Insight 查看原因并重新发起。'); });
    return () => { active = false; };
  }, [valid, user?.id, installation, ticket, recoveryId, xiOrigin]);

  async function confirm() {
    if (!org || !preview || !confirmed || busy) return;
    setBusy(true); setError('');
    try {
      const value = await xInsightSetupApi.recoveryConfirm(org, installation, ticket);
      if (value.installation_id !== installation || value.state !== 'pending') throw new Error(t("恢复回执不一致"));
      setAccepted(value);
      const next = await xInsightSetupApi.status(org, installation);
      setStatus(next);
    } catch {
      setError('恢复尚未确认。请先重新读取状态；若仍未接续，可用同一页面重试原票据。');
    } finally { setBusy(false); }
  }

  async function refresh() {
    if (!org || !installation) return;
    try { setStatus(await xInsightSetupApi.status(org, installation)); setError(''); }
    catch { setError('当前连接状态不可读，请稍后重试。'); }
  }

  if (!valid) return <Banner type="danger" description={t("恢复链接不完整或格式不正确。请从 X-Insight 原连接页面重新发起。")} />;
  return <section style={{maxWidth: 760, margin: '32px auto', display: 'grid', gap: 16}}>
    <Title heading={3}>{t("恢复原 X-Insight 连接")}</Title>
    {!preview && !error && <Spin tip={t("正在核对原连接与当前管理员权限…")} />}
    {error && <Banner type="warning" description={translateLabel(error)} />}
    {preview && <>
      <p>{t("此操作只更新原连接的控制凭据，沿用原连接的全部授权范围。请核对这是您要恢复的连接。")}</p>
      <div style={{padding: 16, border: '1px solid var(--semi-color-border)', borderRadius: 8}}>
        <p>{t("原 X-Insight 连接（沿用原授权范围）")}</p>
        <p>X-Insight：{preview.xinsight_origin}</p>
        <p>UniRouter：{preview.unirouter_origin}</p>
        <p>{t("本地凭据：")}{labelFor(credentialLabel, preview.credential.state) || t("暂不可用")}</p>
        <p>{t("本次确认期限：")}{localTime(preview.expires_at)}</p>
        <details><summary>{t("技术识别信息")}</summary>
          <p>{t("连接识别码：")}<Text code>{preview.installation_id}</Text></p>
          <p>{t("控制项目：")}{preview.project_id}</p>
          <p>{t("事件类型：")}{preview.final_event_scope.join('、')}</p>
        </details>
      </div>
      {!accepted && <>
        <label><input type="checkbox" checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />  {t("我已核对这是原连接和原范围，确认恢复其控制凭据。")}</label>
        <Button theme="solid" disabled={!confirmed || busy} loading={busy} onClick={() => { void confirm(); }}>{t("确认恢复原连接")}</Button>
      </>}
      {accepted && <p role="status">{t("本端确认已持久保存。后台正在使用原连接接续控制凭据；请读取最新状态。")}</p>}
      {(accepted || error) && <Button onClick={() => { void refresh(); }}>{t("读取连接状态")}</Button>}
      {status?.credential && <p role="status">{t("当前控制凭据：")}{labelFor(credentialLabel, status.credential.state) || t("暂不可用")}{t("。数据新鲜度和正文读取需另按实际接收核对。")}</p>}
    </>}
  </section>;
}
