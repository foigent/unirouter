import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Tag, Toast } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

const ROLES = [
  { label: '系统管理员 (admin)', value: 'admin' },
  { label: '组织管理员 (org_admin)', value: 'org_admin' },
  { label: '组织查看者 (org_viewer)', value: 'org_viewer' },
];

export default function UsersPage() {
  const [users, setUsers] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [editUser, setEditUser] = useState<any>(null);
  const [createApi, setCreateApi] = useState<any>(null);
  const [editApi, setEditApi] = useState<any>(null);

  const load = (p = page) => {
    setLoading(true);
    api.getUsers(p, 20)
      .then((d: any) => { setUsers(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);

  const handleCreate = async (v: any) => {
    try {
      await api.createUser(v);
      Toast.success('用户创建成功');
      setShowCreate(false);
      createApi?.reset();
      load(1);
    } catch { Toast.error('创建失败'); }
  };

  const handleUpdate = async (v: any) => {
    if (!editUser) return;
    try {
      await api.updateUser(editUser.id, v);
      Toast.success('更新成功');
      setEditUser(null);
      load();
    } catch { Toast.error('更新失败'); }
  };

  const handleDelete = (u: any) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定要删除用户 ${u.username} 吗？`,
      onOk: async () => {
        try {
          await api.deleteUser(u.id);
          Toast.success('删除成功');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '用户名', dataIndex: 'username' },
    { title: '组织', dataIndex: 'org_name', render: (v: string) => v || '-' },
    {
      title: '角色', dataIndex: 'role',
      render: (v: string) => <Tag color={v === 'admin' ? 'red' : v === 'org_admin' ? 'blue' : 'green'}>{v}</Tag>,
    },
    {
      title: '状态', dataIndex: 'status',
      render: (v: string) => <Tag color={v === 'active' ? 'green' : 'red'}>{v}</Tag>,
    },
    { title: '创建时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
    {
      title: '操作',
      render: (_: any, r: any) => (
        <div style={{ display: 'flex', gap: 8 }}>
          <Button size="small" icon={<IconEdit />} onClick={() => setEditUser(r)}>编辑</Button>
          <Button size="small" theme="solid" type="danger" icon={<IconDelete />} onClick={() => handleDelete(r)}>删除</Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>用户管理</Title>
        <Button icon={<IconPlus />} theme="solid" onClick={() => setShowCreate(true)}>新增用户</Button>
      </div>
      <Table columns={columns} dataSource={users} loading={loading} rowKey="id"
        pagination={{ currentPage: page, pageSize: 20, total, onPageChange: (p) => { setPage(p); load(p); } }} />

      <Modal title="新增用户" visible={showCreate}
        onCancel={() => { setShowCreate(false); createApi?.reset(); }}
        onOk={() => createApi?.submitForm()} okText="创建">
        <Form getFormApi={(a) => setCreateApi(a)} onSubmit={handleCreate}>
          <Form.Input field="username" label="用户名" rules={[{ required: true }]} />
          <Form.Input field="password" label="密码" type="password" rules={[{ required: true }]} />
          <Form.Select field="role" label="角色" optionList={ROLES} initValue="org_viewer" rules={[{ required: true }]} />
        </Form>
      </Modal>

      <Modal title="编辑用户" visible={!!editUser} onCancel={() => setEditUser(null)}
        onOk={() => editApi?.submitForm()} okText="保存">
        <Form getFormApi={(a) => setEditApi(a)} onSubmit={handleUpdate}
          initValues={editUser ? { role: editUser.role, status: editUser.status } : {}}>
          <Form.Select field="role" label="角色" optionList={ROLES} rules={[{ required: true }]} />
          <Form.Select field="status" label="状态" optionList={[{ label: 'active', value: 'active' }, { label: 'disabled', value: 'disabled' }]} rules={[{ required: true }]} />
        </Form>
      </Modal>
    </div>
  );
}
