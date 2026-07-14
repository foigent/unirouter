import { useState, useEffect } from 'react';
import { Button, Table, Modal, Form, Tag, Toast, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { roleLabel, roleTagColor, userManagementRoleOptions } from '../utils/displayLabels';
import { PageHeader, PasswordField, StatusTag, Surface } from '../ui';

export default function UsersPage() {
  const [users, setUsers] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editUser, setEditUser] = useState<any>(null);
  const [formApi, setFormApi] = useState<any>(null);

  const load = (p = page, size = pageSize) => {
    setLoading(true);
    api.getUsers(p, size)
      .then((d: any) => { setUsers(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1, pageSize); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditUser(null); };

  const handleSubmit = async (v: any) => {
    try {
      if (editUser) {
        await api.updateUser(editUser.id, { role: v.role, status: v.status });
        Toast.success('更新成功');
      } else {
        await api.createUser(v);
        Toast.success('用户创建成功');
      }
      closeDrawer();
      load(editUser ? page : 1);
    } catch {
      Toast.error(editUser ? '更新失败' : '创建失败');
    }
  };

  const handleDelete = (u: any) => {
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
    setEditUser(u);
    setShowDrawer(true);
  };

  useEffect(() => {
    if (!showDrawer || !formApi) return;
    formApi.reset();
    if (editUser) {
      formApi.setValues({
        username: editUser.username,
        role: editUser.role,
        status: editUser.status,
      });
    }
  }, [editUser, formApi, showDrawer]);

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '用户名', dataIndex: 'username' },
    { title: '邮箱', dataIndex: 'email', render: (v: string) => v || '-' },
    { title: '认证来源', dataIndex: 'auth_provider', width: 110, render: (v: string) => v === 'oidc' ? '企业 OIDC' : '本地账号' },
    { title: '角色', dataIndex: 'role', render: (v: string) => <Tag color={roleTagColor(v) as any}>{roleLabel(v)}</Tag> },
    { title: '状态', dataIndex: 'status', render: (v: string) => <StatusTag status={v}>{v === 'active' ? '启用' : v === 'disabled' ? '停用' : v}</StatusTag> },
    { title: '创建时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
    {
      title: '操作', width: 120,
      render: (_: any, record: any) => (
        <span>
          <Button aria-label={`编辑用户 ${record.username}`} icon={<IconEdit />} size="small" style={{ marginRight: 8 }} onClick={() => openEdit(record)} />
          <Button aria-label={`删除用户 ${record.username}`} icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record)} />
        </span>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="访问控制"
        title="用户管理"
        description="管理本地账号及企业 OIDC 映射账号的角色与状态；企业密码和目录资料由身份提供方维护，项目角色还需要项目成员关系。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增本地用户</Button>}
      />

      <Surface>
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={users}
            loading={loading}
            rowKey="id"
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
          <Form.Input field="username" label="用户名" disabled={!!editUser} rules={[{ required: true, message: '必填' }]} />
          {!editUser && <PasswordField
            field="password"
            label="初始密码"
            rules={[{ required: true, message: '必填' }]}
            visibilityResetKey={`${showDrawer ? 'open' : 'closed'}-${editUser?.id || 'create'}`}
          />}
          <Form.Select field="role" label="角色" optionList={userManagementRoleOptions} rules={[{ required: true, message: '必填' }]} />
          <Form.Select field="status" label="状态" optionList={[{ label: '启用', value: 'active' }, { label: '禁用', value: 'disabled' }]} initValue="active" />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editUser ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
