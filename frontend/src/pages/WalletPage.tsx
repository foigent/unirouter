import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Card, Button, Table, Modal, Form, Tag, Toast, Row, Col } from '@douyinfe/semi-ui';
import { IconPlus, IconDownload } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title, Text } = Typography;

interface WalletInfo {
  balance_cents: number;
  granted_balance_cents: number;
  frozen_cents: number;
  total_spent_cents: number;
  auto_freeze: boolean;
  low_balance_threshold_cents: number;
}

export default function WalletPage() {
  const [wallet, setWallet] = useState<WalletInfo | null>(null);
  const [txns, setTxns] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);

  const load = (p = page) => {
    setLoading(true);
    Promise.all([api.getWallet(), api.getWalletTransactions(p)])
      .then(([w, t]) => {
        setWallet(w);
        setTxns(t.data || []);
        setTotal(t.total || 0);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);

  const handleTopUp = async (values: Record<string, any>) => {
    try {
      await api.topUp({ amount_cents: Math.round(values.amount * 100) });
      Toast.success('充值成功');
      setShowModal(false);
      load();
    } catch {
      Toast.error('充值失败');
    }
  };

  const txnColumns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      render: (v: string) => {
        const map: Record<string, { color: string; label: string }> = {
          topup: { color: 'green', label: '充值' },
          freeze: { color: 'orange', label: '冻结' },
          settle: { color: 'blue', label: '结算' },
          refund: { color: 'purple', label: '退款' },
          adjust: { color: 'cyan', label: '调整' },
          gift: { color: 'pink', label: '赠送' },
          reclaim: { color: 'red', label: '回收' },
        };
        const t = map[v] || { color: 'grey', label: v };
        return <Tag color={t.color as any}>{t.label}</Tag>;
      },
    },
    {
      title: '金额 (元)',
      dataIndex: 'amount_cents',
      key: 'amount',
      render: (v: number) => `¥${(v / 100).toFixed(2)}`,
    },
    { title: '描述', dataIndex: 'description', key: 'desc', ellipsis: true },
    { title: '时间', dataIndex: 'created_at', key: 'time', render: (v: string) => v ? formatDateTime(v) : '' },
  ];

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 24 }}>钱包</Title>

      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col span={8}>
          <Card>
            <Text type="secondary">可用余额</Text>
            <Title heading={2} style={{ margin: '8px 0 0', color: '#05944F' }}>
              {wallet ? `¥${(wallet.balance_cents / 100).toFixed(2)}` : '—'}
            </Title>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Text type="secondary">冻结金额</Text>
            <Title heading={2} style={{ margin: '8px 0 0', color: '#F5A623' }}>
              {wallet ? `¥${(wallet.frozen_cents / 100).toFixed(2)}` : '—'}
            </Title>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Text type="secondary">赠送额度</Text>
            <Title heading={2} style={{ margin: '8px 0 0', color: '#3B82F6' }}>
              {wallet ? `¥${(wallet.granted_balance_cents / 100).toFixed(2)}` : '—'}
            </Title>
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Text type="secondary">累计消费</Text>
            <Title heading={2} style={{ margin: '8px 0 0', color: '#333' }}>
              {wallet ? `¥${(wallet.total_spent_cents / 100).toFixed(2)}` : '—'}
            </Title>
          </Card>
        </Col>
      </Row>

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={5} style={{ margin: 0 }}>交易记录</Title>
        <div style={{ display: 'flex', gap: 8 }}>
          <Button icon={<IconDownload />} onClick={() => { window.open('/api/v1/wallet/transactions/export', '_blank'); }}>导出 CSV</Button>
          <Button icon={<IconPlus />} type="primary" onClick={() => setShowModal(true)}>充值</Button>
        </div>
      </div>

      <Table
        dataSource={txns}
        columns={txnColumns}
        rowKey="id"
        loading={loading}
        pagination={{
          currentPage: page,
          pageSize: 20,
          total,
          onPageChange: (p) => { setPage(p); load(p); },
        }}
      />

      <Modal
        title="充值"
        visible={showModal}
        onCancel={() => setShowModal(false)}
        onOk={() => formApi?.submitForm()}
        okText="确认充值"
      >
        <Form getFormApi={(api) => setFormApi(api)} onSubmit={handleTopUp}>
          <Form.InputNumber
            field="amount"
            label="充值金额 (元)"
            rules={[{ required: true, message: '必填' }]}
            min={0.01}
            step={100}
            precision={2}
          />
        </Form>
      </Modal>
    </div>
  );
}
