import { useState, useEffect } from 'react';
import { Button, Table, Modal, Form, Toast, Space, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { PageHeader, StatusTag, Surface } from '../ui';

interface ChannelKey {
  id: number;
  channel_id: number;
  channel_name: string;
  api_key: string;
  key_prefix: string;
  status: string;
  created_at: string;
}

export default function ChannelKeysPage() {
  const [keys, setKeys] = useState<ChannelKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editing, setEditing] = useState<ChannelKey | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [channels, setChannels] = useState<any[]>([]);

  const load = (p = page, size = pageSize) => {
    setLoading(true);
    api.getChannelKeys(p, size)
      .then((d: any) => { setKeys(d.data || []); setTotal(d.total || 0); })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  const loadChannels = () => {
    api.getChannels(1, 100)
      .then((d: any) => {
        const list = (d.data || d.channels || []).map((c: any) => ({ label: c.name, value: c.id }));
        setChannels(list);
      })
      .catch(() => {});
  };

  useEffect(() => { load(1); loadChannels(); }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); };

  const handleSubmit = async (values: Record<string, any>) => {
    try {
      if (editing) {
        await api.updateChannelKey(editing.id, {
          channel_id: parseInt(values.channel_id),
          key: values.api_key,
        });
        Toast.success('更新成功');
      } else {
        await api.createChannelKey({
          channel_id: parseInt(values.channel_id),
          key: values.api_key,
        });
        Toast.success('创建成功');
      }
      closeDrawer();
      load();
    } catch {
      Toast.error(editing ? '更新失败' : '创建失败');
    }
  };

  const handleDelete = async (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '删除后该密钥将无法使用，确定吗？',
      onOk: async () => {
        try {
          await api.deleteChannelKey(id);
          Toast.success('已删除');
          load();
        } catch { Toast.error('删除失败'); }
      },
    });
  };

  const openEdit = (record: ChannelKey) => {
    setEditing(record);
    setShowDrawer(true);
    setTimeout(() => {
      formApi?.setValues({
        channel_id: record.channel_id,
        api_key: '',
      });
    }, 100);
  };

  const openCreate = () => {
    setEditing(null);
    setShowDrawer(true);
    formApi?.reset();
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '渠道', dataIndex: 'channel_name', render: (v: string) => v || '-' },
    { title: 'Key 前缀', dataIndex: 'key_prefix', render: (v: string) => <code>{v}***</code> },
    {
      title: '状态', dataIndex: 'status', width: 80,
      render: (v: string) => <StatusTag status={v}>{v}</StatusTag>,
    },
    { title: '创建时间', dataIndex: 'created_at', width: 180 },
    {
      title: '操作', width: 120,
      render: (_: any, record: ChannelKey) => (
        <Space>
          <Button icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Traffic"
        title="上游凭证"
        description="管理每个上游通道绑定的 provider API key。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增上游凭证</Button>}
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
              onPageChange: (p: number) => { setPage(p); load(p, pageSize); },
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1, s); },
            }}
          />
        </div>
      </Surface>

      <SideSheet
        title={editing ? '编辑上游凭证' : '新增上游凭证'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Select field="channel_id" label="所属渠道" rules={[{ required: true, message: '必填' }]}
            optionList={channels} />
          <Form.Input
            field="api_key"
            label="API Key"
            rules={[{ required: true, message: '必填' }]}
            placeholder={editing ? '输入新的上游 API Key，保存后会重置冷却状态' : 'sk-...'}
            extraText={editing ? '出于安全原因，已有密钥不会回显；请输入新的完整密钥。' : undefined}
          />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
