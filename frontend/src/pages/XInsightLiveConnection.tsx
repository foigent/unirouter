import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Descriptions, Input, Popconfirm, Select, SideSheet, Spin, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { StatusTag } from '../ui';

const { Text, Title } = Typography;

interface Project {
  id: number;
  name: string;
  org_name?: string;
}

interface FeedCredential {
  id: number;
  key_prefix: string;
  lifecycle: 'active' | 'staged' | 'revoked';
  created_at: string;
  revoked_at?: string;
}

interface FeedConnectionStatus {
  project_id: number;
  project_name: string;
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
  cursor_outside_retention: boolean;
}

interface OneTimeCredential {
  id: number;
  key: string;
  key_prefix: string;
  lifecycle: 'staged';
  project_id: number;
  purpose: 'xinsight.feed.read';
  key_type: 'service';
}

function credentialLabel(credential?: FeedCredential | null) {
  if (!credential) return '-';
  return `#${credential.id} · ${credential.key_prefix} · ${credential.lifecycle}`;
}

export default function XInsightLiveConnection() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState<number>();
  const [status, setStatus] = useState<FeedConnectionStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [allowedIP, setAllowedIP] = useState('');
  const [oneTimeCredential, setOneTimeCredential] = useState<OneTimeCredential | null>(null);

  const loadProjects = useCallback(async () => {
    try {
      const response = await api.getProjects(1, 100, { status: 'active' });
      const next = (response?.data || []) as Project[];
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
      const created = await api.createXInsightFeedCredential(projectID, { allowed_ip: allowedIP }) as OneTimeCredential;
      setOneTimeCredential(created);
      Toast.success('已创建一个 staged feed-only 凭证；连接仍保持暂停');
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
      const created = await api.prepareXInsightFeedRotation(projectID) as OneTimeCredential;
      setOneTimeCredential(created);
      Toast.success('轮换已准备；旧凭证保持 active，新凭证仅在暂停态可认证');
      await loadStatus();
    } catch (error: any) {
      Toast.error(`准备轮换失败${error?.message ? `：${error.message}` : ''}`);
    } finally {
      setLoading(false);
    }
  };

  const projectOptions = useMemo(() => projects.map(project => ({
    value: project.id,
    label: `${project.name}${project.org_name ? ` · ${project.org_name}` : ''}`,
  })), [projects]);

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
      </div>

      <Descriptions
        row
        data={[
          { key: '全局配置 / 启用', value: `${status?.global_configured ? '已配置' : '未配置'} / ${status?.global_enabled ? '已启用' : '未启用'}` },
          { key: '连接状态', value: <StatusTag status={status?.connection_status || 'not_configured'}>{status?.connection_status || 'not_configured'}</StatusTag> },
          { key: '来源实例', value: status?.source_instance || '-' },
          { key: '当前 active 凭证', value: credentialLabel(status?.active_credential) },
          { key: 'staged 替换凭证', value: credentialLabel(status?.staged_credential) },
          { key: '最近撤销凭证', value: credentialLabel(status?.last_revoked_credential) },
          { key: '最近认证拉取', value: status?.last_authenticated_pull_at ? formatDateTime(status.last_authenticated_pull_at) : '-' },
          { key: '最近认证凭证', value: credentialLabel(status?.last_authenticated_credential) },
          { key: 'UniRouter 来源高水位', value: status?.source_high_watermark || '-' },
          { key: '接收端提交确认 cursor', value: status?.receiver_acknowledged_cursor || '-' },
          { key: '待拉取项', value: String(status?.pending_item_count ?? 0) },
        ]}
      />

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
        {!status?.active_credential && !status?.staged_credential && (
          <>
            <Input
              aria-label="X-Insight worker exact IP"
              placeholder="X worker 私网 /32 或 /128"
              value={allowedIP}
              onChange={setAllowedIP}
              style={{ width: 250 }}
            />
            <Button type="primary" disabled={!allowedIP} onClick={createCredential}>创建初始 staged 凭证</Button>
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
              <Button type="primary">激活初始凭证</Button>
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
          <Button type="primary" onClick={() => projectID && mutate(() => api.resumeXInsightFeedConnection(projectID), '连接恢复')}>恢复连接</Button>
        )}

        {status?.connection_status === 'paused' && status?.active_credential && !status?.staged_credential && (
          <Button onClick={prepareRotation}>准备两阶段轮换</Button>
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
              <Button type="primary">确认重启与 503，激活轮换</Button>
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

      <SideSheet
        title="一次性 feed-only 凭证"
        visible={Boolean(oneTimeCredential)}
        onCancel={() => setOneTimeCredential(null)}
        width={560}
      >
        {oneTimeCredential && (
          <div style={{ display: 'grid', gap: 14 }}>
            <Tag color="orange">关闭后不可再次读取明文</Tag>
            <Text>用途：<Text code>{oneTimeCredential.purpose}</Text></Text>
            <Text>生命周期：staged（连接保持 paused）</Text>
            <Text>Key ID / Prefix：#{oneTimeCredential.id} · <Text code>{oneTimeCredential.key_prefix}</Text></Text>
            <Input readOnly value={oneTimeCredential.key} aria-label="一次性 feed key" />
            <Text type="tertiary">
              将凭证直接安装到 X-Insight 的 systemd credential，随后关闭此面板。不得把管理员凭证或通用模型网关 key 交给 X。
            </Text>
            <Button type="primary" onClick={() => setOneTimeCredential(null)}>我已安全安装并关闭明文</Button>
          </div>
        )}
      </SideSheet>
    </div>
  );
}
