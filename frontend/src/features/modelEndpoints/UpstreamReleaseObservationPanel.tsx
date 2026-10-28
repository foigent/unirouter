import '../../i18n/upstreamRelease';
import { translate as t, useI18n } from '../../i18n';
import { useEffect, useRef, useState } from 'react';
import { Banner, Button, Input, Space, Typography } from '@douyinfe/semi-ui';
import { ApiError } from '../../api/transport';
import { modelDeploymentApi, type CanonicalModelApplication, type CanonicalOperation, type UpstreamReleaseState } from '../../api/modelDeployments';
import type { MessageKey } from '../../i18n/catalog';
import { useAuthStore } from '../../store/auth';
import { upstreamScopeLabel } from './presentation';
import { Surface } from '../../ui';
import { formatDateTime } from '../../utils/formatDate';

const { Text } = Typography;
type Props = { selected: CanonicalModelApplication; operation: CanonicalOperation; canManage: boolean; onChanged?: () => void | Promise<void> };
const empty: UpstreamReleaseState = { observation: null, revision: 0 };

export default function UpstreamReleaseObservationPanel({ selected, operation, canManage, onChanged }: Props) {
  useI18n();
  const initial = selected.deployment.upstream_release_observation || empty;
  const [state, setState] = useState(initial), [editing, setEditing] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const [label, setLabel] = useState(''), [date, setDate] = useState(''), [url, setUrl] = useState(''), [revision, setRevision] = useState(''), [sha, setSha] = useState('');
  const deploymentID = selected.deployment.id;
  const user = useAuthStore(s => s.user);
  const authenticated = useAuthStore(s => s.isAuthenticated);
  const target = JSON.stringify([deploymentID, selected.deployment.config_revision, selected.connection.id, selected.connection.config_revision, selected.connection.provider, selected.connection.base_url, selected.deployment.upstream_model, user?.id, user?.org_id, user?.organization_role, user?.role, user?.customer_context, authenticated]);
  const draftTarget = useRef('');
  const generation = useRef(0);
  useEffect(() => { generation.current++; setState(initial); setEditing(false); setBusy(false); setError(''); }, [target, initial.revision]);
  useEffect(() => () => { generation.current++; }, []);
  const observation = state.observation;
  const staleOrigin = Boolean(observation && (observation.scope.connection_id !== selected.connection.id || observation.scope.provider !== selected.connection.provider || observation.scope.base_url !== selected.connection.base_url || observation.scope.api_model !== selected.deployment.upstream_model));
  const begin = () => {
    draftTarget.current = target;
    setLabel(observation?.claim.release_label || ''); setDate(observation?.claim.announced_on || ''); setUrl(observation?.provenance.source_url || '');
    setRevision(observation?.provenance.source_revision || ''); setSha(observation?.provenance.source_sha256 || ''); setError(''); setEditing(true);
  };
  const refresh = async () => {
    const stamp = generation.current; setBusy(true);
    try { const current = await modelDeploymentApi.getUpstreamRelease(deploymentID); if (stamp !== generation.current) return; setState(current); setEditing(false); setError(''); await onChanged?.(); }
    catch { if (stamp === generation.current) setError(t('声明刷新失败')); }
    finally { if (stamp === generation.current) setBusy(false); }
  };
  const save = async () => {
    if (draftTarget.current !== target || !canManage || selected.deployment.admin_state === 'retired' || busy) return;
    if (!label.trim() || !url.trim()) { setError(t('必须填写发布名称和官方来源。')); return; }
    const stamp = generation.current; setBusy(true); setError('');
    try {
      const next = await modelDeploymentApi.putUpstreamRelease(deploymentID, { expected_connection_revision: selected.connection.config_revision, expected_deployment_revision: selected.deployment.config_revision, expected_revision: state.revision, expected_identity: observation?.identity || '', release_label: label.trim(), source_url: url.trim(),
        ...(date ? { announced_on: date } : {}), ...(revision.trim() ? { source_revision: revision.trim() } : {}), ...(sha.trim() ? { source_sha256: sha.trim() } : {}) });
      if (stamp !== generation.current) return; setState(next); setEditing(false); await onChanged?.();
    } catch (e) { if (stamp === generation.current) setError(e instanceof ApiError && [400,403,404,409].includes(e.status) ? t('声明未保存，请检查输入或刷新端点后重新核对。') : t('声明保存未确认；请刷新后检查，再决定是否重新提交。')); }
    finally { if (stamp === generation.current) setBusy(false); }
  };
  const field = (name: MessageKey, value: string, update: (v: string) => void, maxLength: number, type: 'text' | 'date' = 'text') => <label key={name} style={{ display: 'block', marginTop: 8 }}>
    <Text>{t(name)}</Text><Input aria-label={t(name)} value={value} onChange={update} maxLength={maxLength} type={type} disabled={busy} />
  </label>;
  return <Surface title={t('模型身份与证据范围')}>
    <Space vertical align="start" style={{ width: '100%' }}>
      <Text strong>{t('API 模型别名')}</Text><Text code>{selected.deployment.upstream_model}</Text>
      <Text strong>{t('供应商发布声明')}</Text>
      <Text>{observation?.claim.release_label || t('尚未记录；上游版本未知')}{observation?.claim.announced_on ? ` · ${observation.claim.announced_on}` : ''}</Text>
      <Text type="tertiary">{t('声明由维护者记录；远端权重未固定。')}</Text>
      {observation && <><a href={observation.provenance.source_url} target="_blank" rel="noreferrer">{t('来源')}</a><Text>{t('检查时间')} · {formatDateTime(observation.checked_at)}</Text><Text code>{observation.identity}</Text></>}
      {staleOrigin && <Banner type="warning" description={t('上游端点已改变，需要重新记录声明。')} />}
      <Text strong>{t('UR 执行合同')}</Text>
      <Text code>{operation.execution_profile_id || selected.public_model.execution_profile_id}@{operation.execution_profile_version || selected.public_model.execution_profile_version}</Text>
      {operation.profile_definition_digest && <Text code>{operation.profile_definition_digest}</Text>}
      <Text code>{operation.protocol_profile_id}@{operation.protocol_profile_version}</Text>
      <Text strong>{t('探测证据适用性')}</Text><Text>{upstreamScopeLabel(operation.latest_probe?.upstream_scope?.state)}</Text>
      {error && <Banner type="danger" description={error} />}
      <Space>
        {canManage && selected.deployment.admin_state !== 'retired' && !editing && <Button onClick={begin}>{t(observation ? '修改发布声明' : '记录发布声明')}</Button>}
        <Button onClick={() => { void refresh(); }} disabled={busy}>{t('刷新声明')}</Button>
      </Space>
      {editing && canManage && <div style={{ width: '100%' }}>
        {field('发布名称', label, setLabel, 160)}{field('公布日期（可选）', date, setDate, 10, 'date')}{field('官方来源 HTTPS URL', url, setUrl, 2048)}
        {field('来源版本（可选）', revision, setRevision, 255)}{field('来源 SHA-256（可选）', sha, setSha, 64)}
        <Space style={{ marginTop: 12 }}><Button theme="solid" loading={busy} onClick={() => { void save(); }}>{t('保存声明')}</Button><Button disabled={busy} onClick={() => setEditing(false)}>{t('取消声明编辑')}</Button></Space>
      </div>}
    </Space>
  </Surface>;
}
