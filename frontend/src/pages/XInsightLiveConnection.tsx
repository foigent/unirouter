import '../i18n/integrations';
import { translate as t, useI18n, translateLabel } from '../i18n';
import { finishXInsightLoginContinuation } from '../routing/xInsightLoginContinuation';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Banner, Button, Descriptions, Input, Popconfirm, Select, Spin, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { canonicalOrganizationRole } from '../navigation';
import { api } from '../api/client';
import type { DeliveredMachineCandidate, ReceiverAdoption } from '../api/xInsightMachine';
import { formatDateTime } from '../utils/formatDate';
import { loadAllPages } from '../utils/loadAllPages';
import { StatusTag } from '../ui';
import { useAuthStore } from '../store/auth';
import { customerOrganizationDisplayName } from '../utils/customerContext';
import { projectDisplayName } from '../utils/projectDisplay';
import XInsightSetup from './XInsightSetup';
import XInsightCommunicationControls from './XInsightCommunicationControls';
import { xInsightCommunicationApi } from '../api/xInsightCommunication';
import XInsightSourceConnection from './XInsightSourceConnection';
import XInsightContentManagement from './XInsightContentManagement';
import XInsightMachineDelegationManagement from './XInsightMachineDelegationManagement';

const { Text, Title } = Typography;

interface Project {
  id: number;
  org_id?: number;
  name: string;
  slug?: string;
  org_name?: string;
  status: 'active' | 'disabled' | 'archived';
}

interface FeedCredential {
  id: number;
  key_prefix: string;
  lifecycle: 'active' | 'staged' | 'revoked';
  verifier_scheme: 'legacy_hmac_sha256_v1' | 'receiver_sha256_v1';
  operation_id: string;
  created_at: string;
  revoked_at?: string;
}

interface FeedConnectionStatus {
  project_id: number;
  project_name: string;
  project_status: 'active' | 'disabled' | 'archived';
  global_configured: boolean;
  global_enabled: boolean;
  source_instance: string;
  connection_status: 'not_configured' | 'paused' | 'active';
  active_credential?: FeedCredential | null;
  staged_credential?: FeedCredential | null;
  last_revoked_credential?: FeedCredential | null;
  last_authenticated_pull_at?: string | null;
  last_authenticated_credential?: FeedCredential | null;
  source_high_watermark: string;
  receiver_acknowledged_cursor: string;
  receiver_acknowledgement_semantics: string;
  pending_item_count: number;
  pending_item_count_capped: boolean;
  oldest_pending_at?: string | null;
  oldest_pending_age_seconds: number;
  cursor_outside_retention: boolean;
  connector_product_status: 'not_connected' | 'connecting' | 'connected' | 'paused' | 'abnormal';
  connector_installation?: {
    delivered_machine_candidates?: DeliveredMachineCandidate[];
    receiver_adoption?: ReceiverAdoption;
    schema_version: 'unirouter.xinsight.connector-installation.v1';
    installation_id: string;
    x_deployment_id: string;
    event_scope: 'model.request.completed';
    source_instance: string;
    source_binding_sha256: string;
    desired_state: 'active' | 'paused' | 'disconnected';
    observed_state: 'provisioning' | 'connected' | 'paused' | 'abnormal' | 'disconnected';
    state_revision: number;
    control_fence: number;
    last_successful_sync_at?: string;
    last_error_code?: string;
    last_error_at?: string;
    consented_at: string;
    updated_at: string;
  } | null;
  connector_latest_operation?: {
    schema_version: 'unirouter.xinsight.connector-operation.v1';
    operation_ref: string;
    installation_id: string;
    action: 'connect' | 'pause' | 'resume' | 'rotate' | 'replay';
    desired_state: 'active' | 'paused';
    state: 'queued' | 'claimed' | 'retry_wait' | 'succeeded' | 'failed' | 'cancelled';
    requested_revision: number;
    control_fence: number;
    attempt_count: number;
    result_code?: string;
    error_code?: string;
    completed_at?: string;
    created_at: string;
    updated_at: string;
  } | null;
}

export interface ReceiverFeedCredentialRegistrationV1 {
  schema_version: 'unirouter.xinsight.feed-credential-registration.v1';
  token_format: 'ur-xf1-hex256.v1';
  verifier_scheme: 'receiver_sha256_v1';
  key_prefix: string;
  verifier_sha256: string;
  operation_id: string;
}

export interface FrozenConnectorInvitationV1 {
  schema_version: 'xinsight.connector-installation-invitation.v1';
  invitation_id: string;
  installation_id: string;
  deployment_id: string;
  audience: 'unirouter';
  xinsight_origin: string;
  requested_event_scope: ['model.request.completed'];
  expires_at: string;
  install_url: string;
}

interface RegisteredCredential {
  id: number;
  key_prefix: string;
  lifecycle: 'staged';
  project_id: number;
  purpose: 'xinsight.feed.read';
  key_type: 'service';
  verifier_scheme: 'receiver_sha256_v1';
  operation_id: string;
}

const registrationKeys = [
  'schema_version',
  'token_format',
  'verifier_scheme',
  'key_prefix',
  'verifier_sha256',
  'operation_id',
] as const;

const invitationKeys = [
  'schema_version',
  'invitation_id',
  'installation_id',
  'deployment_id',
  'audience',
  'xinsight_origin',
  'requested_event_scope',
  'expires_at',
  'install_url',
] as const;

export function parseReceiverRegistrationBundle(raw: string): ReceiverFeedCredentialRegistrationV1 {
  const parsed = JSON.parse(raw) as Record<string, unknown>;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error(t("registration 必须是 JSON object"));
  }
  const keys = Object.keys(parsed).sort();
  if (keys.length !== registrationKeys.length ||
      keys.some((key, index) => key !== [...registrationKeys].sort()[index])) {
    throw new Error(t("registration 字段必须与 Frozen Registration Bundle V1 完全一致"));
  }
  if (parsed.schema_version !== 'unirouter.xinsight.feed-credential-registration.v1' ||
      parsed.token_format !== 'ur-xf1-hex256.v1' ||
      parsed.verifier_scheme !== 'receiver_sha256_v1' ||
      typeof parsed.key_prefix !== 'string' ||
      !/^ur-xf1-[0-9a-f]{13}$/.test(parsed.key_prefix) ||
      typeof parsed.verifier_sha256 !== 'string' ||
      !/^[0-9a-f]{64}$/.test(parsed.verifier_sha256) ||
      typeof parsed.operation_id !== 'string' ||
      !/^[0-9a-f]{32}$/.test(parsed.operation_id)) {
    throw new Error(t("registration 不符合 Frozen Registration Bundle V1"));
  }
  return parsed as unknown as ReceiverFeedCredentialRegistrationV1;
}

export function parseFrozenConnectorInvitation(
  raw: string,
  now = new Date(),
  expectedFragment = '',
): FrozenConnectorInvitationV1 {
  const parsed = JSON.parse(raw) as Record<string, unknown>;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error(t("invitation 必须是 JSON object"));
  }
  const keys = Object.keys(parsed).sort();
  if (keys.length !== invitationKeys.length ||
      keys.some((key, index) => key !== [...invitationKeys].sort()[index])) {
    throw new Error(t("invitation 字段必须与 frozen Connector invitation 完全一致"));
  }
  if (parsed.schema_version !== 'xinsight.connector-installation-invitation.v1' ||
      typeof parsed.invitation_id !== 'string' || !/^xii_[A-Za-z0-9_-]{22}$/.test(parsed.invitation_id) ||
      typeof parsed.installation_id !== 'string' || !/^xci_[A-Za-z0-9_-]{22}$/.test(parsed.installation_id) ||
      typeof parsed.deployment_id !== 'string' || !/^xid_[A-Za-z0-9_-]{22}$/.test(parsed.deployment_id) ||
      parsed.audience !== 'unirouter' ||
      typeof parsed.xinsight_origin !== 'string' || !isCanonicalHTTPSOrigin(parsed.xinsight_origin) ||
      !Array.isArray(parsed.requested_event_scope) || parsed.requested_event_scope.length !== 1 ||
      parsed.requested_event_scope[0] !== 'model.request.completed' ||
      typeof parsed.expires_at !== 'string' ||
      !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(parsed.expires_at) ||
      typeof parsed.install_url !== 'string') {
    throw new Error(t("invitation 不符合 frozen Connector invitation"));
  }
  const expiresAt = Date.parse(parsed.expires_at);
  if (!Number.isFinite(expiresAt) || expiresAt <= now.getTime() || expiresAt > now.getTime() + 30 * 60 * 1000) {
    throw new Error(t("invitation 已过期或超过 frozen 30 分钟有效期"));
  }
  const installURL = new URL(parsed.install_url);
  const fragment = new URLSearchParams(installURL.hash.slice(1));
  const fragmentKeys = [...fragment.keys()];
  const token = fragment.get('invitation') || '';
  const xInsightOrigin = fragment.get('xinsight_origin') || '';
  if (installURL.protocol !== 'https:' || installURL.username || installURL.password ||
      installURL.pathname !== '/connectors/x-insight/install' || installURL.search ||
      fragmentKeys.length !== 2 || fragment.getAll('invitation').length !== 1 ||
      fragment.getAll('xinsight_origin').length !== 1 ||
      !/^xiiv1_[A-Za-z0-9_-]{43}$/.test(token) ||
      xInsightOrigin !== parsed.xinsight_origin ||
      parsed.install_url !== `${installURL.origin}/connectors/x-insight/install#${fragment.toString()}` ||
      (expectedFragment !== '' && installURL.hash !== expectedFragment)) {
    throw new Error(t("install_url 不符合 frozen Connector invitation"));
  }
  return parsed as unknown as FrozenConnectorInvitationV1;
}

function isCanonicalHTTPSOrigin(value: string) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' && !parsed.username && !parsed.password &&
      parsed.origin === value && parsed.pathname === '/' && !parsed.search && !parsed.hash;
  } catch {
    return false;
  }
}

function credentialLabel(credential?: FeedCredential | null) {
  if (!credential) return '-';
  return `#${credential.id} · ${credential.key_prefix} · ${credential.lifecycle}`;
}

function connectorProductLabel(status?: FeedConnectionStatus['connector_product_status']) {
  switch (status) {
    case 'connected': return t("已连接");
    case 'abnormal': return t("异常");
    case 'paused': return t("已暂停");
    case 'connecting': return t("连接中");
    default: return t("未连接");
  }
}

function feedConnectionLabel(status?: FeedConnectionStatus['connection_status']) {
  switch (status) {
    case 'active': return t("读取已启用");
    case 'paused': return t("已暂停");
    default: return t("未配置");
  }
}

function boundedBacklogLabel(status: FeedConnectionStatus | null) {
  if (!status) return '0';
  return `${status.pending_item_count}${status.pending_item_count_capped ? '+' : ''}`;
}

interface XInsightLiveConnectionProps {
  expansionAllowed?: boolean;
  expansionRestriction?: string;
}

export default function XInsightLiveConnection({
  expansionAllowed = true,
  expansionRestriction = '',
}: XInsightLiveConnectionProps) {
  const { locale } = useI18n();
  const user = useAuthStore((state) => state.user);
  const normalOnly = canonicalOrganizationRole(user?.organization_role, user?.role) === 'org_admin';
  const projectCacheKey = `xinsight.setup-project:${user?.id}:${user?.org_id}`;
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState<number>();
  const [communicationRevision, setCommunicationRevision] = useState(0);
  const [status, setStatus] = useState<FeedConnectionStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [allowedIP, setAllowedIP] = useState('');
  const [registrationJSON, setRegistrationJSON] = useState('');
  const [invitationJSON, setInvitationJSON] = useState('');
  const [projectLoadError, setProjectLoadError] = useState<{ detail: string } | null>(null);
  const [statusLoadError, setStatusLoadError] = useState('');

  const loadProjects = useCallback(async () => {
    setProjectLoadError(null);
    try {
      const next = await loadAllPages<Project>((page, pageSize) => api.getProjects(page, pageSize));
      setProjects(next);
      const saved=Number(sessionStorage.getItem(projectCacheKey));
 setProjectID(next.find(p=>p.id===saved)?.id || next[0]?.id);
    } catch (error: any) {
      setProjects([]);
      setProjectLoadError({ detail: error?.message || '' });
    }
  }, [projectCacheKey]);

  const loadStatus = useCallback(async () => {
    if (!projectID || normalOnly) {
      setStatus(null);
      return;
    }
    setLoading(true);
    setStatusLoadError('');
    try {
      setStatus(await api.getXInsightFeedConnection(projectID) as FeedConnectionStatus);
    } catch (error: any) {
      setStatusLoadError(error?.message || "无法读取 X-Insight 连接状态");
      setStatus(null);
    } finally {
      setLoading(false);
    }
  }, [projectID, normalOnly]);

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadStatus(); }, [loadStatus]);

  const mutate = useCallback(async (operation: () => Promise<any>, success: string) => {
    setLoading(true);
    try {
      await operation();
      Toast.success(success);
      await loadStatus();
    } catch (error: any) {
      Toast.error(t("{v0}失败{v1}", { v0: String(success), v1: String(error?.message ? `：${error.message}` : '') }));
    } finally {
      setLoading(false);
    }
  }, [loadStatus]);

  const createCredential = async () => {
    if (!projectID) return;
    setLoading(true);
    try {
      const registration = parseReceiverRegistrationBundle(registrationJSON);
      const created = await api.createXInsightFeedCredential(
        projectID,
        { allowed_ip: allowedIP, registration },
      ) as RegisteredCredential;
      setRegistrationJSON('');
      Toast.success(t("已登记 staged feed-only 凭证 #{v0}（{v1}）；连接仍保持暂停", { v0: String(created.id), v1: String(created.key_prefix) }));
      await loadStatus();
    } catch (error: any) {
      Toast.error(t("创建失败{v0}", { v0: String(error?.message ? `：${error.message}` : '') }));
    } finally {
      setLoading(false);
    }
  };

  const prepareRotation = async () => {
    if (!projectID) return;
    setLoading(true);
    try {
      const registration = parseReceiverRegistrationBundle(registrationJSON);
      const created = await api.prepareXInsightFeedRotation(projectID, { registration }) as RegisteredCredential;
      setRegistrationJSON('');
      Toast.success(t("轮换已准备（#{v0} · {v1}）；旧凭证保持 active，新凭证仅在暂停态可认证", { v0: String(created.id), v1: String(created.key_prefix) }));
      await loadStatus();
    } catch (error: any) {
      Toast.error(t("准备轮换失败{v0}", { v0: String(error?.message ? `：${error.message}` : '') }));
    } finally {
      setLoading(false);
    }
  };

  const installConnector = async () => {
    if (!projectID) return;
    setLoading(true);
    try {
      const invitation = parseFrozenConnectorInvitation(
        invitationJSON,
        new Date(),
        window.location.hash,
      );
      await api.installXInsightConnector(projectID, { invitation, ...(status?.connection_status === 'active' ? { receiver_mode: 'adopt_existing' as const } : {}) });
      try {
        const communication = await xInsightCommunicationApi.get(projectID);
        if (!communication.automatic_enabled) {
          const initialization = await xInsightCommunicationApi.once(projectID, communication, `install:${invitation.invitation_id}`);
          if (!['completed', 'empty'].includes(initialization.state)) Toast.warning(t('连接已保存；初始化通讯尚未完成，请在通讯设置中执行一次。'));
        }
      } catch { Toast.warning(t('连接已保存；初始化通讯尚未完成，请在通讯设置中执行一次。')); }
      setCommunicationRevision(value=>value+1);
      setInvitationJSON('');
      if (window.location.hash) {
        window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);
      }
      Toast.success(status?.connection_status === 'active' ? t("接管申请已登记；原连接、凭据和同步进度保留，等待接管验证完成") : t("Connector invitation 已登记，等待连接验证完成"));
      await loadStatus();
    } catch (error: any) {
      Toast.error(t("Connector invitation 登记失败{v0}", { v0: String(error?.message ? `：${error.message}` : '') }));
    } finally {
      setLoading(false);
    }
  };

  const projectOptions = useMemo(() => projects.map(project => ({
    value: project.id,
    label: `${projectDisplayName(project)}${project.org_name || project.org_id ? ` · ${customerOrganizationDisplayName(user, project.org_id, project.org_name)}` : ''}${
      project.status === 'active' ? '' : project.status === 'archived' ? t(" · 已归档") : t(" · 已停用")
    }`,
  })), [projects, user, locale]);
  const selectedProject = useMemo(
    () => projects.find(project => project.id === projectID),
    [projectID, projects],
  );
  const projectStatus = status?.project_status || selectedProject?.status;
  const projectActive = projectStatus === 'active';
  const expansionEnabled = projectActive && expansionAllowed;
  const registrationReady = useMemo(() => {
    try {
      parseReceiverRegistrationBundle(registrationJSON);
      return true;
    } catch {
      return false;
    }
  }, [registrationJSON]);
  const [linkedInvitationFragment] = useState(() => typeof window === 'undefined' ? '' : window.location.hash);
  useEffect(() => { finishXInsightLoginContinuation(linkedInvitationFragment); }, [linkedInvitationFragment]);
  const parsedInvitation = useMemo(() => {
    try {
      return parseFrozenConnectorInvitation(invitationJSON, new Date(), linkedInvitationFragment);
    } catch {
      return null;
    }
  }, [invitationJSON, linkedInvitationFragment]);
  const connectorIntakeEnabled = expansionEnabled && Boolean(status?.global_enabled && status?.source_instance);

  if (!projectID && projects.length === 0) {
    if (projectLoadError) return <Banner type="warning" description={t('无法读取可用于 X-Insight 的项目{v0}', { v0: projectLoadError.detail ? `: ${projectLoadError.detail}` : '' })} />;
    return <Spin tip={t("正在读取可用项目…")} />;
  }

  if (normalOnly) return <div className="ur-integration-workspace" style={{display:'grid',gap:18}}>
    <Select aria-label={t("实时连接项目")} value={projectID} optionList={projectOptions} onChange={value=>setProjectID(Number(value))} style={{width:320}} />
    {projectID && <XInsightCommunicationControls key={`communication-${projectID}-${communicationRevision}`} projectId={projectID} expansionEnabled={expansionEnabled} />}
    {expansionRestriction && <Banner type="warning" description={expansionRestriction} />}
    {projectID && selectedProject?.org_id && user && <XInsightSetup key={`setup-${user.id}-${selectedProject.org_id}-${projectID}`} actorId={user.id} actorOrgId={user.org_id} orgId={selectedProject.org_id} projectId={projectID} projectName={selectedProject.name} projects={projects} invitationFragment={linkedInvitationFragment} enabled={expansionEnabled} onSaved={()=>setCommunicationRevision(value=>value+1)} />}
  </div>;

  return (
    <div className="ur-integration-workspace" style={{ display: 'grid', gap: 18 }}>
      <div className="ur-toolbar">
        <Select
          aria-label={t("实时连接项目")}
          value={projectID}
          optionList={projectOptions}
          onChange={value => setProjectID(Number(value))}
          style={{ width: 320 }}
        />
        <div style={{ flex: 1 }} />
        <Button icon={<IconRefresh />} onClick={loadStatus} loading={loading}>{t("刷新状态")}</Button>
      </div>

      {projectID && <XInsightCommunicationControls key={`communication-${projectID}-${communicationRevision}`} projectId={projectID} expansionEnabled={expansionEnabled} />}
      {statusLoadError && <Banner
        type="warning"
        description={t("当前项目尚不能读取 X-Insight 连接状态：{v0}。这只影响 X-Insight 连接管理，不影响模型网关、其他集成或本页目录。请确认该 Connector 已配置且 License 包含 Data Exchange 后再重试。", { v0: translateLabel(statusLoadError) })}
        closeIcon={null}
      />}

      {projectID && selectedProject?.org_id && <XInsightSetup key={`setup-${user?.id}-${selectedProject.org_id}-${projectID}`} actorId={user?.id} actorOrgId={user?.org_id} orgId={selectedProject.org_id} projectId={projectID} projectName={selectedProject.name} projects={projects} invitationFragment={linkedInvitationFragment} installationId={status?.connector_installation?.installation_id} enabled={connectorIntakeEnabled} onSaved={()=>{setInvitationJSON('');void loadStatus();}} />}
      <details><summary>{t("维护与技术详情")}</summary>
      <div className="ur-chip-row">
        <Tag color="green">metadata only</Tag>
        <Tag color="blue">project-bound</Tag>
        <Tag>purpose=xinsight.feed.read</Tag>
        <Tag>{t("无模型调用权限")}</Tag>
        <Tag>{t("不向 X-Insight 提供管理员凭证")}</Tag>
        <Tag color="purple">receiver-generated credential</Tag>
      </div>

      {status && !status.connector_installation && (
        <div style={{ padding: 14, border: '1px solid var(--semi-color-border)', borderRadius: 8, display: 'grid', gap: 10 }}>
          <Title heading={6} style={{ margin: 0 }}>{t("接受 Connector invitation")}</Title>
          <Text type="tertiary">

            {t("粘贴 X-Insight 返回的完整 frozen invitation JSON。UniRouter 会校验全部字段、固定 HTTPS origin、唯一 scope、有效期和 install_url fragment；token 仅做用途隔离加密持久化，响应不会返回。")}
          </Text>
          {linkedInvitationFragment && (
            <Tag color="blue">{t("已从 invitation link 进入；完整 package 必须与当前 fragment 精确一致")}</Tag>
          )}
          <TextArea
            aria-label="Frozen X-Insight Connector invitation"
            placeholder='{"schema_version":"xinsight.connector-installation-invitation.v1",...}'
            value={invitationJSON}
            onChange={setInvitationJSON}
            rows={9}
            disabled={!connectorIntakeEnabled}
          />
          {parsedInvitation && (
            <Descriptions
              row
              data={[
                { key: 'Installation', value: parsedInvitation.installation_id },
                { key: 'Deployment', value: parsedInvitation.deployment_id },
                { key: 'X-Insight origin', value: parsedInvitation.xinsight_origin },
                { key: t("固定 scope"), value: parsedInvitation.requested_event_scope[0] },
                { key: t("到期"), value: formatDateTime(parsedInvitation.expires_at) },
              ]}
            />
          )}
          <Popconfirm
            title={status.connection_status === 'active' ? t("接管当前项目的现有连接，保留原凭据 {v0} 和同步进度。", { v0: String(status.active_credential?.key_prefix || '') }) : t("确认将 X-Insight 绑定到当前项目，登记邀请与项目范围。")}
            onConfirm={installConnector}
          >
            <Button type="primary" disabled={!parsedInvitation || !connectorIntakeEnabled || loading || (status.connection_status === 'active' && (!status.active_credential || !!status.staged_credential || status.active_credential.verifier_scheme !== 'receiver_sha256_v1' || !status.active_credential.operation_id))}>
              {status.connection_status === 'active' ? t("接管现有连接") : t("确认项目范围并登记 invitation")}
            </Button>
          </Popconfirm>
          {!status.global_enabled || !status.source_instance ? (
            <Text type="warning">{t("需要先启用并固定 completed-execution Source instance；不会生成默认来源身份。")}</Text>
          ) : null}
        </div>
      )}

      <div style={{ padding: 14, border: '1px solid var(--semi-color-border)', borderRadius: 8 }}>
        <Title heading={6} style={{ marginTop: 0 }}>{t("Connector 配对与 Feed 状态")}</Title>
        <Descriptions
          row
          data={[
            {
              key: t("Connector 配对状态"),
              value: <StatusTag status={status?.connector_product_status || 'not_connected'}>
                {connectorProductLabel(status?.connector_product_status)}
              </StatusTag>,
            },
            {
              key: t("Feed 实际状态"),
              value: <StatusTag status={status?.connection_status || 'not_configured'}>
                {feedConnectionLabel(status?.connection_status)}
              </StatusTag>,
            },
            { key: t("Feed 当前凭据"), value: credentialLabel(status?.active_credential) },
            {
              key: t("Connector 最近同步"),
              value: status?.connector_installation?.last_successful_sync_at
                ? formatDateTime(status.connector_installation.last_successful_sync_at)
                : '-',
            },
            { key: t("Feed 最近认证拉取"), value: status?.last_authenticated_pull_at ? formatDateTime(status.last_authenticated_pull_at) : '-' },
            { key: t("待同步 backlog"), value: boundedBacklogLabel(status) },
            {
              key: t("最旧待同步延迟"),
              value: status?.oldest_pending_at
                ? t("{v0} 秒 · {v1}", { v0: String(status.oldest_pending_age_seconds), v1: String(formatDateTime(status.oldest_pending_at)) })
                : '-',
            },
            {
              key: t("最近控制动作"),
              value: status?.connector_latest_operation
                ? `${status.connector_latest_operation.action} · ${status.connector_latest_operation.state}`
                : '-',
            },
          ]}
        />
        {status?.connector_installation?.receiver_adoption && <Text>

          {t("接管现有连接：")}{status.connector_installation.receiver_adoption.status === 'confirmed'
            ? t("已完成双方接管与原链读取确认；当前健康以上方实时状态为准")
            : t("UniRouter 已绑定；等待 X-Insight 持久接管及原链读取确认，原连接继续运行")}
        </Text>}
        <Text type="tertiary">{t("Connector 配对状态只反映自动配对；手工 Feed 的真实连接和凭据状态以上方 Feed 字段为准。")}</Text>
        {status?.connector_installation?.last_error_code && (
          <div style={{ marginTop: 8 }}>
            <Tag color="red">{status.connector_installation.last_error_code}</Tag>
          </div>
        )}
      </div>

      <Title heading={6} style={{ marginBottom: -8 }}>{t("高级 Source / 凭证诊断")}</Title>
      <Descriptions
        row
        data={[
          { key: t("全局配置 / 启用"), value: `${status?.global_configured ? t("已配置") : t("未配置")} / ${status?.global_enabled ? t("已启用") : t("未启用")}` },
          { key: t("项目状态"), value: <StatusTag status={projectStatus || 'unknown'}>{projectStatus || 'unknown'}</StatusTag> },
          { key: t("连接状态"), value: <StatusTag status={status?.connection_status || 'not_configured'}>{status?.connection_status || 'not_configured'}</StatusTag> },
          { key: t("来源实例"), value: status?.source_instance || '-' },
          { key: t("当前 active 凭证"), value: credentialLabel(status?.active_credential) },
          { key: t("staged 替换凭证"), value: credentialLabel(status?.staged_credential) },
          { key: t("最近撤销凭证"), value: credentialLabel(status?.last_revoked_credential) },
          { key: t("最近认证拉取"), value: status?.last_authenticated_pull_at ? formatDateTime(status.last_authenticated_pull_at) : '-' },
          { key: t("最近认证凭证"), value: credentialLabel(status?.last_authenticated_credential) },
          { key: t("UniRouter 来源高水位"), value: status?.source_high_watermark || '-' },
          { key: t("接收端提交确认 cursor"), value: status?.receiver_acknowledged_cursor || '-' },
          { key: t("待拉取项"), value: boundedBacklogLabel(status) },
        ]}
      />

      {!projectActive && (
        <div style={{ padding: 12, border: '1px solid var(--semi-color-warning-light-default)', borderRadius: 8 }}>
          <Tag color="orange">{projectStatus === 'archived' ? t("项目已归档") : t("项目已停用")}</Tag>
          <Text type="tertiary">

            {t("创建、激活、恢复和准备轮换已禁用；状态查看、暂停、取消轮换和不可逆撤销仍可使用。")}
          </Text>
        </div>
      )}

      {!expansionAllowed && (
        <div style={{ padding: 12, border: '1px solid var(--semi-color-warning-light-default)', borderRadius: 8 }}>
          <Tag color="orange">{t("仅保留安全控制")}</Tag>
          <Text type="tertiary">
            {expansionRestriction || t("当前 Data Exchange entitlement 不可用。")}

            {t("状态查看、暂停、取消轮换与不可逆撤销保持可用；创建、激活、恢复和准备轮换已禁用。")}
          </Text>
        </div>
      )}

      <div style={{ padding: 12, border: '1px solid var(--semi-color-border)', borderRadius: 8 }}>
        <Text strong>{t("确认语义")}</Text>
        <div style={{ marginTop: 6 }}>
          <Text type="tertiary">

            {t("“接收端提交确认”仅表示 X-Insight 在发出下一次认证 GET 前，前一页已写入 EvidenceStore （或收敛为 duplicate/no-op）且 durable cursor 已提交；不代表搜索、API 或 UI 已可见，也不证明来源完整性。")}
          </Text>
        </div>
        {status?.cursor_outside_retention && (
          <div style={{ marginTop: 8 }}><Tag color="red">{t("确认 cursor 已超出保留窗口，禁止自动重置或回填")}</Tag></div>
        )}
      </div>

      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        {((!status?.active_credential && !status?.staged_credential) ||
          (status?.connection_status === 'paused' && status?.active_credential && !status?.staged_credential)) && (
          <div style={{ width: '100%', display: 'grid', gap: 8 }}>
            <Text strong>{t("Frozen Registration Bundle V1（非秘密）")}</Text>
            <Text type="tertiary">

              {t("X-Insight 在本机生成并保留 bearer token；此处只登记不可逆 verifier、公开 prefix 和 operation ID。 UniRouter 不接收、不返回、也不展示明文 token。")}
            </Text>
            <TextArea
              aria-label="X-Insight feed credential registration bundle"
              placeholder='{"schema_version":"unirouter.xinsight.feed-credential-registration.v1",...}'
              value={registrationJSON}
              onChange={setRegistrationJSON}
              rows={7}
              disabled={!expansionEnabled}
            />
          </div>
        )}
        {!status?.active_credential && !status?.staged_credential && (
          <>
            <Input
              aria-label="X-Insight worker exact IP"
              placeholder={t("X worker 私网 /32 或 /128")}
              value={allowedIP}
              onChange={setAllowedIP}
              disabled={!expansionEnabled}
              style={{ width: 250 }}
            />
            <Button type="primary" disabled={!allowedIP || !registrationReady || !expansionEnabled} onClick={createCredential}>{t("登记初始 staged 凭证")}</Button>
          </>
        )}

        {status?.staged_credential && !status?.active_credential && (
          <>
            <Popconfirm
              title={t("确认 staged key 已安全安装；激活后连接仍保持暂停。")}
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.activateInitialXInsightFeedCredential(projectID, status.staged_credential!.id),
                  t("初始凭证激活"),
                );
              }}
            >
              <Button type="primary" disabled={!expansionEnabled}>{t("激活初始凭证")}</Button>
            </Popconfirm>
            <Popconfirm
              title={t("撤销未安装或交付失败的 staged 凭证；连接保持暂停，之后可重新创建。")}
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.revokeXInsightFeedCredential(projectID, status.staged_credential!.id),
                  t("staged 凭证撤销"),
                );
              }}
            >
              <Button>{t("撤销未安装的 staged 凭证")}</Button>
            </Popconfirm>
          </>
        )}

        {status?.connection_status === 'active' && (
          <Button onClick={() => projectID && mutate(() => api.pauseXInsightFeedConnection(projectID), t("连接暂停"))}>{t("暂停连接")}</Button>
        )}
        {status?.connection_status === 'paused' && status?.active_credential && !status?.staged_credential && (
          <Button type="primary" disabled={!expansionEnabled} onClick={() => projectID && mutate(() => api.resumeXInsightFeedConnection(projectID), t("连接恢复"))}>{t("恢复连接")}</Button>
        )}

        {status?.connection_status === 'paused' && status?.active_credential && !status?.staged_credential && (
          <Button disabled={!expansionEnabled || !registrationReady} onClick={prepareRotation}>{t("准备两阶段轮换")}</Button>
        )}
        {status?.connection_status === 'paused' && status?.active_credential && status?.staged_credential && (
          <>
            <Popconfirm
              title={t("仅在 staged key 已原子安装、X 已 systemctl restart（不是 reload），并用新 key 观察到暂停 503 后继续。")}
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.activateXInsightFeedRotation(projectID, status.staged_credential!.id),
                  t("轮换激活"),
                );
              }}
            >
              <Button type="primary" disabled={!expansionEnabled}>{t("确认重启与 503，激活轮换")}</Button>
            </Popconfirm>
            <Popconfirm
              title={t("撤销 staged 替换凭证；旧 active 凭证保持不变，连接保持暂停。")}
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.cancelXInsightFeedRotation(projectID, status.staged_credential!.id),
                  t("轮换取消"),
                );
              }}
            >
              <Button>{t("取消轮换")}</Button>
            </Popconfirm>
          </>
        )}

        {status?.active_credential && !status?.staged_credential && (
          <Popconfirm
            title={t("撤销不可逆：当前 feed 凭证将终止，连接同时暂停；其他 key、用户和 grant 不受影响。")}
            onConfirm={() => {
              if (projectID) return mutate(
                () => api.revokeXInsightFeedCredential(projectID, status.active_credential!.id),
                t("凭证撤销"),
              );
            }}
          >
            <Button type="danger">{t("撤销当前 feed 凭证")}</Button>
          </Popconfirm>
        )}
      </div>

      <div>
        <Title heading={6}>{t("轮换顺序（固定）")}</Title>
        <Text type="tertiary">

          {t("暂停并确认旧 key 返回可重试 503 → prepare staged（旧凭证保持 active）→ X 原子替换 credential file → systemctl restart（不可 reload）→ 用 staged key 确认同一 503 → activate 原子晋升新 key 并撤销旧 key → 手动恢复连接。")}
        </Text>
      </div>

      {projectID && <XInsightSourceConnection key={projectID} projectId={projectID} orgId={selectedProject?.org_id} expansionAllowed={expansionEnabled} budgetReadAllowed={['platform_admin', 'org_admin', 'billing_admin'].includes(user?.organization_role ?? user?.role ?? '')} budgetWriteAllowed={['platform_admin', 'billing_admin'].includes(user?.organization_role ?? user?.role ?? '')} />}
      {projectID && <XInsightContentManagement key={`content-${projectID}`} projectId={projectID} expansionAllowed={expansionEnabled} />}
      {projectID && selectedProject?.org_id && status?.connector_installation && <XInsightMachineDelegationManagement
        key={`machine-${status.connector_installation.installation_id}`}
        orgId={selectedProject.org_id} projectId={projectID}
        installationId={status.connector_installation.installation_id}
        deploymentId={status.connector_installation.x_deployment_id}
        deliveredCandidates={status.connector_installation.delivered_machine_candidates}
        onRefreshCandidates={loadStatus}
        sourceInstance={status.connector_installation.source_instance}
        orgAdmin={['platform_admin', 'org_admin'].includes(user?.organization_role ?? user?.role ?? '')} />}
      </details>
    </div>
  );
}
