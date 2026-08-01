import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Table, Modal, Form, Tag, Toast, SideSheet, Typography } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { roleLabel, roleTagColor, userManagementRoleOptions } from '../utils/displayLabels';
import { EmptyState, PageHeader, PasswordField, StatusTag, Surface } from '../ui';
import { useAuthStore } from '../store/auth';
import { isPlatformAdminRole } from '../navigation';
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

interface ProjectOption {
  id: number;
  org_id: number;
  name: string;
  status: string;
}

const projectScopedRoles = new Set(['project_admin', 'developer', 'viewer']);

const apiErrorCode = (error: ApiError) => {
  if (!error.payload || typeof error.payload !== 'object') return '';
  const code = (error.payload as { code?: unknown }).code;
  return typeof code === 'string' ? code : '';
};

export default function UsersPage({ embedded = false }: { embedded?: boolean }) {
  const currentUser = useAuthStore((state) => state.user);
  const [users, setUsers] = useState<any[]>([]);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [projects, setProjects] = useState<ProjectOption[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editUser, setEditUser] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [selectedOrgId, setSelectedOrgId] = useState<number | undefined>();
  const [selectedRole, setSelectedRole] = useState('org_viewer');
  const [selectedStatus, setSelectedStatus] = useState('active');

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setUsers([]);
    setTotal(0);
    api.getUsers(p, size)
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

  useEffect(() => { load(1, pageSize); }, []);
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
  useEffect(() => {
    if (!showDrawer || !selectedOrgId) {
      setProjects([]);
      return;
    }
    let active = true;
    loadAllPages<ProjectOption>((projectPage, size) => api.getProjects(projectPage, size, { org_id: selectedOrgId }))
      .then(items => { if (active) setProjects(items); })
      .catch(() => { if (active) setProjects([]); });
    return () => { active = false; };
  }, [selectedOrgId, showDrawer]);

  const roleOptions = useMemo(
    () => isPlatformAdminRole(currentUser?.role)
      ? userManagementRoleOptions
      : userManagementRoleOptions.filter(option => option.value !== 'platform_admin'),
    [currentUser?.role],
  );

  const closeDrawer = () => { setShowDrawer(false); setEditUser(null); };

  const projectOptions = useMemo(
    () => projects
      .filter(project => !selectedOrgId || project.org_id === Number(selectedOrgId))
      .map(project => ({
        label: `${project.name}${project.status === 'active' ? '' : `（${project.status === 'disabled' ? '已停用' : '已归档'}）`}`,
        value: project.id,
        disabled: project.status === 'archived',
      })),
    [projects, selectedOrgId],
  );

  const mutationRestriction = (user: any) => {
    if (Number(user.id) === Number(currentUser?.id)) {
      return '当前登录账号不能在这里修改角色、停用或删除';
    }
    if (!isPlatformAdminRole(currentUser?.role) && isPlatformAdminRole(user.role)) {
      return '组织管理员只能查看平台管理员，不能修改或删除';
    }
    return '';
  };

  const handleSubmit = async (v: any) => {
    try {
      if (editUser) {
        await api.updateUser(editUser.id, {
          role: v.role,
          status: v.status,
          project_ids: projectScopedRoles.has(v.role) ? (v.project_ids || []) : [],
        });
        Toast.success('更新成功');
      } else {
        await api.createUser({
          org_id: Number(v.org_id),
          username: v.username,
          password: v.password,
          role: v.role,
          status: v.status || 'active',
          project_ids: projectScopedRoles.has(v.role) ? (v.project_ids || []) : [],
        });
        Toast.success('用户创建成功');
      }
      closeDrawer();
      load(editUser ? page : 1);
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
      title: '确认删除',
      content: `确定要删除用户 ${u.username} 吗？`,
      onOk: async () => {
        try {
          await api.deleteUser(u.id);
          Toast.success('已删除');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const openCreate = () => {
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
      setSelectedOrgId(Number(editUser.org_id));
      setSelectedRole(editUser.role);
      setSelectedStatus(editUser.status);
      formApi.setValues({
        org_id: editUser.org_id,
        username: editUser.username,
        role: editUser.role,
        status: editUser.status,
        project_ids: editUser.project_ids || [],
      });
    } else {
      const defaultOrgID = Number(currentUser?.org_id || organizations[0]?.id || 0) || undefined;
      setSelectedOrgId(defaultOrgID);
      setSelectedRole('org_viewer');
      setSelectedStatus('active');
      formApi.setValues({
        org_id: defaultOrgID,
        role: 'org_viewer',
        status: 'active',
        project_ids: [],
      });
    }
  }, [currentUser?.org_id, editUser, formApi, organizations, showDrawer]);
  useEffect(() => {
    if (!showDrawer || !formApi || !projectScopedRoles.has(selectedRole)) return;
    formApi.setValue('project_ids', editUser?.project_ids || []);
  }, [editUser, formApi, selectedRole, showDrawer]);

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
    { title: '角色', dataIndex: 'role', render: (v: string) => <Tag color={roleTagColor(v) as any}>{roleLabel(v)}</Tag> },
    {
      title: '所属项目',
      dataIndex: 'project_names',
      render: (value: string, record: any) => projectScopedRoles.has(record.role) ? (value || '未分配') : '—',
    },
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
              aria-label={`删除用户 ${record.username}`}
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
            <Typography.Text type="tertiary">无需企业目录即可创建本地账号并配置角色和项目归属；账号、人员和调用主体仍是不同实体，不把 Key Owner 当成实际调用人。</Typography.Text>
          </div>
          <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增本地用户</Button>
        </div>
      ) : <PageHeader
        eyebrow="访问与安全"
        title="用户管理"
        description="管理本地账号及企业 OIDC 映射账号的角色与状态；企业密码和目录资料由身份提供方维护，项目角色还需要项目成员关系。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增本地用户</Button>}
      />}

      <Surface>
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
              onPageChange: (p: number) => { setPage(p); load(p, pageSize); },
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1, s); },
            }}
          />
        </div>
      </Surface>

      <SideSheet
        title={editUser ? '编辑用户权限' : '新增本地用户'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Select
            field="org_id"
            label="所属组织"
            disabled={!!editUser || (!isPlatformAdminRole(currentUser?.role) && organizations.length <= 1)}
            optionList={organizations.map(org => ({ label: `${org.name} (#${org.id})`, value: org.id }))}
            rules={[{ required: true, message: '请选择所属组织' }]}
            style={{ width: '100%' }}
            onChange={(value: unknown) => {
              const orgID = Number(value) || undefined;
              setSelectedOrgId(orgID);
              formApi?.setValue('project_ids', []);
            }}
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
            field="role"
            label="角色"
            optionList={roleOptions}
            rules={[{ required: true, message: '必填' }]}
            onChange={(value: unknown) => {
              const role = String(value || '');
              setSelectedRole(role);
              if (!projectScopedRoles.has(role)) formApi?.setValue('project_ids', []);
            }}
          />
          {projectScopedRoles.has(selectedRole) && (
            <Form.Select
              field="project_ids"
              label="所属项目"
              multiple
              filter
              optionList={projectOptions}
              rules={selectedStatus === 'active' ? [{ required: true, message: '启用的项目角色至少需要一个项目' }] : []}
              extraText="项目管理员可管理项目成员；开发者和只读成员只能访问所选项目。"
              style={{ width: '100%' }}
            />
          )}
          <Form.Select
            field="status"
            label="状态"
            optionList={[{ label: '启用', value: 'active' }, { label: '禁用', value: 'disabled' }]}
            initValue="active"
            onChange={(value: unknown) => setSelectedStatus(String(value || 'active'))}
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
