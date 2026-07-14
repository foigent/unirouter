import { useState, useEffect } from 'react';
import { Typography, Tag, Progress, Button, Modal, Input } from '@douyinfe/semi-ui';
import { Toast } from '@douyinfe/semi-ui';
import { IconBell, IconCoinMoney, IconGift, IconLock, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, MetricTile, MoneyText, PageHeader, Surface, formatMoney, microsRatio, parseMicros } from '../ui';

const { Text } = Typography;

interface WalletInfo {
	balance_micros: string;
	granted_balance_micros: string;
	frozen_micros: string;
	threshold_micros: string;
	total_spent_micros: string;
	currency: 'CNY';
	amount_scale: 1000000;
  low_balance_threshold_cents?: number;
  is_admin: boolean;
  org_name: string;
}

export function parseYuanToCents(value: string): number | null {
  const normalized = value.trim();
  if (!/^-?\d+(?:\.\d{1,2})?$/.test(normalized)) return null;
  const negative = normalized.startsWith('-');
  const unsigned = negative ? normalized.slice(1) : normalized;
  const [whole, fraction = ''] = unsigned.split('.');
  const cents = Number(whole) * 100 + Number(fraction.padEnd(2, '0'));
  if (!Number.isSafeInteger(cents)) return null;
  return negative ? -cents : cents;
}

function createWalletOperationID(type: 'recharge' | 'adjust') {
  const randomPart = globalThis.crypto?.randomUUID?.()
    ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return `wallet-${type}-${randomPart}`;
}

export default function WalletPage() {
  const [wallet, setWallet] = useState<WalletInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [modalVisible, setModalVisible] = useState(false);
  const [modalType, setModalType] = useState<'recharge' | 'adjust'>('recharge');
  const [modalAmount, setModalAmount] = useState('');
  const [modalDescription, setModalDescription] = useState('');
  const [clientRequestID, setClientRequestID] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const load = () => {
    setLoading(true);
    api.getWallet()
			.then((d: WalletInfo) => setWallet({ ...d, is_admin: Boolean(d.is_admin) }))
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  const openWalletModal = (type: 'recharge' | 'adjust') => {
    setModalType(type);
    setModalAmount('');
    setModalDescription('');
    setClientRequestID(createWalletOperationID(type));
    setModalVisible(true);
  };

  const closeWalletModal = () => {
    setModalVisible(false);
    setModalAmount('');
    setModalDescription('');
    setClientRequestID('');
  };

  const handleWalletOp = async () => {
    if (!wallet) return;
    const amountCents = parseYuanToCents(modalAmount);
    if (amountCents === null || (modalType === 'recharge' && amountCents <= 0) || (modalType === 'adjust' && amountCents === 0)) {
      Toast.error('请输入有效金额');
      return;
    }
    const description = modalDescription.trim();
    if (!description) {
      Toast.error('请输入操作原因');
      return;
    }
    const operationID = clientRequestID || createWalletOperationID(modalType);
    if (!clientRequestID) setClientRequestID(operationID);
    setSubmitting(true);
    try {
      if (modalType === 'recharge') {
        await api.topUp({
          amount_cents: amountCents,
          description,
          client_request_id: operationID,
        });
      } else {
        await api.adjustWallet({
          delta_cents: amountCents,
          description,
          client_request_id: operationID,
        });
      }
      Toast.success(modalType === 'recharge' ? '入账成功' : '调整成功');
      closeWalletModal();
      load();
    } catch (err: any) {
      Toast.error(err.message || '操作失败');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="ur-page">
        <EmptyState title="加载资金账户信息..." minHeight={360} />
      </div>
    );
  }

  if (!wallet) {
    return (
      <div className="ur-page">
        <EmptyState title="无法加载资金账户信息" minHeight={360} />
      </div>
    );
  }

  // The backend debits cash/grant buckets when a reservation is created.
	// frozen_micros is therefore a held-funds control total, not a second amount
  // to subtract from the already-spendable balance.
	const available = parseMicros(wallet.balance_micros) + parseMicros(wallet.granted_balance_micros);
	const held = parseMicros(wallet.frozen_micros);
	const managedTotal = available + held;
	const usagePct = microsRatio(held, managedTotal) * 100;
  const barColor = usagePct >= 80 ? '#d92d20' : usagePct >= 50 ? '#b76e00' : '#0f8f5f';

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Billing"
        title="资金账户"
        description="查看组织可用现金、可用赠送额度、冻结金额和低余额告警阈值。管理员可进行现金入账和额度调整。"
        actions={(
          <Button icon={<IconRefresh />} size="small" onClick={load}>
            刷新
          </Button>
        )}
      />

      <div className="ur-kpi-grid">
        <MetricTile
          label="可用现金"
			value={<MoneyText micros={wallet.balance_micros} strong tone="income" />}
          tone="blue"
          icon={<IconCoinMoney />}
        />
        <MetricTile
          label="可用赠送额度"
			value={<MoneyText micros={wallet.granted_balance_micros} strong tone="income" />}
          tone="green"
          icon={<IconGift />}
        />
        <MetricTile
          label="冻结金额"
			value={<MoneyText micros={wallet.frozen_micros} strong tone="warning" />}
          tone="amber"
          icon={<IconLock />}
        />
        <MetricTile
          label="告警阈值"
			value={<MoneyText micros={wallet.threshold_micros} strong />}
          tone="violet"
          icon={<IconBell />}
        />
      </div>

      <Surface title="资金占用情况" description="可用金额已经扣除了冻结中的请求；冻结金额仅用于展示仍在处理的资金占用，不会再次扣减。">
        <div className="ur-wallet-budget">
          <div>
            <Progress
              percent={usagePct}
              stroke={barColor}
              showInfo={false}
            />
          </div>
          <Text className="ur-budget-caption">
            {usagePct.toFixed(1)}% 冻结中 / 管理资金: {formatMoney(managedTotal)} / 冻结中: {formatMoney(held)} / 当前可用: {formatMoney(available)}
          </Text>
        </div>
      </Surface>

      {wallet.is_admin && (
        <Surface
          title="管理员操作"
          description="入账会增加余额；调整额度支持正负数，但调整后余额不能小于 0。"
          actions={(
            <>
              <Button type="primary" onClick={() => openWalletModal('recharge')}>
                入账
              </Button>
              <Button type="tertiary" onClick={() => openWalletModal('adjust')}>
                调整额度
              </Button>
            </>
          )}
        >
          <Text type="tertiary">操作完成后会自动刷新资金账户数据，并写入后端审计路径。</Text>
        </Surface>
      )}

      {wallet.org_name && (
        <Surface title="组织信息">
          <div className="ur-org-line">
            <Text>所属组织: <Tag color="blue">{wallet.org_name}</Tag></Text>
            <Text>角色: <Tag color={wallet.is_admin ? 'red' : 'grey' as any}>{wallet.is_admin ? '管理员' : '普通用户'}</Tag></Text>
          </div>
        </Surface>
      )}

      <Modal
        title={modalType === 'recharge' ? '资金入账' : '调整额度'}
        visible={modalVisible}
        onCancel={closeWalletModal}
        onOk={handleWalletOp}
        confirmLoading={submitting}
        okText={modalType === 'recharge' ? '确认入账' : '确认调整'}
      >
        <div style={{ padding: '8px 0' }}>
          <Text style={{ display: 'block', marginBottom: 8 }}>
            {modalType === 'recharge' ? '入账金额（元）' : '调整金额（元，正数增加，负数减少）'}
          </Text>
          <Input
            value={modalAmount}
            onChange={setModalAmount}
            placeholder="请输入金额，例如 100.00"
            type="number"
          />
          <Text style={{ display: 'block', margin: '16px 0 8px' }}>操作原因</Text>
          <Input
            value={modalDescription}
            onChange={setModalDescription}
            placeholder="请输入业务原因或工单号"
          />
          {modalType === 'adjust' && (
            <Text type="tertiary" style={{ display: 'block', marginTop: 8 }}>
              增减额会原子应用到服务器最新余额；余额不足时操作会被拒绝，不会覆盖并发消费或入账。
            </Text>
          )}
        </div>
      </Modal>
    </div>
  );
}
