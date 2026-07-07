import { useState, useEffect } from 'react';
import { Button, Table, Modal, Form, Toast, Space, SideSheet, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit, IconCopy, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { PageHeader, StatusTag, Surface } from '../ui';
import { keyQuotaText } from '../utils/quota';

const { Text } = Typography;

function fallbackCopyText(text: string) {
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', 'true');
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  document.body.appendChild(textarea);
  textarea.select();
  try {
    return document.execCommand('copy');
  } finally {
    document.body.removeChild(textarea);
  }
}

interface ApiKey {
  id: number;
  name: string;
  key_prefix: string;
  allowed_ips: string;
  allowed_models: string;
  status: string;
  rpm_limit: number;
  rps_limit?: number;
  qps_limit?: number;
  quota?: {
    rpm_limit?: number | null;
    rps_limit?: number | null;
    tpm_limit?: number | null;
    budget_limit_cents?: number | null;
  };
  created_at: string;
}

export default function KeysPage() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editing, setEditing] = useState<ApiKey | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [createdKey, setCreatedKey] = useState<{ key: string; key_prefix: string; name: string } | null>(null);

  const load = (p = page) => {
    setLoading(true);
    api.getKeys(p)
      .then((data) => {
        setKeys(data.data || []);
        setTotal(data.total || 0);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);
  useEffect(() => {
    api.getModelNames().then((d: any) => {
      const source = Array.isArray(d?.data) ? d.data : d;
      const models = (Array.isArray(source) ? source : [])
        .map((m: any) => typeof m === 'string' ? m : m.model)
        .filter(Boolean);
      setAvailableModels(models);
    }).catch(() => {});
  }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); };

  const handleSubmit = async (values: Record<string, any>) => {
    // Convert allowed_models array to comma-separated string
    if (Array.isArray(values.allowed_models)) {
      values.allowed_models = values.allowed_models.join(',');
    }
    if (values.rps_limit !== undefined) {
      values.rps_limit = Number(values.rps_limit || 10);
    }
    try {
      if (editing) {
        await api.updateKey(editing.id, values);
        Toast.success('密钥更新成功');
      } else {
        const result = await api.createKey(values);
        if (result?.key) {
          setCreatedKey({
            key: result.key,
            key_prefix: result.key_prefix,
            name: values.name,
          });
        }
        Toast.success('密钥创建成功');
      }
      closeDrawer();
      load();
    } catch (err: any) {
      const message = err?.message ? `：${err.message}` : '';
      Toast.error(`${editing ? '更新失败' : '创建失败'}${message}`);
    }
  };

  const handleDisable = async (id: number) => {
    Modal.confirm({
      title: '确认禁用',
      content: '禁用后使用该密钥的请求将被拒绝，确定要禁用吗？',
      onOk: async () => {
        try {
          await api.disableKey(id);
          Toast.success('已禁用');
          load();
        } catch {
          Toast.error('禁用失败');
        }
      },
    });
  };

  const handleEnable = async (id: number) => {
    Modal.confirm({
      title: '确认启用',
      content: '启用后该密钥可继续用于网关请求，确定要启用吗？',
      onOk: async () => {
        try {
          await api.updateKey(id, { status: 'active' });
          Toast.success('已启用');
          load();
        } catch {
          Toast.error('启用失败');
        }
      },
    });
  };

  const copyCreatedKey = async () => {
    if (!createdKey?.key) return;
    try {
      if (navigator.clipboard?.writeText && window.isSecureContext) {
        await navigator.clipboard.writeText(createdKey.key);
      } else if (!fallbackCopyText(createdKey.key)) {
        throw new Error('fallback copy failed');
      }
      Toast.success('已复制完整密钥');
    } catch {
      Toast.error('复制失败，请手动复制');
    }
  };

  const openEdit = (record: ApiKey) => {
    setEditing(record);
    setShowDrawer(true);
    setTimeout(() => {
      formApi?.setValues({
        name: record.name,
        allowed_ips: record.allowed_ips,
        allowed_models: record.allowed_models ? record.allowed_models.split(',').map(m => m.trim()).filter(Boolean) : [],
        rpm_limit: record.rpm_limit || 60,
        rps_limit: record.rps_limit ?? record.qps_limit ?? 10,
      });
    }, 100);
  };

  const openCreate = () => {
    setEditing(null);
    setShowDrawer(true);
    formApi?.reset();
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Key 前缀', dataIndex: 'key_prefix', key: 'prefix', render: (v: string) => <code>{v}***</code> },
    { title: '允许 IP', dataIndex: 'allowed_ips', key: 'ips', render: (v: string) => v || '全部' },
    { title: '允许模型', dataIndex: 'allowed_models', key: 'models', render: (v: string) => v || '全部' },
    { title: '限额', key: 'quota', render: (_: unknown, record: ApiKey) => keyQuotaText(record) },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 80,
      render: (v: string) => <StatusTag status={v}>{v}</StatusTag>,
    },
    {
      title: '操作', key: 'actions', width: 140,
      render: (_: any, record: ApiKey) => (
        <Space>
          <Button icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          {record.status === 'disabled' ? (
            <Button icon={<IconRefresh />} size="small" type="tertiary" onClick={() => handleEnable(record.id)} />
          ) : (
            <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDisable(record.id)} />
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="流量治理"
        title="访问凭证"
        description="管理调用方访问凭证、允许 IP、允许模型范围和 RPM/RPS/TPM 限额口径。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增访问凭证</Button>}
      />

      <Surface>
        <div className="ur-table-shell">
          <Table
            dataSource={keys}
            columns={columns}
            rowKey="id"
            loading={loading}
            pagination={{
              currentPage: page, pageSize, total,
              pageSizeOpts: [20, 50, 100], showSizeChanger: true,
              onPageChange: (p: number) => { setPage(p); load(p); },
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1); },
            }}
          />
        </div>
      </Surface>

      <SideSheet
        title={editing ? '编辑访问凭证' : '新增访问凭证'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit} initValues={{ rpm_limit: 60, rps_limit: 10 }}>
          <Form.Input field="name" label="密钥名称" placeholder="service-prod" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="allowed_ips" label="允许 IP (逗号分隔，留空=全部)" placeholder="127.0.0.1,10.0.0.0/8" />
          <Form.Select
            field="allowed_models"
            label="允许模型 (留空=全部)"
            placeholder="选择允许的模型"
            multiple
            optionList={availableModels.map(m => ({ label: m, value: m }))}
            style={{ width: '100%' }}
          />
          <Form.InputNumber field="rpm_limit" label="RPM 限速" min={1} style={{ width: '100%' }} />
          <Form.InputNumber field="rps_limit" label="RPS 限速" min={1} style={{ width: '100%' }} />
          <Text type="tertiary" size="small">TPM 暂未配置为 Key 级限额；当前仅展示口径，不启用 Scheduler V2。</Text>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>

      <Modal
        title="请保存完整 API 密钥"
        visible={!!createdKey}
        onCancel={() => setCreatedKey(null)}
        footer={(
          <Space>
            <Button icon={<IconCopy />} onClick={copyCreatedKey}>复制密钥</Button>
            <Button type="primary" onClick={() => setCreatedKey(null)}>我已保存</Button>
          </Space>
        )}
      >
        <Text type="warning">
          完整密钥只在创建后显示一次。关闭后系统只保留哈希，无法再次查看或找回。
        </Text>
        <div className="ur-secret-reveal">
          <Text strong>{createdKey?.name}</Text>
          <code>{createdKey?.key}</code>
        </div>
      </Modal>
    </div>
  );
}
