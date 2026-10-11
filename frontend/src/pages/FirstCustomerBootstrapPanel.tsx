import '../i18n/integrations';
import { translate as t, useI18n, translateLabel } from '../i18n';
import { useRef, useState } from 'react';
import { Banner, Button, Checkbox, Input, Typography } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { Surface } from '../ui';
import { IntentRequestIDs } from '../utils/intentRequestId';

type Preview = {
  contract_version: 'unirouter.first-customer-bootstrap-preview.v1';
  dry_run: true;
  ready: true;
  checkpoint: 'strict_reset' | 'fresh_seed' | 'runtime_reset';
  organization_name: string;
  platform_org_id: number;
  would_remove: { project_id?: number; zero_wallet_org_id?: number };
};

export default function FirstCustomerBootstrapPanel() {
  useI18n();
  const user = useAuthStore(state => state.user);
  const token = useAuthStore(state => state.token);
  // The form owns one actor/session's intent. Keep the secret out of React
  // keys, rendered output and persistence; an opaque generation remounts it.
  const actor = user ? `${user.id}:${user.org_id}:${organizationRoleForUser(user)}` : '';
  const boundary = useRef({ actor, token, generation: 0 });
  if (boundary.current.actor !== actor || boundary.current.token !== token) {
    boundary.current = { actor, token, generation: boundary.current.generation + 1 };
  }
  if (!user || organizationRoleForUser(user) !== 'platform_admin'
    || !Number.isSafeInteger(user.org_id) || Number(user.org_id) <= 0) return null;
  if (user.customer_context?.state === 'ready' && user.customer_context.org_id !== user.org_id) return null;
  return <FirstCustomerBootstrapForm key={boundary.current.generation} platformOrgID={Number(user.org_id)} />;
}

function FirstCustomerBootstrapForm({ platformOrgID }: { platformOrgID: number }) {
  useI18n();
  const loadProfile = useAuthStore(state => state.loadProfile);
  const intents = useRef(new IntentRequestIDs()).current;
  const [name, setName] = useState('');
  const [preview, setPreview] = useState<Preview | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [completed, setCompleted] = useState(false);
  const [error, setError] = useState('');

  const inspect = async () => {
    setBusy(true); setError(''); setPreview(null); setConfirmed(false);
    try {
      const result = await api.bootstrapFirstCustomer(name.trim(), intents.get('first-customer'), { dryRun: true });
      const checkpoint = result?.checkpoint;
      const remove = result?.would_remove;
      const validRemoval = checkpoint === 'strict_reset'
        ? remove && Object.keys(remove).length === 0
        : Number.isSafeInteger(remove?.project_id) && remove.project_id > 0 && remove.zero_wallet_org_id === platformOrgID;
      if (result?.contract_version !== 'unirouter.first-customer-bootstrap-preview.v1'
        || result.dry_run !== true || result.ready !== true || result.organization_name !== name.trim()
        || result.platform_org_id !== platformOrgID
        || !['strict_reset', 'fresh_seed', 'runtime_reset'].includes(checkpoint) || !validRemoval) {
        throw new Error(t("未取得有效的只读预检结果，不能初始化。"));
      }
      setPreview(result as Preview);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "预检失败；未初始化客户。");
    } finally { setBusy(false); }
  };

  const apply = async () => {
    if (!preview || !confirmed || busy || completed) return;
    setBusy(true); setSubmitted(true); setError('');
    try {
      const result = await api.bootstrapFirstCustomer(preview.organization_name, intents.get('first-customer'));
      if (!Number.isSafeInteger(result?.organization_id) || result.organization_id <= 0
        || result.organization_name !== preview.organization_name || result.wallet_balance_micros !== 0) {
        throw new Error(t("初始化结果未能确认；请保留当前页面，用同一请求重试。"));
      }
      setCompleted(true);
      intents.complete('first-customer');
      await loadProfile();
    } catch (reason) {
      // Keep the intent and name: a lost response must not create a new intent.
      setError(reason instanceof Error ? reason.message : "结果未确认，请用同一请求重试。");
    } finally { setBusy(false); }
  };

  return (
    <Surface title={t("首次客户初始化")} description={t("仅用于尚无独立客户的精确空基线。预检不会写入数据；提交时重新核验全部前态，不会切路由或调用供应商。")}>
      <label htmlFor="first-customer-name">{t("测试或客户组织名称")}</label>
      <Input id="first-customer-name" value={name} disabled={busy || submitted || completed} maxLength={160}
        onChange={value => { setName(value); setPreview(null); setConfirmed(false); intents.complete('first-customer'); }} />
      {!submitted && <Button onClick={inspect} loading={busy} disabled={!name.trim()}>{t("只读预检")}</Button>}
      {preview && <>
        <Typography.Paragraph>{t("将新增独立客户“")}{preview.organization_name}{t("”及零余额 CNY 钱包；现有 admin、平台组织、登录审计与 License/Release 保留。")}</Typography.Paragraph>
        {preview.would_remove.project_id ? <Banner type="warning" closeIcon={null} description={t("同一事务将移除自动空项目 #{v0} 和平台组织 #{v1} 的全零钱包。存在金额、成员或业务引用时拒绝。客户默认项目随后由正常启动流程创建。", { v0: String(preview.would_remove.project_id), v1: String(preview.would_remove.zero_wallet_org_id) })} />
          : <Typography.Paragraph>{t("本次没有需要移除的运行时壳对象。")}</Typography.Paragraph>}
        <Checkbox checked={confirmed} disabled={busy || submitted || completed} onChange={event => setConfirmed(Boolean(event.target.checked))}>{t("确认上述精确对象及初始化范围")}</Checkbox>
        <Button type="primary" onClick={apply} loading={busy} disabled={!confirmed || busy || completed}>{submitted ? t("重试同一初始化请求") : t("确认初始化客户")}</Button>
      </>}
      {completed && <Banner type="success" closeIcon={null} description={t("客户初始化已提交；尚未创建供应商连接、执行 probe 或切换 canonical。")} />}
      {error && <Banner type="danger" closeIcon={null} description={translateLabel(error)} />}
    </Surface>
  );
}
