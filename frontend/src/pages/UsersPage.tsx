import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Table, Modal, Form, Tag, Toast, SideSheet, Typography, Tabs, TabPane } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { organizationRoleLabel, roleTagColor, userManagementOrganizationRoleOptions } from '../utils/displayLabels';
import { EmptyState, PageHeader, PasswordField, StatusTag, Surface } from '../ui';
import { useAuthStore } from '../store/auth';
import { canonicalOrganizationRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { loadAllPages } from '../utils/loadAllPages';
import {
  LOCAL_PASSWORD_MAX_UTF8_BYTES,
  LOCAL_PASSWORD_MIN_CODE_POINTS,
  localPasswordCodePointCount,
  localPasswordUTF8ByteCount,
} from '../utils/passwordPolicy';

interface Organization {
  id: number;
  name: string;
}

const apiErrorCode = (error: ApiError) => {
  if (!error.payload || typeof error.payload !== 'object') return '';
  const code = (error.payload as { code?: unknown }).code;
  return typeof code === 'string' ? code : '';
};

export default function UsersPage({ embedded = false }: { embedded?: boolean }) {
  const currentUser = useAuthStore((state) => state.user);
  const [users, setUsers] = useState<any[]>([]);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editUser, setEditUser] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);
	const [providerScope, setProviderScope] = useState<'local' | 'enterprise'>('local');

  const load = (p = page, size = pageSize, provider: 'local' | 'enterprise' = providerScope) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setUsers([]);
    setTotal(0);
    api.getUsers(p, size, provider)
      .then((d: any) => {
        if (version !== requestVersion.current) return;
        setUsers(d.data || []);
        setTotal(d.total || 0);
      })
      .catch(() => {
        if (version === requestVersion.current) setLoadError('无法加载用户列表。');
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

	useEffect(() => {
		setPage(1);
		load(1, pageSize, providerScope);
	}, [providerScope]);
  useEffect(() => {
    const fallback = currentUser?.org_id
      ? [{ id: currentUser.org_id, name: currentUser.org_name || `组织 #${currentUser.org_id}` }]
      : [];
    loadAllPages<Organization>((page, size) => api.getOrganizations(page, size))
      .then((items) => {
        setOrganizations(items.length ? items : fallback);
      })
      .catch(() => setOrganizations(fallback));
  }, [currentUser?.org_id, currentUser?.org_name]);
  const roleOptions = useMemo(
    () => isPlatformAdminRole(organizationRoleForUser(currentUser))
      ? userManagementOrganizationRoleOptions
      : userManagementOrganizationRoleOptions.filter(option => option.value !== 'platform_admin'),
    [currentUser?.organization_role, currentUser?.role],
  );

  const closeDrawer = () => { setShowDrawer(false); setEditUser(null); };

  const mutationRestriction = (user: any) => {
    if (Number(user.id) === Number(currentUser?.id)) {
      return '当前登录账号不能在这里修改角色、停用或删除';
    }
    if (!isPlatformAdminRole(organizationRoleForUser(currentUser)) && isPlatformAdminRole(canonicalOrganizationRole(user.organization_role, user.role))) {
      return '组织管理员只能查看平台管理员，不能修改或删除';
    }
    return '';
  };

  const handleSubmit = async (v: any) => {
    try {
      if (editUser) {
        await api.updateUser(editUser.id, {
          organization_role: v.organization_role,
          status: v.status,
        });
        Toast.success('更新成功');
      } else {
        await api.createUser({
          org_id: Number(v.org_id),
          username: v.username,
          password: v.password,
          organization_role: v.organization_role,
          status: v.status || 'active',
        });
        Toast.success('用户创建成功');
      }
      closeDrawer();
      load(editUser ? page : 1, pageSize, providerScope);
    } catch (error) {
      const code = error instanceof ApiError ? apiErrorCode(error) : '';
      if (!editUser && code === 'local_login_id_conflict') {
        Toast.error('本地登录名不可用，请使用其他登录名');
        return;
      }
      if (!editUser && error instanceof ApiError && code.startsWith('license_error.') && error.message) {
        Toast.error(error.message);
        return;
      }
      Toast.error(editUser ? '更新失败' : '创建失败');
    }
  };

  const handleDelete = (u: any) => {
    const restriction = mutationRestriction(u);
    if (restriction) {
      Toast.warning(restriction);
      return;
    }
    Modal.confirm({
      title: '确认账号离场',
      content: `确定要让 ${u.username} 离场吗？UniRouter 将停用账号并收口会话、项目调用授权与凭据，同时保留审计身份和历史证据。`,
      onOk: async () => {
        try {
          await api.deleteUser(u.id);
          Toast.success('账号已离场');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const openCreate = () => {
		if (providerScope !== 'local') return;
    setEditUser(null);
    setShowDrawer(true);
  };

  const openEdit = (u: any) => {
    const restriction = mutationRestriction(u);
    if (restriction) {
      Toast.warning(restriction);
      return;
    }
    setEditUser(u);
    setShowDrawer(true);
  };

  useEffect(() => {
    if (!showDrawer || !formApi) return;
    formApi.reset();
    if (editUser) {
      formApi.setValues({
        org_id: editUser.org_id,
        username: editUser.username,
        organization_role: editUser.organization_role || 'member',
        status: editUser.status,
      });
    } else {
      const defaultOrgID = Number(currentUser?.org_id || organizations[0]?.id || 0) || undefined;
      formApi.setValues({
        org_id: defaultOrgID,
        organization_role: 'member',
        status: 'active',
      });
    }
  }, [currentUser?.org_id, editUser, formApi, organizations, showDrawer]);
  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    {
      title: '用户名', dataIndex: 'username',
      render: (username: string, record: any) => (
        <span>
          {username}
          {Number(record.id) === Number(currentUser?.id) && <Tag color="blue" style={{ marginLeft: 8 }}>当前账号</Tag>}
        </span>
      ),
    },
    {
      title: '所属组织',
      key: 'organization',
      render: (_: unknown, record: any) => record.org_name
        ? `${record.org_name} (#${record.org_id})`
        : (record.org_id ? `组织 #${record.org_id}` : '—'),
    },
    { title: '邮箱', dataIndex: 'email', render: (v: string) => v || '-' },
    { title: '认证来源', dataIndex: 'auth_provider', width: 110, render: (v: string) => v === 'oidc' ? '企业 OIDC' : '本地账号' },
    { title: '组织角色', dataIndex: 'organization_role', render: (v: string) => <Tag color={roleTagColor(v) as any}>{organizationRoleLabel(v)}</Tag> },
    { title: '状态', dataIndex: 'status', render: (v: string) => <StatusTag status={v}>{v === 'active' ? '启用' : v === 'disabled' ? '停用' : v}</StatusTag> },
    { title: '创建时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
    {
      title: '操作', width: 120,
      render: (_: any, record: any) => {
        const restriction = mutationRestriction(record);
        return (
          <span title={restriction || undefined}>
            <Button
              aria-label={`编辑用户 ${record.username}`}
              icon={<IconEdit />}
              size="small"
              style={{ marginRight: 8 }}
              disabled={!!restriction}
              onClick={() => openEdit(record)}
            />
            <Button
              aria-label={`离场用户 ${record.username}`}
              icon={<IconDelete />}
              size="small"
              type="danger"
              disabled={!!restriction}
              onClick={() => handleDelete(record)}
            />
          </span>
        );
      },
    },
  ];

  return (
    <div className={embedded ? 'ur-embedded-page' : 'ur-page'}>
      {embedded ? (
        <div className="ur-embedded-heading">
          <div>
            <Typography.Title heading={4} style={{ margin: 0 }}>账号与身份</Typography.Title>
            <Typography.Text type="tertiary">创建登录账号并设置组织角色；项目角色在“项目与成员”中按项目独立维护。</Typography.Text>
          </div>
			{providerScope === 'local' && <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增本地用户</Button>}
        </div>
      ) : <PageHeader
        eyebrow="访问与安全"
        title="用户管理"
        description="管理本地账号及企业 OIDC 映射账号的组织角色与状态；项目角色在项目入口独立维护。"
		actions={providerScope === 'local' ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增本地用户</Button> : undefined}
      />}

      <Surface>
		<Tabs activeKey={providerScope} onChange={(key) => setProviderScope(key as 'local' | 'enterprise')}>
			<TabPane itemKey="local" tab="本地账号 / Local User" />
			<TabPane itemKey="enterprise" tab="企业身份 / OIDC" />
		</Tabs>
		<Typography.Text type="tertiary">
			{providerScope === 'local'
				? '本页服务端只查询 auth_provider=local；可创建本地账号并管理本地生命周期。'
				: '本页服务端只查询企业身份。OIDC / 企业目录拥有登录来源与外部身份属性；UniRouter 管理已映射账号的组织角色、启停与离场，项目成员关系仍在“项目与成员”中管理。'}
		</Typography.Text>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={users}
            loading={loading}
            rowKey="id"
            empty={loadError ? (
              <EmptyState title="用户列表加载失败" description={loadError} actions={<Button type="primary" onClick={() => load()}>重新加载用户列表</Button>} minHeight={220} />
            ) : <EmptyState title="暂无用户" minHeight={220} />}
            pagination={{
              currentPage: page, pageSize, total,
              pageSizeOpts: [20, 50, 100], showSizeChanger: true,
              onPageChange: (p: number) => { setPage(p); load(p, pageSize, providerScope); },
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1, s, providerScope); },
            }}
          />
        </div>
      </Surface>

      <SideSheet
        title={editUser ? '编辑 UniRouter 权限与状态' : '新增本地用户'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          {editUser?.auth_provider !== 'local' && <Typography.Paragraph type="tertiary">
            登录来源、外部 subject、用户名与邮箱由 OIDC / 企业目录提供；下面的组织角色与账号状态是 UniRouter 本地治理事实。
          </Typography.Paragraph>}
          <Form.Select
            field="org_id"
            label="所属组织"
            disabled={!!editUser || (!isPlatformAdminRole(organizationRoleForUser(currentUser)) && organizations.length <= 1)}
            optionList={organizations.map(org => ({ label: `${org.name} (#${org.id})`, value: org.id }))}
            rules={[{ required: true, message: '请选择所属组织' }]}
            style={{ width: '100%' }}
          />
          <Form.Input
            field="username"
            label="用户名"
            disabled={!!editUser}
            rules={[{ required: true, message: '必填' }]}
            extraText={!editUser ? '本地登录名在当前部署中全局唯一，忽略大小写和首尾空白。' : undefined}
          />
          {!editUser && <PasswordField
            field="password"
            label="初始密码"
            rules={[
              { required: true, message: '必填' },
              {
                validator: (_rule, value) => !value || localPasswordCodePointCount(String(value)) >= LOCAL_PASSWORD_MIN_CODE_POINTS,
                message: '至少 12 个 Unicode 字符（按码点计）',
              },
              {
                validator: (_rule, value) => !value || localPasswordUTF8ByteCount(String(value)) <= LOCAL_PASSWORD_MAX_UTF8_BYTES,
                message: '最多 72 个 UTF-8 字节',
              },
            ]}
            visibilityResetKey={`${showDrawer ? 'open' : 'closed'}-${editUser?.id || 'create'}`}
          />}
          <Form.Select
            field="organization_role"
            label="组织角色"
            optionList={roleOptions}
            rules={[{ required: true, message: '必填' }]}
            extraText="普通账号使用“普通成员”；项目管理员、项目使用者和项目只读在具体项目中分配。"
          />
          <Form.Select
            field="status"
            label="状态"
            optionList={[{ label: '启用', value: 'active' }, { label: '禁用', value: 'disabled' }]}
            initValue="active"
          />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editUser ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
