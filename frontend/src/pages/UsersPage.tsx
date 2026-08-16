import { useEffect, useRef, useState } from 'react';
import { Button, Table, Modal, Form, Tag, Toast, SideSheet, Typography, Tabs, TabPane, Space } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit, IconUserGroup } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { organizationRoleLabel, roleTagColor, userManagementOrganizationRoleOptions } from '../utils/displayLabels';
import { EmptyState, PageHeader, PasswordField, StatusTag, Surface } from '../ui';
import { useAuthStore } from '../store/auth';
import { canonicalOrganizationRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import {
  LOCAL_PASSWORD_MAX_UTF8_BYTES,
  LOCAL_PASSWORD_MIN_CODE_POINTS,
  localPasswordCodePointCount,
  localPasswordUTF8ByteCount,
} from '../utils/passwordPolicy';
import { loadAllPages } from '../utils/loadAllPages';

const apiErrorCode = (error: ApiError) => {
  if (!error.payload || typeof error.payload !== 'object') return '';
  const code = (error.payload as { code?: unknown }).code;
  return typeof code === 'string' ? code : '';
};

export default function UsersPage({ embedded = false }: { embedded?: boolean }) {
  const currentUser = useAuthStore((state) => state.user);
  const customerContext = resolveCustomerContext(currentUser);
  const customerOrgID = effectiveCustomerOrgID(currentUser);
  const [users, setUsers] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editUser, setEditUser] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);
	const [accessUser, setAccessUser] = useState<any>(null);
	const [accessFormApi, setAccessFormApi] = useState<any>(null);
	const [accessProjects, setAccessProjects] = useState<any[]>([]);
	const [accessLoading, setAccessLoading] = useState(false);
	const [providerScope, setProviderScope] = useState<'local' | 'enterprise'>('local');

  const load = (p = page, size = pageSize, provider: 'local' | 'enterprise' = providerScope) => {
    const version = ++requestVersion.current;
    if (!customerOrgID) {
      setUsers([]);
      setTotal(0);
      setLoading(false);
      setLoadError('');
      return;
    }
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
	}, [customerOrgID, providerScope]);
  const roleOptions = userManagementOrganizationRoleOptions.filter(option => option.value !== 'platform_admin');

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
        if (!customerOrgID) {
          Toast.error('无法确认客户组织，未创建用户');
          return;
        }
        await api.createUser({
          org_id: customerOrgID,
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

  const openProjectAccess = async (u: any) => {
    const restriction = mutationRestriction(u);
    if (restriction) {
      Toast.warning(restriction);
      return;
    }
    setAccessUser(u);
    setAccessLoading(true);
    try {
      const projects = await loadAllPages<any>((nextPage, size) => api.getProjects(nextPage, size, { status: 'active' }));
      setAccessProjects(projects.filter(project => Number(project.org_id) === Number(customerOrgID) && project.status === 'active'));
    } catch (error) {
      setAccessProjects([]);
      Toast.error(error instanceof ApiError ? error.message : '无法加载可授权项目');
    } finally {
      setAccessLoading(false);
    }
  };

  const grantProjectAccess = async (values: Record<string, any>) => {
    if (!accessUser) return;
    setAccessLoading(true);
    try {
      await api.upsertProjectMember(Number(values.project_id), Number(accessUser.id), String(values.role || 'project_user'));
      Toast.success(`${accessUser.username} 已获得项目访问；重新登录后可在项目工作台创建自己的项目访问凭证`);
      setAccessUser(null);
      accessFormApi?.reset();
    } catch (error) {
      Toast.error(error instanceof ApiError ? error.message : '授予项目访问失败');
    } finally {
      setAccessLoading(false);
    }
  };

  useEffect(() => {
    if (!showDrawer || !formApi) return;
    formApi.reset();
    if (editUser) {
      formApi.setValues({
        username: editUser.username,
        organization_role: editUser.organization_role || 'member',
        status: editUser.status,
      });
    } else {
      formApi.setValues({
        organization_role: 'member',
        status: 'active',
      });
    }
  }, [editUser, formApi, showDrawer]);

  if (customerContext.state !== 'ready') {
    const blocker = customerContextBlockerCopy(customerContext.state);
    return (
      <div className={embedded ? 'ur-embedded-page' : 'ur-page'}>
        {!embedded ? (
          <PageHeader eyebrow="访问与安全" title="用户管理" description="本页只管理当前部署唯一客户组织中的身份。" />
        ) : null}
        <Surface>
          <EmptyState title={blocker.title} description={blocker.description} minHeight={260} />
        </Surface>
      </div>
    );
  }
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
      render: (_: unknown, record: any) => Number(record.org_id) === Number(customerOrgID)
        ? (customerContext.orgName || '当前客户范围')
        : '非当前客户范围',
    },
    { title: '邮箱', dataIndex: 'email', render: (v: string) => v || '-' },
    { title: '认证来源', dataIndex: 'auth_provider', width: 110, render: (v: string) => v === 'oidc' ? '企业 OIDC' : '本地账号' },
    { title: '组织角色', dataIndex: 'organization_role', render: (v: string) => <Tag color={roleTagColor(v) as any}>{organizationRoleLabel(v)}</Tag> },
    { title: '状态', dataIndex: 'status', render: (v: string) => <StatusTag status={v}>{v === 'active' ? '启用' : v === 'disabled' ? '停用' : v}</StatusTag> },
    { title: '创建时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
    {
      title: '操作', width: 170,
      render: (_: any, record: any) => {
        const restriction = mutationRestriction(record);
        return (
          <span title={restriction || undefined}><Space>
            <Button
              aria-label={`授予项目访问 ${record.username}`}
              icon={<IconUserGroup />}
              size="small"
              disabled={!!restriction || record.status !== 'active'}
              onClick={() => void openProjectAccess(record)}
            />
            <Button
              aria-label={`编辑用户 ${record.username}`}
              icon={<IconEdit />}
              size="small"
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
          </Space></span>
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
            <Typography.Text type="tertiary">当前客户：{customerContext.orgName || `组织 #${customerOrgID}`}。创建登录账号并设置组织角色；项目角色在“项目与成员”中按项目独立维护。</Typography.Text>
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

      <SideSheet
        title={accessUser ? `授予 ${accessUser.username} 项目访问` : '授予项目访问'}
        visible={Boolean(accessUser)}
        onCancel={() => { setAccessUser(null); accessFormApi?.reset(); }}
        width={480}
      >
        <Typography.Paragraph>
          这是让用户能够使用模型的正常入口：先加入一个项目，再由项目使用者在“项目工作台”创建自己的项目访问凭证。工作负载应用则由项目管理员在“应用与 Agent”中管理。组织角色不等于项目访问；“高级模型限制”只用于进一步收窄权限。
        </Typography.Paragraph>
        <Form getFormApi={setAccessFormApi} onSubmit={grantProjectAccess} initValues={{ role: 'project_user' }}>
          <Form.Select
            field="project_id"
            label="项目"
            loading={accessLoading}
            optionList={accessProjects.map(project => ({ value: project.id, label: project.name }))}
            rules={[{ required: true, message: '请选择项目' }]}
            emptyContent="当前没有可授权的启用项目"
          />
          <Form.Select
            field="role"
            label="项目角色"
            optionList={[
              { value: 'project_user', label: '项目使用者（可创建凭证并调用）' },
              { value: 'project_viewer', label: '项目只读（不可调用）' },
              { value: 'project_admin', label: '项目管理员（可管理成员与配置）' },
            ]}
            rules={[{ required: true, message: '请选择项目角色' }]}
            extraText="普通模型使用请选择“项目使用者”；只有确需维护项目成员和配置时才授予项目管理员。"
          />
          <div className="ur-form-actions">
            <Button onClick={() => setAccessUser(null)}>取消</Button>
            <Button type="primary" loading={accessLoading} onClick={() => accessFormApi?.submitForm()}>授予项目访问</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
