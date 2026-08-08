import { useRef, useState, useEffect } from 'react';
import { Banner, Typography, Tag, Progress, Button, Modal, Input, Select, Space, Tabs } from '@douyinfe/semi-ui';
import { Toast } from '@douyinfe/semi-ui';
import { IconBell, IconCoinMoney, IconDownload, IconGift, IconLock, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, MetricTile, MoneyText, PagedTable, PageHeader, Surface, formatMoney, microsRatio, parseMicros } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { GrantCreditsPanel } from './GrantsPage';
import { loadAllPages } from '../utils/loadAllPages';
import { isBillingRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import ProjectBudgetPanel from './ProjectBudgetPanel';
import { EnterpriseBudgetWorkspace } from './EnterpriseFinanceWorkspaces';

const { Text } = Typography;
const { TabPane } = Tabs;

interface WalletInfo {
	org_id: number;
	balance_micros: string;
	granted_balance_micros: string;
	frozen_micros: string;
	threshold_micros: string;
	total_spent_micros: string;
	currency: 'CNY';
	amount_scale: 1000000;
  low_balance_threshold_cents?: number;
  is_admin: boolean;
  can_select_org?: boolean;
  can_export_transactions?: boolean;
  org_name: string;
}

interface OrganizationOption {
  id: number;
  name: string;
}

const transactionTypeLabels: Record<string, string> = {
  topup: '现金入账',
  adjust: '现金调整',
  grant: '赠送额度',
  reserve: '请求冻结',
  settle: '请求结算',
  cancel: '冻结释放',
};

export function WalletTransactionsPanel({ orgId, canExport }: { orgId?: number; canExport: boolean }) {
  const [rows, setRows] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const requestVersion = useRef(0);

  const load = (nextPage = page, nextPageSize = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setRows([]);
    setTotal(0);
    api.getWalletTransactions(nextPage, nextPageSize, orgId)
      .then((data: any) => {
        if (version !== requestVersion.current) return;
        setRows(data?.data || []);
        setTotal(data?.total || 0);
      })
      .catch((error: any) => {
        if (version !== requestVersion.current) return;
        Toast.error(`资金流水加载失败${error?.message ? `：${error.message}` : ''}`);
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  useEffect(() => {
    setPage(1);
    load(1, pageSize);
  }, [orgId]);

  const exportCSV = async () => {
    try {
      const csv = await api.exportWalletTransactions(orgId) as string;
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'uniroute-wallet-transactions.csv';
      link.click();
      URL.revokeObjectURL(url);
    } catch (error: any) {
      Toast.error(`资金流水导出失败${error?.message ? `：${error.message}` : ''}`);
    }
  };

  const columns = [
    { title: '流水 ID', dataIndex: 'id', width: 90 },
    { title: '业务类型', dataIndex: 'type', render: (value: string) => transactionTypeLabels[value] || value || '-' },
    {
      title: '变动金额', dataIndex: 'amount_micros',
      render: (value: string) => <MoneyText micros={value || '0'} tone={parseMicros(value || '0') < 0 ? 'cost' : 'income'} />,
    },
    { title: '说明', dataIndex: 'description', render: (value: string) => value || '-' },
    { title: '发生时间', dataIndex: 'created_at', render: (value: string) => formatDateTime(value) },
  ];

  return (
    <Surface
      title="资金流水"
      description="现金、赠送额度和请求结算统一记录在这里；金额为正表示增加，负数表示扣减或占用。"
      actions={canExport ? <Button icon={<IconDownload />} onClick={exportCSV}>导出 CSV</Button> : undefined}
    >
      <PagedTable
        columns={columns}
        dataSource={rows}
        loading={loading}
        page={page}
        pageSize={pageSize}
        total={total}
        onPageChange={(nextPage: number) => { setPage(nextPage); load(nextPage, pageSize); }}
        onPageSizeChange={(nextSize: number) => { setPageSize(nextSize); setPage(1); load(1, nextSize); }}
        rowKey="id"
      />
    </Surface>
  );
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
  const user = useAuthStore(state => state.user);
  const organizationRole = organizationRoleForUser(user);
  const canManageProjectBudgets = isBillingRole(organizationRole);
	const canManageEnterpriseFinance = isBillingRole(organizationRole) || isPlatformAdminRole(organizationRole);
  const [wallet, setWallet] = useState<WalletInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [organizations, setOrganizations] = useState<OrganizationOption[]>([]);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>();
  const requestVersion = useRef(0);
  const initialTab = new URLSearchParams(window.location.search).get('tab');
  const allowedTabs = canManageProjectBudgets
    ? ['summary', 'transactions', 'grants', 'budgets', 'enterprise']
    : canManageEnterpriseFinance
      ? ['summary', 'transactions', 'grants', 'enterprise']
      : ['summary', 'transactions', 'grants'];
  const [activeTab, setActiveTab] = useState(allowedTabs.includes(initialTab || '') ? initialTab! : 'summary');
  const [modalVisible, setModalVisible] = useState(false);
  const [modalType, setModalType] = useState<'recharge' | 'adjust'>('recharge');
  const [modalAmount, setModalAmount] = useState('');
  const [modalDescription, setModalDescription] = useState('');
  const [clientRequestID, setClientRequestID] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const load = async (orgID = selectedOrgID) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setWallet(null);
    setLoadError('');
    try {
      const data = await api.getWallet(orgID) as WalletInfo;
      if (version !== requestVersion.current) return;
      const nextWallet = { ...data, is_admin: Boolean(data.is_admin) };
      setWallet(nextWallet);
      if (nextWallet.can_select_org && organizations.length === 0) {
        try {
          const items = await loadAllPages<OrganizationOption>((page, size) => api.getOrganizations(page, size));
          if (version === requestVersion.current) setOrganizations(items);
        } catch (error: any) {
          if (version === requestVersion.current) {
            Toast.error(`组织列表加载失败${error?.message ? `：${error.message}` : ''}`);
          }
        }
      }
    } catch (error: any) {
      if (version !== requestVersion.current) return;
      const message = `预算与成本中心数据加载失败${error?.message ? `：${error.message}` : ''}`;
      setLoadError(message);
      Toast.error(message);
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const changeTab = (key: string) => {
    setActiveTab(key);
    const params = new URLSearchParams(window.location.search);
    if (key === 'summary') params.delete('tab'); else params.set('tab', key);
    const query = params.toString();
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
  };

  const changeOrganization = (value: unknown) => {
    const orgID = Number(value);
    if (!Number.isInteger(orgID) || orgID <= 0) return;
    setSelectedOrgID(orgID);
    load(orgID);
  };

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
          org_id: selectedOrgID,
          amount_cents: amountCents,
          description,
          client_request_id: operationID,
        });
      } else {
        await api.adjustWallet({
          org_id: selectedOrgID,
          delta_cents: amountCents,
          description,
          client_request_id: operationID,
        });
      }
      Toast.success(modalType === 'recharge' ? '入账成功' : '调整成功');
      closeWalletModal();
      load(selectedOrgID);
    } catch (err: any) {
      Toast.error(err.message || '操作失败');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="ur-page">
        <EmptyState title="加载预算与成本中心数据..." minHeight={360} />
      </div>
    );
  }

  if (!wallet) {
    return (
      <div className="ur-page">
        <EmptyState
          title="无法加载预算与成本中心数据"
          description={loadError || '当前结算与预算数据不可用。'}
          actions={<Button icon={<IconRefresh />} onClick={() => load(selectedOrgID)}>重试</Button>}
          minHeight={360}
        />
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
  const displayedOrgID = selectedOrgID || wallet.org_id;
  const displayedOrgName = organizations.find((item) => item.id === displayedOrgID)?.name || wallet.org_name;

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="成本与财务"
        title="预算与成本中心"
        description="统一查看请求级结算、项目预算和兼容额度；现有 wallet 是运行时结算内核，不代表企业银行账户、总账或 ERP 应付事实。"
        actions={(
          <Space>
            {wallet.can_select_org && organizations.length > 0 ? (
              <>
                <Text id="wallet-organization-label" type="tertiary">查看组织</Text>
                <Select
                  aria-labelledby="wallet-organization-label"
                  value={displayedOrgID}
                  optionList={organizations.map((item) => ({ value: item.id, label: `${item.name}（ID ${item.id}）` }))}
                  onChange={changeOrganization}
                  filter
                  style={{ width: 240 }}
                />
              </>
            ) : null}
            <Button icon={<IconRefresh />} size="small" onClick={() => load(selectedOrgID)}>刷新</Button>
          </Space>
        )}
      />

      <Banner
        type="info"
        description="请求结算、结算流水、兼容额度和项目预算继续按原语义运行；企业财务期间、成本中心和预算版本已作为独立控制层落地，默认仅报表且不冒充银行账户、ERP 总账或请求拦截策略。"
        style={{ marginBottom: 16 }}
      />

      <div className="ur-org-line" style={{ marginBottom: 12 }}>
        <Text>当前范围: <Tag color="blue">{displayedOrgName || `组织 #${displayedOrgID}`}</Tag></Text>
        <Text>权限: <Tag color={wallet.is_admin ? 'red' : 'grey' as any}>{wallet.is_admin ? '可管理资金' : '只读'}</Tag></Text>
      </div>

      <Tabs activeKey={activeTab} onChange={changeTab}>
        <TabPane tab="结算账户" itemKey="summary">
          <div className="ur-kpi-grid">
            <MetricTile label="结算现金余额" value={<MoneyText micros={wallet.balance_micros} strong tone="income" />} tone="blue" icon={<IconCoinMoney />} />
            <MetricTile label="兼容赠送额度" value={<MoneyText micros={wallet.granted_balance_micros} strong tone="income" />} tone="green" icon={<IconGift />} />
            <MetricTile label="请求预留" value={<MoneyText micros={wallet.frozen_micros} strong tone="warning" />} tone="amber" icon={<IconLock />} />
            <MetricTile label="累计客户结算" value={<MoneyText micros={wallet.total_spent_micros} strong tone="cost" />} tone="blue" icon={<IconCoinMoney />} />
            <MetricTile label="结算告警阈值" value={<MoneyText micros={wallet.threshold_micros} strong />} tone="violet" icon={<IconBell />} />
          </div>

          <Surface title="请求级结算占用" description="可用金额已经扣除了请求预留；预留金额只表示尚未完成结算的占用，不会再次从可用余额扣减。">
            <div className="ur-wallet-budget">
              <Progress percent={usagePct} stroke={barColor} showInfo={false} />
              <Text className="ur-budget-caption">
                {usagePct.toFixed(1)}% 冻结中 / 管理资金: {formatMoney(managedTotal)} / 冻结中: {formatMoney(held)} / 当前可用: {formatMoney(available)}
              </Text>
            </div>
          </Surface>

          {wallet.is_admin && (
            <Surface
              title="兼容结算操作"
              description="这些操作只变更 UniRouter 结算账本：现金入账只允许正数；现金调整允许正负数，服务端会基于最新余额原子校验并记录审计。它们不是银行收款或总账过账。"
              actions={(
                <Space>
                  <Button type="primary" onClick={() => openWalletModal('recharge')}>现金入账</Button>
                  <Button type="tertiary" onClick={() => openWalletModal('adjust')}>现金调整</Button>
                </Space>
              )}
            >
              <Text type="tertiary">所有结算变动都可在“结算流水”中追溯。</Text>
            </Surface>
          )}
        </TabPane>
        <TabPane tab="结算流水" itemKey="transactions">
          {activeTab === 'transactions' ? <WalletTransactionsPanel orgId={selectedOrgID} canExport={Boolean(wallet.can_export_transactions)} /> : null}
        </TabPane>
        <TabPane tab="兼容额度" itemKey="grants">
          {activeTab === 'grants' ? <GrantCreditsPanel orgId={selectedOrgID} canCreate={wallet.is_admin} onChanged={() => load(selectedOrgID)} /> : null}
        </TabPane>
        {canManageProjectBudgets ? (
          <TabPane tab="项目预算" itemKey="budgets">
            {activeTab === 'budgets' ? <ProjectBudgetPanel /> : null}
          </TabPane>
        ) : null}
		{canManageEnterpriseFinance ? (
		  <TabPane tab="企业预算控制" itemKey="enterprise">
			{activeTab === 'enterprise' ? (
			  <EnterpriseBudgetWorkspace orgId={displayedOrgID} canManage={canManageEnterpriseFinance} />
			) : null}
		  </TabPane>
		) : null}
      </Tabs>

      <Modal
        title={modalType === 'recharge' ? '现金入账' : '现金调整'}
        visible={modalVisible}
        onCancel={closeWalletModal}
        onOk={handleWalletOp}
        confirmLoading={submitting}
        okText={modalType === 'recharge' ? '确认入账' : '确认调整'}
      >
        <div style={{ padding: '8px 0' }}>
          <Text style={{ display: 'block', marginBottom: 8 }}>
            {modalType === 'recharge' ? '入账金额（元）' : '现金调整金额（元，正数增加，负数减少）'}
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
