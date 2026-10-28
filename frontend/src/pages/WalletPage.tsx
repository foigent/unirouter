import '../i18n/finance';
import { translate as t, useI18n, labelFor } from '../i18n';
import { useRef, useState, useEffect } from 'react';
import { Banner, Typography, Tag, Progress, Button, Modal, Input, Space, Tabs } from '@douyinfe/semi-ui';
import { Toast } from '@douyinfe/semi-ui';
import { IconBell, IconCoinMoney, IconDownload, IconGift, IconLock, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, MetricTile, MoneyText, PagedTable, PageHeader, Surface, formatMoney, microsRatio, parseMicros } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { GrantCreditsPanel } from './GrantsPage';
import { isBillingRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import ProjectBudgetPanel from './ProjectBudgetPanel';
import { EnterpriseBudgetWorkspace } from './EnterpriseFinanceWorkspaces';

const { Text } = Typography;
const { TabPane } = Tabs;

interface WalletInfo {
  exists: boolean;
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
  can_top_up?: boolean;
  can_adjust?: boolean;
  can_select_org?: boolean;
  can_export_transactions?: boolean;
  org_name: string;
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
  useI18n();
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
        Toast.error(t("资金流水加载失败{v0}", { v0: String(error?.message ? `：${error.message}` : '') }));
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
      Toast.error(t("资金流水导出失败{v0}", { v0: String(error?.message ? `：${error.message}` : '') }));
    }
  };

  const columns = [
    { title: t("流水 ID"), dataIndex: 'id', width: 90 },
    { title: t("业务类型"), dataIndex: 'type', render: (value: string) => labelFor(transactionTypeLabels, value) || '-' },
    {
      title: t("变动金额"), dataIndex: 'amount_micros',
      render: (value: string) => <MoneyText micros={value || '0'} tone={parseMicros(value || '0') < 0 ? 'cost' : 'income'} />,
    },
    { title: t("说明"), dataIndex: 'description', render: (value: string) => value || '-' },
    { title: t("发生时间"), dataIndex: 'created_at', render: (value: string) => formatDateTime(value) },
  ];

  return (
    <Surface
      title={t("资金流水")}
      description={t("现金、赠送额度和请求结算统一记录在这里；金额为正表示增加，负数表示扣减或占用。")}
      actions={canExport ? <Button icon={<IconDownload />} onClick={exportCSV}>{t("导出 CSV")}</Button> : undefined}
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
  useI18n();
  const user = useAuthStore(state => state.user);
  const organizationRole = organizationRoleForUser(user);
  const platform = isPlatformAdminRole(organizationRole);
  const customerContext = resolveCustomerContext(user);
  const customerContextReady = customerContext.state === 'ready';
  const customerOrgID = effectiveCustomerOrgID(user);
  const targetOrgID = platform ? customerOrgID : undefined;
  const canManageProjectBudgets = isBillingRole(organizationRole);
  const canManageEnterpriseFinance = isBillingRole(organizationRole) || platform;
  const [wallet, setWallet] = useState<WalletInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
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

  const load = async () => {
    if (!customerContextReady) return;
    const version = ++requestVersion.current;
    setLoading(true);
    setWallet(null);
    setLoadError(null);
    try {
      const data = await api.getWallet(targetOrgID) as WalletInfo;
      if (version !== requestVersion.current) return;
      if (platform && data.org_id !== targetOrgID) {
        throw new Error(t("钱包响应未绑定当前唯一客户组织"));
      }
      const nextWallet = {
        ...data,
        exists: data.exists === true,
        is_admin: Boolean(data.is_admin),
        can_top_up: data.can_top_up === true,
        can_adjust: data.can_adjust === true,
      };
      setWallet(nextWallet);
    } catch (error: any) {
      if (version !== requestVersion.current) return;
      const message = t("结算与预算数据加载失败{v0}", { v0: String(error?.message ? `：${error.message}` : '') });
      setLoadError(error?.message || "");
      Toast.error(message);
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  };

  useEffect(() => { load(); }, [customerContextReady, targetOrgID]);

  const changeTab = (key: string) => {
    setActiveTab(key);
    const params = new URLSearchParams(window.location.search);
    if (key === 'summary') params.delete('tab'); else params.set('tab', key);
    const query = params.toString();
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
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
      Toast.error(t("请输入有效金额"));
      return;
    }
    const description = modalDescription.trim();
    if (!description) {
      Toast.error(t("请输入操作原因"));
      return;
    }
    const operationID = clientRequestID || createWalletOperationID(modalType);
    if (!clientRequestID) setClientRequestID(operationID);
    setSubmitting(true);
    try {
      if (modalType === 'recharge') {
        await api.topUp({
          org_id: targetOrgID,
          amount_cents: amountCents,
          description,
          client_request_id: operationID,
        });
      } else {
        await api.adjustWallet({
          org_id: targetOrgID,
          delta_cents: amountCents,
          description,
          client_request_id: operationID,
        });
      }
      Toast.success(modalType === 'recharge' ? t("入账成功") : t("调整成功"));
      closeWalletModal();
      load();
    } catch (err: any) {
      Toast.error(err.message || t("操作失败"));
    } finally {
      setSubmitting(false);
    }
  };

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state as Exclude<typeof customerContext.state, 'ready'>);
    return (
      <div className="ur-page ur-finance-workspace">
        <PageHeader eyebrow={t("成本与财务")} title={t("结算与预算")} description={t("结算账户和企业预算必须绑定当前部署唯一客户组织。")} />
        <Surface><EmptyState title={blocker.title} description={blocker.description} minHeight={360} /></Surface>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="ur-page ur-finance-workspace">
        <EmptyState title={t("加载结算与预算数据...")} minHeight={360} />
      </div>
    );
  }

  if (!wallet) {
    return (
      <div className="ur-page ur-finance-workspace">
        <EmptyState
          title={t("无法加载结算与预算数据")}
          description={loadError === null ? t("当前结算与预算数据不可用。") : t("结算与预算数据加载失败{v0}", {v0: loadError ? `: ${loadError}` : ""})}
          actions={<Button icon={<IconRefresh />} onClick={() => load()}>{t("重试")}</Button>}
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
  const displayedOrgID = customerOrgID || wallet.org_id;
  const displayedOrgName = customerContext.orgName || wallet.org_name;

  return (
    <div className="ur-page ur-finance-workspace">
      <PageHeader
        eyebrow={t("成本与财务")}
        title={t("结算与预算")}
        description={t("查看运行时结算、项目预算与成本中心。")}
        actions={(
          <Space>
            {platform ? <Text type="tertiary">{t("客户范围：")}{displayedOrgName || t("组织 #{v0}", { v0: String(displayedOrgID) })}</Text> : null}
            <Button icon={<IconRefresh />} size="small" onClick={() => load()}>{t("刷新")}</Button>
          </Space>
        )}
      />

      <p className="ur-boundary-note">{t('结算额度不代表银行账户或 ERP 总账；预算控制默认仅报表。')}</p>
      <details className="ur-technical-details">
        <summary>{t('结算口径与功能边界')}</summary>
        <p>{t("统一查看请求级结算、项目预算和兼容额度；现有 wallet 是运行时结算内核，不代表企业银行账户、总账或 ERP 应付事实。")}</p>
        <p>{t("请求结算、结算流水、兼容额度和项目预算继续按原语义运行；企业财务期间、成本中心和预算版本已作为独立控制层落地，默认仅报表且不冒充银行账户、ERP 总账或请求拦截策略。")}</p>
      </details>

      <div className="ur-org-line" style={{ marginBottom: 12 }}>
        <Text>{t("当前范围:")} <Tag color="blue">{displayedOrgName || t("组织 #{v0}", { v0: String(displayedOrgID) })}</Tag></Text>
        <Text>{t("权限:")} <Tag color={wallet.is_admin ? 'red' : 'grey' as any}>{wallet.is_admin ? (wallet.exists ? t("可管理资金") : t("财务管理（钱包未初始化）")) : t("只读")}</Tag></Text>
      </div>

      <Tabs activeKey={activeTab} onChange={changeTab}>
        <TabPane tab={t("结算账户")} itemKey="summary">
          {!wallet.exists ? (
            <Banner
              type="warning"
              closeIcon={null}
              description={t("当前组织尚未建立运行时结算钱包。零金额是未初始化事实，不会自动创建钱包；企业预算控制等独立财务读模型仍可查看。")}
              style={{ marginBottom: 16 }}
            />
          ) : null}
          <div className="ur-kpi-grid">
            <MetricTile label={t("结算现金余额")} value={<MoneyText micros={wallet.balance_micros} strong tone="income" />} tone="blue" icon={<IconCoinMoney />} />
            <MetricTile label={t("兼容赠送额度")} value={<MoneyText micros={wallet.granted_balance_micros} strong tone="income" />} tone="green" icon={<IconGift />} />
            <MetricTile label={t("请求预留")} value={<MoneyText micros={wallet.frozen_micros} strong tone="warning" />} tone="amber" icon={<IconLock />} />
            <MetricTile label={t("累计客户结算")} value={<MoneyText micros={wallet.total_spent_micros} strong tone="cost" />} tone="blue" icon={<IconCoinMoney />} />
            <MetricTile label={t("结算告警阈值")} value={<MoneyText micros={wallet.threshold_micros} strong />} tone="violet" icon={<IconBell />} />
          </div>

          <Surface title={t("请求级结算占用")} description={t("可用金额已经扣除了请求预留；预留金额只表示尚未完成结算的占用，不会再次从可用余额扣减。")}>
            <div className="ur-wallet-budget">
              <Progress percent={usagePct} stroke={barColor} showInfo={false} />
              <Text className="ur-budget-caption">
                {usagePct.toFixed(1)}{t("% 冻结中 / 管理资金:")} {formatMoney(managedTotal)}  {t("/ 冻结中:")} {formatMoney(held)}  {t("/ 当前可用:")} {formatMoney(available)}
              </Text>
            </div>
          </Surface>

          {wallet.exists && wallet.is_admin && (wallet.can_top_up || wallet.can_adjust) && (
            <Surface
              title={t("兼容结算操作")}
              description={t("这些操作只变更 UniRouter 结算账本：现金入账只允许正数；现金调整允许正负数，服务端会基于最新余额原子校验并记录审计。它们不是银行收款或总账过账。")}
              actions={(
                <Space>
                  {wallet.can_top_up ? <Button type="primary" onClick={() => openWalletModal('recharge')}>{t("现金入账")}</Button> : null}
                  {wallet.can_adjust ? <Button type="tertiary" onClick={() => openWalletModal('adjust')}>{t("现金调整")}</Button> : null}
                </Space>
              )}
            >
              <Text type="tertiary">{t("所有结算变动都可在“结算流水”中追溯。")}</Text>
            </Surface>
          )}
        </TabPane>
        <TabPane tab={t("结算流水")} itemKey="transactions">
          {activeTab === 'transactions' ? <WalletTransactionsPanel orgId={targetOrgID} canExport={Boolean(wallet.can_export_transactions)} /> : null}
        </TabPane>
        <TabPane tab={t("兼容额度")} itemKey="grants">
          {activeTab === 'grants' ? <GrantCreditsPanel orgId={targetOrgID} canCreate={wallet.exists && wallet.is_admin} onChanged={() => load()} /> : null}
        </TabPane>
        {canManageProjectBudgets ? (
          <TabPane tab={t("项目预算")} itemKey="budgets">
            {activeTab === 'budgets' ? <ProjectBudgetPanel /> : null}
          </TabPane>
        ) : null}
		{canManageEnterpriseFinance ? (
		  <TabPane tab={t("企业预算控制")} itemKey="enterprise">
			{activeTab === 'enterprise' ? (
			  <EnterpriseBudgetWorkspace orgId={displayedOrgID} canManage={canManageEnterpriseFinance} />
			) : null}
		  </TabPane>
		) : null}
      </Tabs>

      <Modal
        title={modalType === 'recharge' ? t("现金入账") : t("现金调整")}
        visible={modalVisible}
        onCancel={closeWalletModal}
        onOk={handleWalletOp}
        confirmLoading={submitting}
        okText={modalType === 'recharge' ? t("确认入账") : t("确认调整")}
      >
        <div style={{ padding: '8px 0' }}>
          <Text style={{ display: 'block', marginBottom: 8 }}>
            {modalType === 'recharge' ? t("入账金额（元）") : t("现金调整金额（元，正数增加，负数减少）")}
          </Text>
          <Input
            aria-label={modalType === 'recharge' ? t("入账金额（元）") : t("现金调整金额（元）")}
            value={modalAmount}
            onChange={setModalAmount}
            placeholder={t("请输入金额，例如 100.00")}
            type="number"
          />
          <Text style={{ display: 'block', margin: '16px 0 8px' }}>{t("操作原因")}</Text>
          <Input
            aria-label={t("操作原因")}
            value={modalDescription}
            onChange={setModalDescription}
            placeholder={t("请输入业务原因或工单号")}
          />
          {modalType === 'adjust' && (
            <Text type="tertiary" style={{ display: 'block', marginTop: 8 }}>

              {t("增减额会原子应用到服务器最新余额；余额不足时操作会被拒绝，不会覆盖并发消费或入账。")}
            </Text>
          )}
        </div>
      </Modal>
    </div>
  );
}
