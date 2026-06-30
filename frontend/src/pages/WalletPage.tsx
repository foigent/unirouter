import { useState, useEffect } from 'react';
import { Typography, Card, Tag, Row, Col, Progress, Button, Modal, Input } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { Toast } from '@douyinfe/semi-ui';

const { Title, Text } = Typography;

interface WalletInfo {
  balance_cents: number;
  granted_cents: number;
  frozen_cents: number;
  threshold_cents: number;
  is_admin: boolean;
  org_name: string;
}

export default function WalletPage() {
  const [wallet, setWallet] = useState<WalletInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [modalVisible, setModalVisible] = useState(false);
  const [modalType, setModalType] = useState<'recharge' | 'adjust'>('recharge');
  const [modalAmount, setModalAmount] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const load = () => {
    setLoading(true);
    api.getWallet()
      .then((d: any) => setWallet(d))
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  if (loading) {
    return <div style={{ textAlign: 'center', padding: 100 }}><Title heading={5}>加载中...</Title></div>;
  }

  if (!wallet) {
    return <div style={{ textAlign: 'center', padding: 100 }}><Title heading={5}>无法加载钱包信息</Title></div>;
  }

  const total = wallet.balance_cents + wallet.granted_cents;
  const used = wallet.frozen_cents;
  const usagePct = total > 0 ? (used / total) * 100 : 0;
  const barColor = usagePct >= 80 ? '#F53F3F' : usagePct >= 50 ? '#F5A623' : '#05944F';

  const handleWalletOp = async () => {
    const amountCents = Math.round(parseFloat(modalAmount) * 100);
    if (isNaN(amountCents) || amountCents <= 0) {
      Toast.error('请输入有效金额');
      return;
    }
    setSubmitting(true);
    try {
      if (modalType === 'recharge') {
        await api.topUp({ amount_cents: amountCents });
      } else {
        await api.request('/wallet/adjust', {
          method: 'POST',
          body: JSON.stringify({ amount_cents: amountCents }),
        });
      }
      Toast.success(modalType === 'recharge' ? '充值成功' : '调整成功');
      setModalVisible(false);
      setModalAmount('');
      load();
    } catch (err: any) {
      Toast.error(err.message || '操作失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 24 }}>钱包管理</Title>

      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col span={6}>
          <Card className="stat-card">
            <Text type="secondary" size="small">余额</Text>
            <Title heading={3} style={{ margin: '8px 0 0', color: '#0077FA' }}>¥{(wallet.balance_cents / 100).toFixed(2)}</Title>
          </Card>
        </Col>
        <Col span={6}>
          <Card className="stat-card">
            <Text type="secondary" size="small">赠送额度</Text>
            <Title heading={3} style={{ margin: '8px 0 0', color: '#05944F' }}>¥{(wallet.granted_cents / 100).toFixed(2)}</Title>
          </Card>
        </Col>
        <Col span={6}>
          <Card className="stat-card">
            <Text type="secondary" size="small">冻结金额</Text>
            <Title heading={3} style={{ margin: '8px 0 0', color: '#F5A623' }}>¥{(wallet.frozen_cents / 100).toFixed(2)}</Title>
          </Card>
        </Col>
        <Col span={6}>
          <Card className="stat-card">
            <Text type="secondary" size="small">告警阈值</Text>
            <Title heading={3} style={{ margin: '8px 0 0', color: '#7B61FF' }}>¥{(wallet.threshold_cents / 100).toFixed(2)}</Title>
          </Card>
        </Col>
      </Row>

      <Card style={{ marginBottom: 24 }}>
        <Title heading={5} style={{ marginBottom: 16 }}>预算使用情况</Title>
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          <div style={{ flex: 1 }}>
            <Progress
              percent={usagePct}
              stroke={barColor}
              showInfo={true}
              format={(pct) => `${pct?.toFixed(1)}% 已使用`}
            />
          </div>
          <div>
            <Text type="secondary">
              总预算: ¥{(total / 100).toFixed(2)} | 已使用: ¥{(used / 100).toFixed(2)} | 剩余: ¥{((total - used) / 100).toFixed(2)}
            </Text>
          </div>
        </div>
      </Card>

      {wallet.is_admin && (
        <Card style={{ marginBottom: 24 }}>
          <Title heading={5} style={{ marginBottom: 16 }}>管理员操作</Title>
          <div style={{ display: 'flex', gap: 12 }}>
            <Button type="primary" onClick={() => { setModalType('recharge'); setModalVisible(true); }}>
              充值
            </Button>
            <Button type="tertiary" onClick={() => { setModalType('adjust'); setModalVisible(true); }}>
              调整额度
            </Button>
          </div>
        </Card>
      )}

      {wallet.org_name && (
        <Card>
          <Title heading={5} style={{ marginBottom: 16 }}>组织信息</Title>
          <Text>所属组织: <Tag color="blue">{wallet.org_name}</Tag></Text>
          <Text style={{ marginLeft: 16 }}>角色: <Tag color={wallet.is_admin ? 'red' : 'grey' as any}>{wallet.is_admin ? '管理员' : '普通用户'}</Tag></Text>
        </Card>
      )}

      <Modal
        title={modalType === 'recharge' ? '充值' : '调整额度'}
        visible={modalVisible}
        onCancel={() => { setModalVisible(false); setModalAmount(''); }}
        onOk={handleWalletOp}
        confirmLoading={submitting}
        okText={modalType === 'recharge' ? '确认充值' : '确认调整'}
      >
        <div style={{ padding: '8px 0' }}>
          <Text style={{ display: 'block', marginBottom: 8 }}>
            {modalType === 'recharge' ? '充值金额（元）' : '调整金额（元，正数增加，负数减少）'}
          </Text>
          <Input
            value={modalAmount}
            onChange={setModalAmount}
            placeholder="请输入金额，例如 100.00"
            type="number"
          />
        </div>
      </Modal>
    </div>
  );
}
