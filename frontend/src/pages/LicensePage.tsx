import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Descriptions, Space, Table, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconFile, IconRefresh, IconSave } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, Surface } from '../ui';

const { Text } = Typography;

interface LicensePayload {
  license_id?: string;
  customer?: string;
  deployment_id?: string;
  tier?: string;
  issued_at?: string;
  not_before?: string;
  expires_at?: string;
  features?: string[];
  limits?: Record<string, number>;
  metadata?: Record<string, string>;
}

interface LicenseStatus {
  status: string;
  valid: boolean;
  enforceable: boolean;
  commercial: boolean;
  signature_valid: boolean;
  deployment_match: boolean;
  warning: boolean;
  days_until_expiry: number;
  checked_at: string;
  verifier_configured: boolean;
  schema_version?: string;
  key_id?: string;
  expected_deployment_id?: string;
  license?: LicensePayload;
  error?: string;
}

function statusColor(status?: string) {
  switch (status) {
    case 'active':
      return 'green';
    case 'expiring':
      return 'orange';
    case 'expired':
    case 'invalid':
    case 'not_yet_valid':
    case 'deployment_mismatch':
      return 'red';
    case 'missing':
    default:
      return 'grey';
  }
}

function statusText(status?: string) {
  switch (status) {
    case 'active':
      return '有效';
    case 'expiring':
      return '即将到期';
    case 'expired':
      return '已到期';
    case 'invalid':
      return '无效';
    case 'not_yet_valid':
      return '尚未生效';
    case 'deployment_mismatch':
      return '部署不匹配';
    case 'missing':
      return '未导入';
    default:
      return status || '-';
  }
}

function formatList(values?: string[]) {
  if (!values || values.length === 0) return '-';
  const labels: Record<string, string> = {
    '*': '全部功能',
    gateway: '模型网关',
    agent_capabilities: 'Agent 能力令牌',
    oidc: '企业身份登录（OIDC）',
    guardrails: '内容安全',
    notifications: '通知中心',
    model_governance: '模型治理',
    data_exchange: 'X-Insight 数据交换',
  };
  return values.map((value) => labels[value] || value).join('、');
}

function formatRecord(record?: Record<string, number | string>) {
  if (!record || Object.keys(record).length === 0) return [];
  const labels: Record<string, string> = {
    active_api_keys: '活跃访问凭证',
    active_users: '活跃用户',
    active_projects: '活跃项目',
    active_channels: '活跃上游通道',
  };
  return Object.entries(record).map(([key, value]) => ({ key, label: labels[key] || key, value }));
}

export default function LicensePage() {
  const [status, setStatus] = useState<LicenseStatus | null>(null);
  const [licenseText, setLicenseText] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [saving, setSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const loadProfile = useAuthStore((state) => state.loadProfile);

  const loadLicense = async () => {
    setLoading(true);
    setStatus(null);
    setLoadError('');
    try {
      const data = await api.getLicense();
      setStatus(data as LicenseStatus);
    } catch (err: any) {
      const message = `商业授权状态加载失败${err?.message ? `：${err.message}` : ''}`;
      setLoadError(message);
      Toast.error(message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadLicense();
  }, []);

  const metadataRows = useMemo(() => {
    const license = status?.license || {};
    return [
      { key: '授权编号', value: license.license_id || '-' },
      { key: '绑定部署', value: license.deployment_id || '-' },
      { key: '签发时间', value: license.issued_at || '-' },
      { key: '生效时间', value: license.not_before || '-' },
    ];
  }, [status]);

  const saveLicense = async () => {
    const value = licenseText.trim();
    if (!value) {
      Toast.error('授权文件内容不能为空');
      return;
    }
    setSaving(true);
    try {
      const parsed = JSON.parse(value);
      const next = await api.updateLicense(parsed);
      setStatus(next as LicenseStatus);
      setLicenseText('');
      Toast.success('商业授权已导入');
      try {
        await loadProfile();
      } catch {
        Toast.warning('授权已导入，但导航权限状态刷新失败；请稍后刷新页面。');
      }
    } catch (err: any) {
      Toast.error(`商业授权导入失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setSaving(false);
    }
  };

  const readFile = async (file?: File) => {
    if (!file) return;
    if (file.size > 240 * 1024) {
      Toast.error('授权文件不能超过 240 KiB');
      return;
    }
    const text = await file.text();
    setLicenseText(text);
  };

  if (loading) {
    return <div className="ur-page"><EmptyState title="加载中..." minHeight={360} /></div>;
  }

  if (loadError || !status) {
    return (
      <div className="ur-page">
        <PageHeader
          eyebrow="系统与集成"
          title="商业授权"
          description="查看当前部署是否获准提供模型服务、授权给谁、何时到期以及可用功能和容量。"
          actions={<Button icon={<IconRefresh />} onClick={loadLicense}>重试</Button>}
        />
        <Surface title="授权结论">
          <EmptyState
            title="无法确认当前授权状态"
            description={`${loadError || '服务未返回授权状态'}。本页不会将加载失败推断为授权失效或流量被拒绝。`}
            actions={<Button type="primary" icon={<IconRefresh />} onClick={loadLicense}>重试</Button>}
            minHeight={280}
          />
        </Surface>
      </div>
    );
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="系统与集成"
        title="商业授权"
        description="查看当前部署是否获准提供模型服务、授权给谁、何时到期以及可用功能和容量。"
        actions={
          <Button icon={<IconRefresh />} onClick={loadLicense}>
            刷新
          </Button>
        }
      />

      <div className="ur-settings-grid">
        <Surface title="授权结论">
          <Descriptions
            data={[
              { key: '状态', value: <Tag color={statusColor(status?.status)}>{statusText(status?.status)}</Tag> },
              { key: '新模型流量', value: status?.enforceable ? <Tag color="green">允许</Tag> : <Tag color="red">拒绝</Tag> },
              { key: '授权客户', value: status?.license?.customer || '-' },
              { key: '授权版本', value: status?.license?.tier || '-' },
              { key: '到期时间', value: status?.license?.expires_at || '-' },
              { key: '剩余天数', value: status?.license ? String(status.days_until_expiry) : '-' },
              { key: '授权功能', value: formatList(status?.license?.features) },
            ]}
            row
          />
          {status?.error && (
            <div style={{ marginTop: 16 }}>
              <Text type="danger">{status.error}</Text>
            </div>
          )}
          {!status?.enforceable && !status?.error && (
            <div style={{ marginTop: 16 }}>
              <Text type="danger">当前授权不可用于商业运行；新模型请求保持拒绝。</Text>
            </div>
          )}
        </Surface>

        <Surface title="授权文件信息">
          <Descriptions data={metadataRows} row />
        </Surface>
      </div>

      <Surface title="验签与部署绑定详情" description="用于支持人员排查授权文件、公钥或部署标识问题；日常判断请以上方“授权结论”为准。">
        <Descriptions
          data={[
            { key: '商业格式', value: status?.commercial ? '是' : '否' },
            { key: '可强制执行', value: status?.enforceable ? '是' : '否' },
            { key: '签名', value: status?.signature_valid ? '有效' : '未验证' },
            { key: '部署匹配', value: status?.deployment_match ? '是' : '否' },
            { key: '验签公钥', value: status?.verifier_configured ? '已配置' : '缺少公钥' },
            { key: 'Schema', value: status?.schema_version || '-' },
            { key: '签名 Key ID', value: status?.key_id || '-' },
            { key: '当前部署标识', value: status?.expected_deployment_id || '-' },
            { key: '最近校验时间', value: status?.checked_at || '-' },
          ]}
          row
        />
      </Surface>

      <Surface title="导入签名商业授权文件" description="推荐选择由 UniRouter License Manager 生成的 JSON 文件；文本框用于预览或支持场景下粘贴。">
        <Space vertical align="start" style={{ width: '100%' }}>
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json,.json"
            style={{ display: 'none' }}
            onChange={(event) => readFile(event.target.files?.[0])}
          />
          <Space>
            <Button icon={<IconFile />} onClick={() => fileInputRef.current?.click()}>
              选择文件
            </Button>
            <Button type="primary" icon={<IconSave />} loading={saving} onClick={saveLicense}>
              导入
            </Button>
          </Space>
          <TextArea
            value={licenseText}
            onChange={setLicenseText}
            autosize={{ minRows: 8, maxRows: 16 }}
            placeholder='{"schema_version":"unirouter.license.v1","key_id":"license-...","payload":{...},"signature":"..."}'
            style={{ width: '100%' }}
          />
        </Space>
      </Surface>

      <Surface title="授权容量" description="只列出授权文件明确设置的活跃资源数量上限；未列出的资源不受商业授权数量约束。">
        <Table
          size="small"
          pagination={false}
          dataSource={formatRecord(status?.license?.limits)}
          rowKey="key"
          columns={[
            { title: '资源', dataIndex: 'label' },
            { title: '最大活跃数量', dataIndex: 'value' },
          ]}
          empty={<EmptyState title="未设置资源数量上限" description="授权文件未设置该资源时，不受商业授权数量约束。" minHeight={180} />}
        />
      </Surface>
    </div>
  );
}
