import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Descriptions, Space, Table, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconFile, IconRefresh, IconSave } from '@douyinfe/semi-icons';
import { api } from '../api/client';
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
  return values.join(', ');
}

function formatRecord(record?: Record<string, number | string>) {
  if (!record || Object.keys(record).length === 0) return [];
  return Object.entries(record).map(([key, value]) => ({ key, value }));
}

export default function LicensePage() {
  const [status, setStatus] = useState<LicenseStatus | null>(null);
  const [licenseText, setLicenseText] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const loadLicense = async () => {
    setLoading(true);
    try {
      const data = await api.getLicense();
      setStatus(data as LicenseStatus);
    } catch (err: any) {
      Toast.error(`License 状态加载失败${err?.message ? `：${err.message}` : ''}`);
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
      { key: 'License ID', value: license.license_id || '-' },
      { key: '客户', value: license.customer || '-' },
      { key: '绑定部署', value: license.deployment_id || '-' },
      { key: '授权层级', value: license.tier || '-' },
      { key: '签发时间', value: license.issued_at || '-' },
      { key: '生效时间', value: license.not_before || '-' },
      { key: '到期时间', value: license.expires_at || '-' },
      { key: '功能授权', value: formatList(license.features) },
    ];
  }, [status]);

  const saveLicense = async () => {
    const value = licenseText.trim();
    if (!value) {
      Toast.error('License JSON 不能为空');
      return;
    }
    setSaving(true);
    try {
      const parsed = JSON.parse(value);
      const next = await api.updateLicense(parsed);
      setStatus(next as LicenseStatus);
      setLicenseText('');
      Toast.success('License 已导入');
    } catch (err: any) {
      Toast.error(`License 导入失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setSaving(false);
    }
  };

  const readFile = async (file?: File) => {
    if (!file) return;
    if (file.size > 240 * 1024) {
      Toast.error('License 文件不能超过 240 KiB');
      return;
    }
    const text = await file.text();
    setLicenseText(text);
  };

  if (loading) {
    return <div className="ur-page"><EmptyState title="加载中..." minHeight={360} /></div>;
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="商业控制"
        title="商业授权"
        description="查看签名、部署绑定、功能与资源授权。只有可强制执行的 unirouter.license.v1 才能放行新模型流量。"
        actions={
          <Button icon={<IconRefresh />} onClick={loadLicense}>
            刷新
          </Button>
        }
      />

      <div className="ur-settings-grid">
        <Surface title="状态">
          <Descriptions
            data={[
              { key: '状态', value: <Tag color={statusColor(status?.status)}>{statusText(status?.status)}</Tag> },
              { key: '商业格式', value: status?.commercial ? '是' : '否' },
              { key: '可强制执行', value: status?.enforceable ? '是' : '否' },
              { key: '签名', value: status?.signature_valid ? '有效' : '未验证' },
              { key: '部署匹配', value: status?.deployment_match ? '是' : '否' },
              { key: '验签器', value: status?.verifier_configured ? '已配置' : '缺少公钥' },
              { key: 'Schema', value: status?.schema_version || '-' },
              { key: 'Key ID', value: status?.key_id || '-' },
              { key: '本机部署标识', value: status?.expected_deployment_id || '-' },
              { key: '剩余天数', value: status?.license ? String(status.days_until_expiry) : '-' },
              { key: '校验时间', value: status?.checked_at || '-' },
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

        <Surface title="元数据">
          <Descriptions data={metadataRows} row />
        </Surface>
      </div>

      <Surface title="导入签名商业 License">
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

      <Surface title="限制">
        <Table
          size="small"
          pagination={false}
          dataSource={formatRecord(status?.license?.limits)}
          rowKey="key"
          columns={[
            { title: '限制项（签名键）', dataIndex: 'key' },
            { title: '活跃数量上限', dataIndex: 'value' },
          ]}
          empty={<EmptyState title="未设置资源数量上限" description="缺少 limit 表示该资源不受 License 数量约束。" minHeight={180} />}
        />
      </Surface>
    </div>
  );
}
