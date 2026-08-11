import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Descriptions, Input, Popconfirm, Select, Spin, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { loadAllPages } from '../utils/loadAllPages';
import { StatusTag } from '../ui';

const { Text, Title } = Typography;

interface Project {
  id: number;
  name: string;
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

export function parseReceiverRegistrationBundle(raw: string): ReceiverFeedCredentialRegistrationV1 {
  const parsed = JSON.parse(raw) as Record<string, unknown>;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('registration 必须是 JSON object');
  }
  const keys = Object.keys(parsed).sort();
  if (keys.length !== registrationKeys.length ||
      keys.some((key, index) => key !== [...registrationKeys].sort()[index])) {
    throw new Error('registration 字段必须与 Frozen Registration Bundle V1 完全一致');
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
    throw new Error('registration 不符合 Frozen Registration Bundle V1');
  }
  return parsed as unknown as ReceiverFeedCredentialRegistrationV1;
}

function credentialLabel(credential?: FeedCredential | null) {
  if (!credential) return '-';
  return `#${credential.id} · ${credential.key_prefix} · ${credential.lifecycle}`;
}

function connectorProductLabel(status?: FeedConnectionStatus['connector_product_status']) {
  switch (status) {
    case 'connected': return '已连接';
    case 'abnormal': return '异常';
    case 'paused': return '已暂停';
    case 'connecting': return '连接中';
    default: return '未连接';
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
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState<number>();
  const [status, setStatus] = useState<FeedConnectionStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [allowedIP, setAllowedIP] = useState('');
  const [registrationJSON, setRegistrationJSON] = useState('');

  const loadProjects = useCallback(async () => {
    try {
      const next = await loadAllPages<Project>((page, pageSize) => api.getProjects(page, pageSize));
      setProjects(next);
      setProjectID(current => current || next[0]?.id);
    } catch (error: any) {
      Toast.error(`项目加载失败${error?.message ? `：${error.message}` : ''}`);
    }
  }, []);

  const loadStatus = useCallback(async () => {
    if (!projectID) {
      setStatus(null);
      return;
    }
    setLoading(true);
    try {
      setStatus(await api.getXInsightFeedConnection(projectID) as FeedConnectionStatus);
    } catch (error: any) {
      Toast.error(`实时连接状态加载失败${error?.message ? `：${error.message}` : ''}`);
      setStatus(null);
    } finally {
      setLoading(false);
    }
  }, [projectID]);

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadStatus(); }, [loadStatus]);

  const mutate = useCallback(async (operation: () => Promise<any>, success: string) => {
    setLoading(true);
    try {
      await operation();
      Toast.success(success);
      await loadStatus();
    } catch (error: any) {
      Toast.error(`${success}失败${error?.message ? `：${error.message}` : ''}`);
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
      Toast.success(`已登记 staged feed-only 凭证 #${created.id}（${created.key_prefix}）；连接仍保持暂停`);
      await loadStatus();
    } catch (error: any) {
      Toast.error(`创建失败${error?.message ? `：${error.message}` : ''}`);
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
      Toast.success(`轮换已准备（#${created.id} · ${created.key_prefix}）；旧凭证保持 active，新凭证仅在暂停态可认证`);
      await loadStatus();
    } catch (error: any) {
      Toast.error(`准备轮换失败${error?.message ? `：${error.message}` : ''}`);
    } finally {
      setLoading(false);
    }
  };

  const projectOptions = useMemo(() => projects.map(project => ({
    value: project.id,
    label: `${project.name}${project.org_name ? ` · ${project.org_name}` : ''}${
      project.status === 'active' ? '' : project.status === 'archived' ? ' · 已归档' : ' · 已停用'
    }`,
  })), [projects]);
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

  if (!projectID && projects.length === 0) {
    return <Spin tip="正在读取可用项目…" />;
  }

  return (
    <div style={{ display: 'grid', gap: 18 }}>
      <div className="ur-toolbar">
        <Select
          aria-label="实时连接项目"
          value={projectID}
          optionList={projectOptions}
          onChange={value => setProjectID(Number(value))}
          style={{ width: 320 }}
        />
        <div style={{ flex: 1 }} />
        <Button icon={<IconRefresh />} onClick={loadStatus} loading={loading}>刷新状态</Button>
      </div>

      <div className="ur-chip-row">
        <Tag color="green">metadata only</Tag>
        <Tag color="blue">project-bound</Tag>
        <Tag>purpose=xinsight.feed.read</Tag>
        <Tag>无模型调用权限</Tag>
        <Tag>不向 X-Insight 提供管理员凭证</Tag>
        <Tag color="purple">receiver-generated credential</Tag>
      </div>

      <div style={{ padding: 14, border: '1px solid var(--semi-color-border)', borderRadius: 8 }}>
        <Title heading={6} style={{ marginTop: 0 }}>Connector 状态</Title>
        <Descriptions
          row
          data={[
            {
              key: '状态',
              value: <StatusTag status={status?.connector_product_status || 'not_connected'}>
                {connectorProductLabel(status?.connector_product_status)}
              </StatusTag>,
            },
            {
              key: '最近同步',
              value: status?.connector_installation?.last_successful_sync_at
                ? formatDateTime(status.connector_installation.last_successful_sync_at)
                : status?.last_authenticated_pull_at ? formatDateTime(status.last_authenticated_pull_at) : '-',
            },
            { key: '待同步 backlog', value: boundedBacklogLabel(status) },
            {
              key: '最旧待同步延迟',
              value: status?.oldest_pending_at
                ? `${status.oldest_pending_age_seconds} 秒 · ${formatDateTime(status.oldest_pending_at)}`
                : '-',
            },
            {
              key: '最近控制动作',
              value: status?.connector_latest_operation
                ? `${status.connector_latest_operation.action} · ${status.connector_latest_operation.state}`
                : '-',
            },
          ]}
        />
        {status?.connector_installation?.last_error_code && (
          <div style={{ marginTop: 8 }}>
            <Tag color="red">{status.connector_installation.last_error_code}</Tag>
          </div>
        )}
      </div>

      <Title heading={6} style={{ marginBottom: -8 }}>高级 Source / 凭证诊断</Title>
      <Descriptions
        row
        data={[
          { key: '全局配置 / 启用', value: `${status?.global_configured ? '已配置' : '未配置'} / ${status?.global_enabled ? '已启用' : '未启用'}` },
          { key: '项目状态', value: <StatusTag status={projectStatus || 'unknown'}>{projectStatus || 'unknown'}</StatusTag> },
          { key: '连接状态', value: <StatusTag status={status?.connection_status || 'not_configured'}>{status?.connection_status || 'not_configured'}</StatusTag> },
          { key: '来源实例', value: status?.source_instance || '-' },
          { key: '当前 active 凭证', value: credentialLabel(status?.active_credential) },
          { key: 'staged 替换凭证', value: credentialLabel(status?.staged_credential) },
          { key: '最近撤销凭证', value: credentialLabel(status?.last_revoked_credential) },
          { key: '最近认证拉取', value: status?.last_authenticated_pull_at ? formatDateTime(status.last_authenticated_pull_at) : '-' },
          { key: '最近认证凭证', value: credentialLabel(status?.last_authenticated_credential) },
          { key: 'UniRouter 来源高水位', value: status?.source_high_watermark || '-' },
          { key: '接收端提交确认 cursor', value: status?.receiver_acknowledged_cursor || '-' },
          { key: '待拉取项', value: boundedBacklogLabel(status) },
        ]}
      />

      {!projectActive && (
        <div style={{ padding: 12, border: '1px solid var(--semi-color-warning-light-default)', borderRadius: 8 }}>
          <Tag color="orange">{projectStatus === 'archived' ? '项目已归档' : '项目已停用'}</Tag>
          <Text type="tertiary">
            创建、激活、恢复和准备轮换已禁用；状态查看、暂停、取消轮换和不可逆撤销仍可使用。
          </Text>
        </div>
      )}

      {!expansionAllowed && (
        <div style={{ padding: 12, border: '1px solid var(--semi-color-warning-light-default)', borderRadius: 8 }}>
          <Tag color="orange">仅保留安全控制</Tag>
          <Text type="tertiary">
            {expansionRestriction || '当前 Data Exchange entitlement 不可用。'}
            状态查看、暂停、取消轮换与不可逆撤销保持可用；创建、激活、恢复和准备轮换已禁用。
          </Text>
        </div>
      )}

      <div style={{ padding: 12, border: '1px solid var(--semi-color-border)', borderRadius: 8 }}>
        <Text strong>确认语义</Text>
        <div style={{ marginTop: 6 }}>
          <Text type="tertiary">
            “接收端提交确认”仅表示 X-Insight 在发出下一次认证 GET 前，前一页已写入 EvidenceStore
            （或收敛为 duplicate/no-op）且 durable cursor 已提交；不代表搜索、API 或 UI 已可见，也不证明来源完整性。
          </Text>
        </div>
        {status?.cursor_outside_retention && (
          <div style={{ marginTop: 8 }}><Tag color="red">确认 cursor 已超出保留窗口，禁止自动重置或回填</Tag></div>
        )}
      </div>

      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        {((!status?.active_credential && !status?.staged_credential) ||
          (status?.connection_status === 'paused' && status?.active_credential && !status?.staged_credential)) && (
          <div style={{ width: '100%', display: 'grid', gap: 8 }}>
            <Text strong>Frozen Registration Bundle V1（非秘密）</Text>
            <Text type="tertiary">
              X-Insight 在本机生成并保留 bearer token；此处只登记不可逆 verifier、公开 prefix 和 operation ID。
              UniRouter 不接收、不返回、也不展示明文 token。
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
              placeholder="X worker 私网 /32 或 /128"
              value={allowedIP}
              onChange={setAllowedIP}
              disabled={!expansionEnabled}
              style={{ width: 250 }}
            />
            <Button type="primary" disabled={!allowedIP || !registrationReady || !expansionEnabled} onClick={createCredential}>登记初始 staged 凭证</Button>
          </>
        )}

        {status?.staged_credential && !status?.active_credential && (
          <>
            <Popconfirm
              title="确认 staged key 已安全安装；激活后连接仍保持暂停。"
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.activateInitialXInsightFeedCredential(projectID, status.staged_credential!.id),
                  '初始凭证激活',
                );
              }}
            >
              <Button type="primary" disabled={!expansionEnabled}>激活初始凭证</Button>
            </Popconfirm>
            <Popconfirm
              title="撤销未安装或交付失败的 staged 凭证；连接保持暂停，之后可重新创建。"
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.revokeXInsightFeedCredential(projectID, status.staged_credential!.id),
                  'staged 凭证撤销',
                );
              }}
            >
              <Button>撤销未安装的 staged 凭证</Button>
            </Popconfirm>
          </>
        )}

        {status?.connection_status === 'active' && (
          <Button onClick={() => projectID && mutate(() => api.pauseXInsightFeedConnection(projectID), '连接暂停')}>暂停连接</Button>
        )}
        {status?.connection_status === 'paused' && status?.active_credential && !status?.staged_credential && (
          <Button type="primary" disabled={!expansionEnabled} onClick={() => projectID && mutate(() => api.resumeXInsightFeedConnection(projectID), '连接恢复')}>恢复连接</Button>
        )}

        {status?.connection_status === 'paused' && status?.active_credential && !status?.staged_credential && (
          <Button disabled={!expansionEnabled || !registrationReady} onClick={prepareRotation}>准备两阶段轮换</Button>
        )}
        {status?.connection_status === 'paused' && status?.active_credential && status?.staged_credential && (
          <>
            <Popconfirm
              title="仅在 staged key 已原子安装、X 已 systemctl restart（不是 reload），并用新 key 观察到暂停 503 后继续。"
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.activateXInsightFeedRotation(projectID, status.staged_credential!.id),
                  '轮换激活',
                );
              }}
            >
              <Button type="primary" disabled={!expansionEnabled}>确认重启与 503，激活轮换</Button>
            </Popconfirm>
            <Popconfirm
              title="撤销 staged 替换凭证；旧 active 凭证保持不变，连接保持暂停。"
              onConfirm={() => {
                if (projectID) return mutate(
                  () => api.cancelXInsightFeedRotation(projectID, status.staged_credential!.id),
                  '轮换取消',
                );
              }}
            >
              <Button>取消轮换</Button>
            </Popconfirm>
          </>
        )}

        {status?.active_credential && !status?.staged_credential && (
          <Popconfirm
            title="撤销不可逆：当前 feed 凭证将终止，连接同时暂停；其他 key、用户和 grant 不受影响。"
            onConfirm={() => {
              if (projectID) return mutate(
                () => api.revokeXInsightFeedCredential(projectID, status.active_credential!.id),
                '凭证撤销',
              );
            }}
          >
            <Button type="danger">撤销当前 feed 凭证</Button>
          </Popconfirm>
        )}
      </div>

      <div>
        <Title heading={6}>轮换顺序（固定）</Title>
        <Text type="tertiary">
          暂停并确认旧 key 返回可重试 503 → prepare staged（旧凭证保持 active）→ X 原子替换 credential file →
          systemctl restart（不可 reload）→ 用 staged key 确认同一 503 → activate 原子晋升新 key 并撤销旧 key → 手动恢复连接。
        </Text>
      </div>

    </div>
  );
}
