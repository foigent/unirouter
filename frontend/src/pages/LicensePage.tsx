import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Descriptions, Space, Table, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconFile, IconRefresh, IconSave } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PageHeader, Surface } from '../ui';

const { Text } = Typography;

interface LicensePayload {
  customer?: string;
  deployment_id?: string;
  tier?: string;
  issued_at?: string;
  expires_at?: string;
  features?: string[];
  limits?: Record<string, number>;
  metadata?: Record<string, string>;
}

interface LicenseStatus {
  status: string;
  valid: boolean;
  signature_valid: boolean;
  warning: boolean;
  days_until_expiry: number;
  checked_at: string;
  verifier_configured: boolean;
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
      return 'red';
    case 'missing':
    default:
      return 'grey';
  }
}

function statusText(status?: string) {
  switch (status) {
    case 'active':
      return 'Active';
    case 'expiring':
      return 'Expiring';
    case 'expired':
      return 'Expired';
    case 'invalid':
      return 'Invalid';
    case 'missing':
      return 'Missing';
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
      { key: 'Customer', value: license.customer || '-' },
      { key: 'Deployment', value: license.deployment_id || '-' },
      { key: 'Tier', value: license.tier || '-' },
      { key: 'Issued At', value: license.issued_at || '-' },
      { key: 'Expires At', value: license.expires_at || '-' },
      { key: 'Features', value: formatList(license.features) },
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
    const text = await file.text();
    setLicenseText(text);
  };

  if (loading) {
    return <div className="ur-page"><EmptyState title="加载中..." minHeight={360} /></div>;
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Governance"
        title="License"
        description="查看离线 License 状态、有效期和部署授权信息。"
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
              { key: 'Status', value: <Tag color={statusColor(status?.status)}>{statusText(status?.status)}</Tag> },
              { key: 'Signature', value: status?.signature_valid ? 'Valid' : 'Not verified' },
              { key: 'Verifier', value: status?.verifier_configured ? 'Configured' : 'Missing public key' },
              { key: 'Days Left', value: status?.license ? String(status.days_until_expiry) : '-' },
              { key: 'Checked At', value: status?.checked_at || '-' },
            ]}
            row
          />
          {status?.error && (
            <div style={{ marginTop: 16 }}>
              <Text type="danger">{status.error}</Text>
            </div>
          )}
        </Surface>

        <Surface title="元数据">
          <Descriptions data={metadataRows} row />
        </Surface>
      </div>

      <Surface title="导入离线 License">
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
            placeholder='{"payload": {...}, "signature": "..."}'
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
            { title: 'Key', dataIndex: 'key' },
            { title: 'Value', dataIndex: 'value' },
          ]}
          empty={<EmptyState title="无 License 限制" minHeight={180} />}
        />
      </Surface>
    </div>
  );
}
