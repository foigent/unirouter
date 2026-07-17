import { useEffect, useRef, useState } from 'react';
import { Button, Table, Modal, Form, Toast, Space, SideSheet, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit, IconCopy, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { keyTypeOptions } from '../utils/displayLabels';
import { useAuthStore } from '../store/auth';
import { isPlatformAdminRole } from '../navigation';
import { loadAllPages } from '../utils/loadAllPages';

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
  org_id: number;
  org_name?: string;
  project_id: number;
  project_name?: string;
  owner_user_id?: number | null;
  owner_username?: string;
  key_type?: string;
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
  policy_labels?: string[];
  created_at: string;
}

interface Organization {
  id: number;
  name: string;
}

interface Project {
  id: number;
  org_id: number;
  name: string;
}

interface KeyOwner {
  id: number;
  org_id: number;
  username: string;
}

const editableKeyTypeOptions = keyTypeOptions.filter(option => ['personal', 'shared', 'service'].includes(option.value));

function keyTypeLabel(value?: string) {
  return keyTypeOptions.find(option => option.value === value)?.label || value || '未知';
}

function keyRateLimitText(record: ApiKey) {
  const rpm = record.quota?.rpm_limit ?? record.rpm_limit;
  const rps = record.quota?.rps_limit ?? record.rps_limit ?? record.qps_limit;
  return `RPM ${rpm ?? '未配置'} / RPS ${rps ?? '未配置'}`;
}

export default function KeysPage() {
  const currentUser = useAuthStore((state) => state.user);
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [owners, setOwners] = useState<KeyOwner[]>([]);
  const [selectedOrgId, setSelectedOrgId] = useState<number>();
  const [selectedKeyType, setSelectedKeyType] = useState('shared');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editing, setEditing] = useState<ApiKey | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [createdKey, setCreatedKey] = useState<{ key: string; key_prefix: string; name: string } | null>(null);

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setKeys([]);
    setTotal(0);
    api.getKeys(p, size)
      .then((data) => {
        if (version !== requestVersion.current) return;
        setKeys(data.data || []);
        setTotal(data.total || 0);
      })
      .catch(() => {
        if (version === requestVersion.current) setLoadError('无法加载访问凭证列表。');
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  useEffect(() => { load(1, pageSize); }, []);
  useEffect(() => {
    api.getModelNames().then((d: any) => {
      const source = Array.isArray(d?.data) ? d.data : d;
      const models = (Array.isArray(source) ? source : [])
        .map((m: any) => typeof m === 'string' ? m : m.model)
        .filter(Boolean);
      setAvailableModels(models);
    }).catch(() => {});
  }, []);
  useEffect(() => {
    const fallbackOrganizations = currentUser?.org_id
      ? [{ id: currentUser.org_id, name: currentUser.org_name || `组织 #${currentUser.org_id}` }]
      : [];
    void (async () => {
      const organizationItems = await loadAllPages<Organization>((page, size) => api.getOrganizations(page, size))
        .catch(() => fallbackOrganizations);
      const effectiveOrganizations = organizationItems.length ? organizationItems : fallbackOrganizations;
      const projectItems = isPlatformAdminRole(currentUser?.role)
        ? (await Promise.all(effectiveOrganizations.map(org =>
            loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active', org_id: org.id })).catch(() => []),
          ))).flat()
        : await loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active' })).catch(() => []);
      const ownerItems = await loadAllPages<KeyOwner>((page, size) => api.getUsers(page, size)).catch(() => []);
      setOrganizations(effectiveOrganizations);
      setProjects(projectItems);
      setOwners(ownerItems);
    })();
  }, [currentUser?.org_id, currentUser?.org_name, currentUser?.role]);

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); };

  const handleSubmit = async (values: Record<string, any>) => {
    const keyType = values.key_type || 'shared';
    const payload: Record<string, any> = {
      org_id: Number(values.org_id),
      project_id: Number(values.project_id),
      key_type: keyType,
      owner_user_id: keyType === 'personal' ? Number(values.owner_user_id) : 0,
      name: values.name,
      allowed_ips: values.allowed_ips || '',
      allowed_models: Array.isArray(values.allowed_models) ? values.allowed_models.join(',') : (values.allowed_models || ''),
      rpm_limit: Number(values.rpm_limit || 60),
      rps_limit: Number(values.rps_limit || 10),
      policy_labels: typeof values.policy_labels === 'string'
        ? values.policy_labels.split(',').map((value: string) => value.trim()).filter(Boolean)
        : (values.policy_labels || []),
    };
    try {
      if (editing) {
        const updatePayload = { ...payload };
        delete updatePayload.org_id;
        await api.updateKey(editing.id, updatePayload);
        Toast.success('密钥更新成功');
      } else {
        const result = await api.createKey(payload);
        if (result?.key) {
          setCreatedKey({
            key: result.key,
            key_prefix: result.key_prefix,
            name: payload.name,
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
  };

  const openCreate = () => {
    setEditing(null);
    setShowDrawer(true);
  };

  useEffect(() => {
    if (!showDrawer || !formApi) return;
    formApi.reset();
    if (editing) {
      const keyType = editing.key_type || 'shared';
      setSelectedOrgId(editing.org_id);
      setSelectedKeyType(keyType);
      formApi.setValues({
        org_id: editing.org_id,
        project_id: editing.project_id,
        key_type: keyType,
        owner_user_id: editing.owner_user_id || undefined,
        name: editing.name,
        allowed_ips: editing.allowed_ips,
        allowed_models: editing.allowed_models ? editing.allowed_models.split(',').map(m => m.trim()).filter(Boolean) : [],
        rpm_limit: editing.rpm_limit || 60,
        rps_limit: editing.rps_limit ?? editing.qps_limit ?? 10,
        policy_labels: (editing.policy_labels || []).join(', '),
      });
    } else {
      const orgId = currentUser?.org_id || organizations[0]?.id;
      const projectId = projects.find(project => !orgId || project.org_id === orgId)?.id;
      setSelectedOrgId(orgId);
      setSelectedKeyType('shared');
      formApi.setValues({
        org_id: orgId,
        project_id: projectId,
        key_type: 'shared',
        rpm_limit: 60,
        rps_limit: 10,
      });
    }
  }, [currentUser?.org_id, editing, formApi, organizations, projects, showDrawer]);

  const scopedProjects = projects.filter(project => !selectedOrgId || project.org_id === selectedOrgId);
  const scopedOwners = owners.filter(owner => !selectedOrgId || owner.org_id === selectedOrgId);

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    {
      title: '组织', key: 'organization',
      render: (_: unknown, record: ApiKey) => record.org_name
        ? `${record.org_name} (#${record.org_id})`
        : (record.org_id ? `组织 #${record.org_id}` : '—'),
    },
    {
      title: '项目', key: 'project',
      render: (_: unknown, record: ApiKey) => record.project_name
        ? `${record.project_name} (#${record.project_id})`
        : (record.project_id ? `项目 #${record.project_id}` : '—'),
    },
    { title: '类型', dataIndex: 'key_type', key: 'key_type', render: (value: string) => keyTypeLabel(value) },
    {
      title: '所有者', key: 'owner',
      render: (_: unknown, record: ApiKey) => record.key_type === 'personal'
        ? (record.owner_username || (record.owner_user_id ? `用户 #${record.owner_user_id}` : '—'))
        : '—',
    },
    { title: 'Key 前缀', dataIndex: 'key_prefix', key: 'prefix', render: (v: string) => <code>{v}***</code> },
    { title: '允许 IP', dataIndex: 'allowed_ips', key: 'ips', render: (v: string) => v || '全部' },
    { title: '允许模型', dataIndex: 'allowed_models', key: 'models', render: (v: string) => v || '全部' },
    { title: '策略标签', dataIndex: 'policy_labels', key: 'policy_labels', render: (v: string[]) => v?.length ? v.join(', ') : '无' },
    { title: '限速', key: 'quota', render: (_: unknown, record: ApiKey) => keyRateLimitText(record) },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 80,
      render: (v: string) => <StatusTag status={v}>{v === 'active' ? '启用' : v === 'disabled' ? '停用' : v}</StatusTag>,
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (v: string) => formatDateTime(v) },
    {
      title: '操作', key: 'actions', width: 180,
      render: (_: any, record: ApiKey) => (
        <Space>
          <Button aria-label={`编辑访问凭证 ${record.name}`} icon={<IconEdit />} size="small" onClick={() => openEdit(record)}>编辑</Button>
          {record.status === 'disabled' ? (
            <Button aria-label={`启用访问凭证 ${record.name}`} icon={<IconRefresh />} size="small" type="tertiary" onClick={() => handleEnable(record.id)}>启用</Button>
          ) : (
            <Button aria-label={`停用访问凭证 ${record.name}`} icon={<IconDelete />} size="small" type="danger" onClick={() => handleDisable(record.id)}>停用</Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问与安全"
        title="访问凭证"
        description="管理调用方访问凭证的组织、项目、类型、所有者与 RPM/RPS 限速；完整 Key 仅创建时显示一次。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增访问凭证</Button>}
      />

      <Surface>
        <div className="ur-table-shell">
          <Table
            dataSource={keys}
            columns={columns}
            rowKey="id"
            loading={loading}
            empty={loadError ? (
              <EmptyState title="访问凭证加载失败" description={loadError} actions={<Button type="primary" onClick={() => load()}>重新加载访问凭证</Button>} minHeight={220} />
            ) : <EmptyState title="暂无访问凭证" minHeight={220} />}
            pagination={{
              currentPage: page, pageSize, total,
              pageSizeOpts: [20, 50, 100], showSizeChanger: true,
              onPageChange: (p: number) => { setPage(p); load(p, pageSize); },
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1, s); },
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
          <Form.Select
            field="org_id"
            label="所属组织"
            disabled={!!editing}
            optionList={organizations.map(org => ({ label: `${org.name} (#${org.id})`, value: org.id }))}
            rules={[{ required: true, message: '请选择所属组织' }]}
            onChange={(value: any) => {
              const orgId = Number(value);
              const firstProject = projects.find(project => project.org_id === orgId);
              setSelectedOrgId(orgId);
              formApi?.setValue('project_id', firstProject?.id);
              formApi?.setValue('owner_user_id', undefined);
            }}
            style={{ width: '100%' }}
          />
          <Form.Select
            field="project_id"
            label="所属项目"
            optionList={scopedProjects.map(project => ({ label: `${project.name} (#${project.id})`, value: project.id }))}
            rules={[{ required: true, message: '请选择所属项目' }]}
            style={{ width: '100%' }}
          />
          <Form.Select
            field="key_type"
            label="凭证类型"
            optionList={editableKeyTypeOptions}
            rules={[{ required: true, message: '请选择凭证类型' }]}
            onChange={(value: any) => {
              const keyType = String(value || 'shared');
              setSelectedKeyType(keyType);
              if (keyType !== 'personal') formApi?.setValue('owner_user_id', undefined);
            }}
            style={{ width: '100%' }}
          />
          {selectedKeyType === 'personal' && (
            <Form.Select
              field="owner_user_id"
              label="Key 所有者"
              optionList={scopedOwners.map(owner => ({ label: `${owner.username} (#${owner.id})`, value: owner.id }))}
              rules={[{ required: true, message: '个人 Key 必须选择所有者' }]}
              style={{ width: '100%' }}
            />
          )}
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
          <Form.Input field="policy_labels" label="共享限额标签（逗号分隔）" placeholder="production, customer-facing" />
          <Text type="tertiary" size="small">标签由管理员绑定，调用方不能自行声明；匹配标签的共享策略只会进一步收紧额度。</Text>
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
