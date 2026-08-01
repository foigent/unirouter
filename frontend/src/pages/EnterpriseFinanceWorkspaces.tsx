import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Input, Modal, Select, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, MetricTile, StatusTag, Surface, formatMoney } from '../ui';
import { formatDateTime } from '../utils/formatDate';

type ExactAmount = { value_minor: string; currency: string; scale: number };
type Scope = { kind: string; ref: string };

type FiscalPeriod = {
  org_id: number;
  period_ref: string;
  start_at: string;
  end_at: string;
  state: string;
  version: number;
  state_changed_by: string;
};

type CostCenter = {
  cost_center_ref: string;
  version: number;
  name: string;
  legal_entity_ref: string;
  owner_unit_ref?: string;
  source_ref: string;
  source_revision: string;
  valid_from: string;
};

type BudgetRecord = {
  plan: {
    budget_ref: string;
    scope: Scope;
    fiscal_period_ref: string;
    amount: ExactAmount;
    version: number;
    state: string;
    approved_by?: string;
  };
  created_by: string;
  runtime_effect: string;
};

type BudgetPosition = {
  budget_ref: string;
  budget_version: number;
  actual: { status: string; amount?: ExactAmount };
  committed: { status: string; amount?: ExactAmount };
  forecast: { status: string; amount?: ExactAmount };
  variance_minor?: string;
  coverage_state: string;
  allocation_run_ref?: string;
};

type RateCardRecord = {
  id: number;
  card: {
    rate_card_ref: string;
    provider_ref: string;
    model_ref: string;
    layer: string;
    version: number;
    state: string;
    effective_from: string;
    effective_to?: string;
    source_evidence_ref?: string;
    created_by: string;
    approved_by?: string;
    components: Array<{ meter: string; unit_quantity: string; amount: ExactAmount }>;
  };
};

type AllocationRun = {
  id: number;
  request: {
    allocation_run_ref: string;
    source_revision: string;
    rule_set_ref: string;
    rule_set_revision: string;
    supersedes_run_ref?: string;
  };
  input_hash: string;
  source_record_count: number;
  direct_count: number;
  rule_based_count: number;
  unallocated_count: number;
  conflict_count: number;
  source_total: ExactAmount;
  allocated_total: ExactAmount;
  created_at: string;
};

type ReconciliationSummary = {
  fiscal_period_ref: string;
  period_state: string;
  period_version: number;
  source_record_count: number;
  source_total: ExactAmount;
  latest_run?: AllocationRun;
  missing_record_count: number;
  coverage_state: string;
  close_ready: boolean;
  ledger_mutation: string;
  customer_debt_action: string;
  gl_export_state: string;
};

type JournalRecord = {
  journal: {
    journal_ref: string;
    version: number;
    request_ref: string;
    entry_type: string;
    reversal_of_journal_ref?: string;
    target: Scope;
    amount: ExactAmount;
    state: string;
    source_allocation_run_ref: string;
    created_by: string;
    approved_by?: string;
    gl_export_state: string;
  };
  created_at: string;
};

const periodStateLabels: Record<string, string> = {
  open: '开放',
  preliminary: '初步核对',
  reconciled: '已对账',
  closed: '已关账',
};

const rateLayerLabels: Record<string, string> = {
  provider_list: '供应商目录价',
  supplier_contracted: '供应商合同价',
  supplier_billed_effective: '供应商账单有效价',
  internal_showback: '内部展示价',
  internal_chargeback: '内部结算价',
};

function exactAmount(amount?: ExactAmount) {
  if (!amount) return '—';
  if (amount.currency === 'CNY' && Number(amount.scale) === 1_000_000) {
    return formatMoney(amount.value_minor, 6);
  }
  return `${amount.value_minor} / ${amount.scale} ${amount.currency}`;
}

function errorMessage(prefix: string, error: unknown) {
  return `${prefix}${error instanceof Error && error.message ? `：${error.message}` : ''}`;
}

export function yuanToMicrosString(value: string): string | null {
  const normalized = value.trim();
  if (!/^(?:0|[1-9][0-9]*)(?:\.[0-9]{1,6})?$/.test(normalized)) return null;
  const [whole, fraction = ''] = normalized.split('.');
  const micros = `${whole}${fraction.padEnd(6, '0')}`.replace(/^0+(?=\d)/, '');
  return micros || '0';
}

export function EnterpriseBudgetWorkspace({ orgId, canManage }: { orgId?: number; canManage: boolean }) {
  const [periods, setPeriods] = useState<FiscalPeriod[]>([]);
  const [centers, setCenters] = useState<CostCenter[]>([]);
  const [budgets, setBudgets] = useState<BudgetRecord[]>([]);
  const [positions, setPositions] = useState<BudgetPosition[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [createMode, setCreateMode] = useState<'period' | 'center' | 'budget' | ''>('');
  const [form, setForm] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const [transitionTarget, setTransitionTarget] = useState<{ period: FiscalPeriod; toState: string } | null>(null);
  const [transitionReason, setTransitionReason] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const [periodResponse, centerResponse, budgetResponse, positionResponse] = await Promise.all([
        api.getEnterpriseFiscalPeriods(orgId),
        api.getEnterpriseCostCenters(orgId),
        api.getEnterpriseBudgets(orgId),
        api.getEnterpriseBudgetPositions(orgId),
      ]) as any[];
      setPeriods(periodResponse?.data || []);
      setCenters(centerResponse?.data || []);
      setBudgets(budgetResponse?.data || []);
      setPositions(positionResponse?.data || []);
    } catch (loadError) {
      setPeriods([]);
      setCenters([]);
      setBudgets([]);
      setPositions([]);
      setError(errorMessage('企业预算控制数据加载失败', loadError));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, [orgId]);

  const positionByBudget = useMemo(() => new Map(
    positions.map(item => [`${item.budget_ref}:${item.budget_version}`, item]),
  ), [positions]);

  const approveBudget = async (record: BudgetRecord) => {
    try {
      await api.approveEnterpriseBudget(record.plan.budget_ref, {
        org_id: orgId,
        expected_version: record.plan.version,
      });
      Toast.success('预算已追加批准版本');
      await load();
    } catch (approvalError) {
      Toast.error(errorMessage('预算批准失败', approvalError));
    }
  };

  const updateForm = (key: string, value: string) => setForm(current => ({ ...current, [key]: value }));

  const openCreate = (mode: 'period' | 'center' | 'budget') => {
    setCreateMode(mode);
    setForm(mode === 'center'
      ? { version: '1', source_ref: 'local-finance-master' }
      : mode === 'budget'
        ? { scope_kind: 'cost_center', fiscal_period_ref: periods[0]?.period_ref || '' }
        : {});
  };

  const submitCreate = async () => {
    setSubmitting(true);
    try {
      if (createMode === 'period') {
        if (!form.period_ref?.trim() || !/^\d{4}-\d{2}-\d{2}$/.test(form.start_date || '') || !/^\d{4}-\d{2}-\d{2}$/.test(form.end_date || '')) {
          throw new Error('请填写期间标识以及有效的开始/结束日期');
        }
        await api.createEnterpriseFiscalPeriod({
          org_id: orgId,
          period_ref: form.period_ref.trim(),
          start_at: `${form.start_date}T00:00:00.000000Z`,
          end_at: `${form.end_date}T00:00:00.000000Z`,
        });
      } else if (createMode === 'center') {
        const version = Number(form.version);
        if (!Number.isSafeInteger(version) || version <= 0 || !form.cost_center_ref?.trim() || !form.name?.trim() || !form.legal_entity_ref?.trim() || !form.source_revision?.trim() || !/^\d{4}-\d{2}-\d{2}$/.test(form.valid_from || '')) {
          throw new Error('请完整填写成本中心、法人实体、来源修订、生效日期与连续版本');
        }
        await api.createEnterpriseCostCenterVersion({
          org_id: orgId,
          cost_center_ref: form.cost_center_ref.trim(), version,
          name: form.name.trim(), legal_entity_ref: form.legal_entity_ref.trim(),
          owner_unit_ref: form.owner_unit_ref?.trim() || '',
          source_ref: form.source_ref?.trim() || 'local-finance-master',
          source_revision: form.source_revision.trim(),
          valid_from: `${form.valid_from}T00:00:00.000000Z`, valid_to: null,
        });
      } else if (createMode === 'budget') {
        const valueMinor = yuanToMicrosString(form.amount_yuan || '');
        if (!valueMinor || !form.budget_ref?.trim() || !form.scope_ref?.trim() || !form.fiscal_period_ref?.trim()) {
          throw new Error('请填写预算标识、归属范围、财务期间与最多六位小数的金额');
        }
        await api.createEnterpriseBudgetDraft({
          org_id: orgId,
          budget_ref: form.budget_ref.trim(),
          scope: { kind: form.scope_kind || 'cost_center', ref: form.scope_ref.trim() },
          fiscal_period_ref: form.fiscal_period_ref.trim(),
          amount: { value_minor: valueMinor, currency: 'CNY', scale: 1_000_000 },
        });
      }
      Toast.success(createMode === 'period' ? '财务期间已创建' : createMode === 'center' ? '成本中心版本已追加' : '预算草稿已创建');
      setCreateMode('');
      setForm({});
      await load();
    } catch (createError) {
      Toast.error(errorMessage('创建失败', createError));
    } finally {
      setSubmitting(false);
    }
  };

  const transitionPeriod = async () => {
    if (!transitionTarget || !transitionReason.trim()) {
      Toast.error('请填写财务状态变更原因');
      return;
    }
    setSubmitting(true);
    try {
      await api.transitionEnterpriseFiscalPeriod(transitionTarget.period.period_ref, {
        org_id: orgId,
        to_state: transitionTarget.toState,
        expected_version: transitionTarget.period.version,
        reason: transitionReason.trim(),
      });
      Toast.success('财务期间状态已推进');
      setTransitionTarget(null);
      setTransitionReason('');
      await load();
    } catch (transitionError) {
      Toast.error(errorMessage('期间状态变更失败', transitionError));
    } finally {
      setSubmitting(false);
    }
  };

  const nextPeriodStates: Record<string, string[]> = {
    open: ['preliminary'],
    preliminary: ['open', 'reconciled'],
    reconciled: ['preliminary', 'closed'],
    closed: [],
  };

  const periodColumns = [
    { title: '财务期间', dataIndex: 'period_ref', width: 150 },
    {
      title: '期间范围',
      render: (_: unknown, record: FiscalPeriod) => `${formatDateTime(record.start_at)} — ${formatDateTime(record.end_at)}`,
    },
    {
      title: '状态', dataIndex: 'state', width: 130,
      render: (value: string) => <StatusTag status={value === 'closed' || value === 'reconciled' ? 'success' : value}>{periodStateLabels[value] || value}</StatusTag>,
    },
    { title: '版本', dataIndex: 'version', width: 90 },
    { title: '最后操作人', dataIndex: 'state_changed_by', width: 150 },
    {
      title: '推进', width: 180,
      render: (_: unknown, record: FiscalPeriod) => canManage ? (
        <Space>
          {(nextPeriodStates[record.state] || []).map(toState => (
            <Button key={toState} size="small" onClick={() => { setTransitionTarget({ period: record, toState }); setTransitionReason(''); }}>
              {periodStateLabels[toState] || toState}
            </Button>
          ))}
          {(nextPeriodStates[record.state] || []).length === 0 ? <Typography.Text type="tertiary">不可重开</Typography.Text> : null}
        </Space>
      ) : <Typography.Text type="tertiary">只读</Typography.Text>,
    },
  ];

  const budgetColumns = [
    { title: '预算', render: (_: unknown, record: BudgetRecord) => record.plan.budget_ref, width: 180 },
    { title: '归属范围', render: (_: unknown, record: BudgetRecord) => `${record.plan.scope.kind} / ${record.plan.scope.ref}` },
    { title: '期间', render: (_: unknown, record: BudgetRecord) => record.plan.fiscal_period_ref, width: 140 },
    { title: '金额', render: (_: unknown, record: BudgetRecord) => exactAmount(record.plan.amount), width: 150 },
    {
      title: '已分摊实际', width: 160,
      render: (_: unknown, record: BudgetRecord) => {
        const position = positionByBudget.get(`${record.plan.budget_ref}:${record.plan.version}`);
        return position?.actual?.amount ? exactAmount(position.actual.amount) : <Tag color="grey">{position?.actual?.status || '无证据'}</Tag>;
      },
    },
    {
      title: '承诺 / 预测', width: 150,
      render: () => <Tag color="grey">尚未建模</Tag>,
    },
    {
      title: '状态', render: (_: unknown, record: BudgetRecord) => (
        <Space>
          <StatusTag status={record.plan.state === 'approved' ? 'success' : record.plan.state}>{record.plan.state === 'approved' ? '已批准' : '草稿'}</StatusTag>
          <Tag color="grey">仅报表</Tag>
        </Space>
      ), width: 180,
    },
    {
      title: '操作', width: 110,
      render: (_: unknown, record: BudgetRecord) => canManage && record.plan.state === 'draft'
        ? <Button size="small" onClick={() => approveBudget(record)}>复核批准</Button>
        : <Typography.Text type="tertiary">—</Typography.Text>,
    },
  ];

  const centerColumns = [
    { title: '成本中心', render: (_: unknown, record: CostCenter) => `${record.name}（${record.cost_center_ref}）` },
    { title: '法人实体', dataIndex: 'legal_entity_ref', width: 150 },
    { title: '组织单元', dataIndex: 'owner_unit_ref', width: 150, render: (value: string) => value || '—' },
    { title: '主数据来源', render: (_: unknown, record: CostCenter) => `${record.source_ref} / ${record.source_revision}` },
    { title: '版本', dataIndex: 'version', width: 80 },
  ];

  return (
    <div>
      <Banner
        type="info"
        description="企业预算、成本中心和财务期间已使用不可变版本与精确整数金额；预算默认只影响报表，不会自行阻断模型请求，也不会改写现有 wallet 结算。真实 ERP/HCM 主数据同步仍等待客户环境。"
        style={{ marginBottom: 16 }}
      />
      {error ? <Banner type="danger" description={error} style={{ marginBottom: 16 }} /> : null}
      <div className="ur-kpi-grid">
        <MetricTile label="财务期间" value={periods.length} tone="blue" meta={`${periods.filter(item => item.state === 'closed').length} 个已关账`} />
        <MetricTile label="成本中心版本" value={centers.length} tone="violet" meta="按来源修订与生效时间读取" />
        <MetricTile label="预算计划" value={budgets.length} tone="green" meta={`${budgets.filter(item => item.plan.state === 'approved').length} 个已双人复核`} />
        <MetricTile label="请求路径影响" value="无" tone="slate" meta="默认 report_only" />
      </div>
      <Surface title="财务期间" actions={(
        <Space>
          {canManage ? <Button size="small" type="primary" onClick={() => openCreate('period')}>新建期间</Button> : null}
          <Button size="small" icon={<IconRefresh />} onClick={load}>刷新</Button>
        </Space>
      )}>
        {loading || periods.length > 0
          ? <Table rowKey="period_ref" columns={periodColumns} dataSource={periods} loading={loading} pagination={false} scroll={{ x: 880 }} />
          : <EmptyState title="尚未建立财务期间" description="可通过受控企业财务 API 创建；当前不会用自然月或模拟数据代替企业财务日历。" minHeight={180} />}
      </Surface>
      <Surface
        title="预算版本"
        description="草稿和批准版本始终分离；发起人与复核人必须不同。"
        actions={canManage ? <Button size="small" type="primary" onClick={() => openCreate('budget')} disabled={periods.length === 0}>新建预算草稿</Button> : undefined}
      >
        {loading || budgets.length > 0
          ? <Table rowKey={(row?: BudgetRecord) => row ? `${row.plan.budget_ref}:${row.plan.version}` : 'missing'} columns={budgetColumns} dataSource={budgets} loading={loading} pagination={false} scroll={{ x: 1160 }} />
          : <EmptyState title="暂无企业预算版本" description="现有项目预算继续兼容运行；企业预算不会被伪造为请求拦截策略。" minHeight={180} />}
      </Surface>
      <Surface
        title="成本中心主数据"
        description="显示当前生效版本；历史版本保持不可变。"
        actions={canManage ? <Button size="small" type="primary" onClick={() => openCreate('center')}>追加成本中心版本</Button> : undefined}
      >
        {loading || centers.length > 0
          ? <Table rowKey={(row?: CostCenter) => row ? `${row.cost_center_ref}:${row.version}` : 'missing'} columns={centerColumns} dataSource={centers} loading={loading} pagination={false} scroll={{ x: 820 }} />
          : <EmptyState title="暂无成本中心投影" description="待客户提供权威财务或组织主数据来源后再同步，不在 UI 中预造部门与成本中心。" minHeight={180} />}
      </Surface>

      <Modal
        title={createMode === 'period' ? '新建财务期间' : createMode === 'center' ? '追加成本中心版本' : '新建预算草稿'}
        visible={Boolean(createMode)}
        onCancel={() => { setCreateMode(''); setForm({}); }}
        onOk={submitCreate}
        confirmLoading={submitting}
        okText="保存"
      >
        {createMode === 'period' ? (
          <Space vertical align="start" style={{ width: '100%' }}>
            <Input aria-label="财务期间标识" value={form.period_ref || ''} onChange={value => updateForm('period_ref', value)} placeholder="例如 FY26-P08" />
            <Input aria-label="财务期间开始日期" type="date" value={form.start_date || ''} onChange={value => updateForm('start_date', value)} />
            <Input aria-label="财务期间结束日期" type="date" value={form.end_date || ''} onChange={value => updateForm('end_date', value)} />
            <Typography.Text type="tertiary">结束日期为排他边界；重叠期间会被服务端拒绝。</Typography.Text>
          </Space>
        ) : null}
        {createMode === 'center' ? (
          <Space vertical align="start" style={{ width: '100%' }}>
            <Input aria-label="成本中心标识" value={form.cost_center_ref || ''} onChange={value => updateForm('cost_center_ref', value)} placeholder="例如 CC-RND" />
            <Input aria-label="成本中心名称" value={form.name || ''} onChange={value => updateForm('name', value)} placeholder="成本中心名称" />
            <Input aria-label="法人实体标识" value={form.legal_entity_ref || ''} onChange={value => updateForm('legal_entity_ref', value)} placeholder="例如 LE-CN" />
            <Input aria-label="组织单元标识" value={form.owner_unit_ref || ''} onChange={value => updateForm('owner_unit_ref', value)} placeholder="可选：企业目录 unit_ref" />
            <Input aria-label="成本中心版本" value={form.version || '1'} onChange={value => updateForm('version', value)} placeholder="连续版本号" />
            <Input aria-label="成本中心来源" value={form.source_ref || ''} onChange={value => updateForm('source_ref', value)} placeholder="主数据来源" />
            <Input aria-label="成本中心来源修订" value={form.source_revision || ''} onChange={value => updateForm('source_revision', value)} placeholder="权威来源修订" />
            <Input aria-label="成本中心生效日期" type="date" value={form.valid_from || ''} onChange={value => updateForm('valid_from', value)} />
          </Space>
        ) : null}
        {createMode === 'budget' ? (
          <Space vertical align="start" style={{ width: '100%' }}>
            <Input aria-label="预算标识" value={form.budget_ref || ''} onChange={value => updateForm('budget_ref', value)} placeholder="例如 BUDGET-RND-P08" />
            <Select
              aria-label="预算归属类型"
              value={form.scope_kind || 'cost_center'}
              onChange={value => updateForm('scope_kind', String(value || ''))}
              optionList={[
                { value: 'legal_entity', label: '法人实体' }, { value: 'business_unit', label: '事业部' },
                { value: 'department', label: '部门' }, { value: 'cost_center', label: '成本中心' },
                { value: 'project', label: '项目' },
              ]}
              style={{ width: '100%' }}
            />
            <Input aria-label="预算归属标识" value={form.scope_ref || ''} onChange={value => updateForm('scope_ref', value)} placeholder="填写对应的稳定 ref" />
            <Select
              aria-label="预算财务期间"
              value={form.fiscal_period_ref || undefined}
              onChange={value => updateForm('fiscal_period_ref', String(value || ''))}
              optionList={periods.map(item => ({ value: item.period_ref, label: item.period_ref }))}
              style={{ width: '100%' }}
            />
            <Input aria-label="预算金额元" value={form.amount_yuan || ''} onChange={value => updateForm('amount_yuan', value)} placeholder="CNY，最多六位小数" />
            <Typography.Text type="tertiary">保存后为 report_only 草稿；必须由另一名财务角色复核。</Typography.Text>
          </Space>
        ) : null}
      </Modal>

      <Modal
        title={`财务期间推进：${transitionTarget ? periodStateLabels[transitionTarget.toState] || transitionTarget.toState : ''}`}
        visible={Boolean(transitionTarget)}
        onCancel={() => { setTransitionTarget(null); setTransitionReason(''); }}
        onOk={transitionPeriod}
        confirmLoading={submitting}
        okText="确认推进"
      >
        <Input
          aria-label="财务期间状态变更原因"
          value={transitionReason}
          onChange={setTransitionReason}
          placeholder="填写月结工单、复核结论或退回原因"
        />
        {transitionTarget?.toState === 'closed' ? (
          <Banner type="warning" description="关账前服务端会再次核对完整 Source 覆盖、零未分摊、零冲突和精确金额；失败时不会改变状态。" style={{ marginTop: 12 }} />
        ) : null}
      </Modal>
    </div>
  );
}

export function EnterpriseRateCardWorkspace({ orgId, canManage }: { orgId?: number; canManage: boolean }) {
  const [cards, setCards] = useState<RateCardRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [requestRef, setRequestRef] = useState('');
  const [snapshot, setSnapshot] = useState<any>(null);
  const [createVisible, setCreateVisible] = useState(false);
  const [form, setForm] = useState<Record<string, string>>({ layer: 'supplier_contracted', unit_quantity: '1000000' });
  const [submitting, setSubmitting] = useState(false);

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const response = await api.getEnterpriseRateCards(orgId) as any;
      setCards(response?.data || []);
    } catch (loadError) {
      setCards([]);
      setError(errorMessage('企业费率卡加载失败', loadError));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, [orgId]);

  const transition = async (record: RateCardRecord, action: 'approve' | 'activate') => {
    try {
      const payload = { org_id: orgId, expected_version: record.card.version };
      if (action === 'approve') await api.approveEnterpriseRateCard(record.card.rate_card_ref, payload);
      else await api.activateEnterpriseRateCard(record.card.rate_card_ref, payload);
      Toast.success(action === 'approve' ? '费率卡已追加批准版本' : '费率卡已激活');
      await load();
    } catch (transitionError) {
      Toast.error(errorMessage(action === 'approve' ? '费率卡批准失败' : '费率卡激活失败', transitionError));
    }
  };

  const lookupSnapshot = async () => {
    if (!requestRef.trim()) return;
    try {
      setSnapshot(await api.getEnterprisePricingSnapshot(requestRef.trim(), orgId));
    } catch (lookupError) {
      setSnapshot(null);
      Toast.error(errorMessage('计价快照查询失败', lookupError));
    }
  };

  const updateForm = (key: string, value: string) => setForm(current => ({ ...current, [key]: value }));

  const createRateCard = async () => {
    const valueMinor = yuanToMicrosString(form.amount_yuan || '');
    if (!valueMinor || !form.rate_card_ref?.trim() || !form.provider_ref?.trim() || !form.model_ref?.trim() ||
      !form.meter?.trim() || !/^[1-9][0-9]*$/.test(form.unit_quantity || '') || !/^\d{4}-\d{2}-\d{2}$/.test(form.effective_from || '')) {
      Toast.error('请完整填写费率卡、模型、meter、正整数单位、金额和生效日期');
      return;
    }
    if (form.layer === 'supplier_billed_effective' && !form.source_evidence_ref?.trim()) {
      Toast.error('供应商账单有效价必须绑定来源证据');
      return;
    }
    setSubmitting(true);
    try {
      await api.createEnterpriseRateCardDraft({
        org_id: orgId,
        rate_card_ref: form.rate_card_ref.trim(), provider_ref: form.provider_ref.trim(),
        model_ref: form.model_ref.trim(), layer: form.layer,
        effective_from: `${form.effective_from}T00:00:00.000000Z`,
        effective_to: form.effective_to ? `${form.effective_to}T00:00:00.000000Z` : null,
        source_evidence_ref: form.source_evidence_ref?.trim() || '',
        components: [{
          meter: form.meter.trim(), unit_quantity: form.unit_quantity,
          amount: { value_minor: valueMinor, currency: 'CNY', scale: 1_000_000 },
        }],
      });
      Toast.success('企业 Rate Card 草稿已创建');
      setCreateVisible(false);
      setForm({ layer: 'supplier_contracted', unit_quantity: '1000000' });
      await load();
    } catch (createError) {
      Toast.error(errorMessage('企业 Rate Card 创建失败', createError));
    } finally {
      setSubmitting(false);
    }
  };

  const columns = [
    { title: '费率卡', render: (_: unknown, record: RateCardRecord) => record.card.rate_card_ref, width: 180 },
    { title: '供应商 / 模型', render: (_: unknown, record: RateCardRecord) => `${record.card.provider_ref} / ${record.card.model_ref}` },
    { title: '经济层', render: (_: unknown, record: RateCardRecord) => rateLayerLabels[record.card.layer] || record.card.layer, width: 180 },
    { title: '组件', render: (_: unknown, record: RateCardRecord) => record.card.components.map(item => item.meter).join('、'), width: 180 },
    {
      title: '状态', render: (_: unknown, record: RateCardRecord) => (
        <StatusTag status={record.card.state === 'active' ? 'success' : record.card.state}>{record.card.state}</StatusTag>
      ), width: 100,
    },
    { title: '版本', render: (_: unknown, record: RateCardRecord) => record.card.version, width: 80 },
    {
      title: '操作', width: 180,
      render: (_: unknown, record: RateCardRecord) => canManage ? (
        <Space>
          {record.card.state === 'draft' ? <Button size="small" onClick={() => transition(record, 'approve')}>复核</Button> : null}
          {record.card.state === 'approved' ? <Button size="small" type="primary" onClick={() => transition(record, 'activate')}>激活</Button> : null}
          {!['draft', 'approved'].includes(record.card.state) ? <Typography.Text type="tertiary">—</Typography.Text> : null}
        </Space>
      ) : <Typography.Text type="tertiary">只读</Typography.Text>,
    },
  ];

  return (
    <div>
      <Banner
        type="info"
        description="企业费率卡将目录价、合同价、供应商账单有效价、内部展示价和内部结算价分层管理。当前只建立版本、审批、激活与快照证据，尚未接入现有请求结算路径，因此不会改变历史账单。"
        style={{ marginBottom: 16 }}
      />
      {error ? <Banner type="danger" description={error} style={{ marginBottom: 16 }} /> : null}
      <div className="ur-kpi-grid">
        <MetricTile label="企业费率卡" value={cards.length} tone="blue" />
        <MetricTile label="已激活" value={cards.filter(item => item.card.state === 'active').length} tone="green" />
        <MetricTile label="待复核" value={cards.filter(item => item.card.state === 'draft').length} tone="amber" />
        <MetricTile label="请求结算接入" value="未启用" tone="slate" meta="保持现有计费兼容" />
      </div>
      <Surface title="版本化 Rate Card" actions={(
        <Space>
          {canManage ? <Button size="small" type="primary" onClick={() => setCreateVisible(true)}>新建 Rate Card</Button> : null}
          <Button size="small" icon={<IconRefresh />} onClick={load}>刷新</Button>
        </Space>
      )}>
        {loading || cards.length > 0
          ? <Table rowKey={(row?: RateCardRecord) => row ? `${row.card.rate_card_ref}:${row.card.version}` : 'missing'} columns={columns} dataSource={cards} loading={loading} pagination={false} scroll={{ x: 1050 }} />
          : <EmptyState title="暂无企业 Rate Card" description="基础费率和组织倍率继续兼容；不把它们自动解释成企业合同价。" minHeight={200} />}
      </Surface>
      <Surface title="请求计价证据" description="按 request_ref 查询未来授权生产者持久化的不可变快照；此页面不能创建或重算快照。">
        <Space style={{ marginBottom: 16 }}>
          <Input value={requestRef} onChange={setRequestRef} placeholder="输入 request_ref" style={{ width: 320 }} />
          <Button icon={<IconSearch />} onClick={lookupSnapshot}>查询</Button>
        </Space>
        {snapshot ? (
          <div className="ur-kpi-grid">
            <MetricTile label="快照哈希" value={<span className="ur-mono-break">{snapshot.snapshot?.snapshot_hash}</span>} tone="blue" />
            <MetricTile label="计价组件" value={snapshot.binding_count || 0} tone="violet" />
            <MetricTile label="项目" value={snapshot.snapshot?.project_ref || '—'} tone="slate" />
            <MetricTile label="计价时间" value={formatDateTime(snapshot.snapshot?.priced_at)} tone="green" />
          </div>
        ) : <Typography.Text type="tertiary">尚未查询计价快照。</Typography.Text>}
      </Surface>
      <Modal
        title="新建企业 Rate Card 草稿"
        visible={createVisible}
        onCancel={() => { setCreateVisible(false); setForm({ layer: 'supplier_contracted', unit_quantity: '1000000' }); }}
        onOk={createRateCard}
        confirmLoading={submitting}
        okText="保存草稿"
      >
        <Space vertical align="start" style={{ width: '100%' }}>
          <Input aria-label="Rate Card 标识" value={form.rate_card_ref || ''} onChange={value => updateForm('rate_card_ref', value)} placeholder="例如 RATE-PROVIDER-MODEL-2026" />
          <Input aria-label="Rate Card 供应商" value={form.provider_ref || ''} onChange={value => updateForm('provider_ref', value)} placeholder="稳定 provider_ref" />
          <Input aria-label="Rate Card 模型" value={form.model_ref || ''} onChange={value => updateForm('model_ref', value)} placeholder="稳定 model_ref" />
          <Select
            aria-label="Rate Card 经济层"
            value={form.layer || 'supplier_contracted'}
            onChange={value => updateForm('layer', String(value || ''))}
            optionList={Object.entries(rateLayerLabels).map(([value, label]) => ({ value, label }))}
            style={{ width: '100%' }}
          />
          <Input aria-label="Rate Card meter" value={form.meter || ''} onChange={value => updateForm('meter', value)} placeholder="例如 input_tokens" />
          <Input aria-label="Rate Card 单位数量" value={form.unit_quantity || ''} onChange={value => updateForm('unit_quantity', value)} placeholder="例如 1000000" />
          <Input aria-label="Rate Card 单位金额元" value={form.amount_yuan || ''} onChange={value => updateForm('amount_yuan', value)} placeholder="CNY，最多六位小数" />
          <Input aria-label="Rate Card 生效日期" type="date" value={form.effective_from || ''} onChange={value => updateForm('effective_from', value)} />
          <Input aria-label="Rate Card 失效日期" type="date" value={form.effective_to || ''} onChange={value => updateForm('effective_to', value)} />
          <Input aria-label="Rate Card 来源证据" value={form.source_evidence_ref || ''} onChange={value => updateForm('source_evidence_ref', value)} placeholder="供应商账单有效价必填" />
          <Typography.Text type="tertiary">首个 UI 草稿支持一个 meter；更多组件可用同一受控 API 提交。保存、复核、激活分为三个版本。</Typography.Text>
        </Space>
      </Modal>
    </div>
  );
}

export function EnterpriseAllocationWorkspace({ orgId, canManage = false }: { orgId?: number; canManage?: boolean }) {
  const [periods, setPeriods] = useState<FiscalPeriod[]>([]);
  const [periodRef, setPeriodRef] = useState('');
  const [summary, setSummary] = useState<ReconciliationSummary | null>(null);
  const [runs, setRuns] = useState<AllocationRun[]>([]);
  const [journals, setJournals] = useState<JournalRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [journalVisible, setJournalVisible] = useState(false);
  const [journalForm, setJournalForm] = useState<Record<string, string>>({ entry_type: 'adjustment', target_kind: 'cost_center' });
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setLoading(true);
    setError('');
    api.getEnterpriseFiscalPeriods(orgId)
      .then((response: any) => {
        const next = response?.data || [];
        setPeriods(next);
        setPeriodRef(current => next.some((item: FiscalPeriod) => item.period_ref === current) ? current : (next[0]?.period_ref || ''));
      })
      .catch((loadError: unknown) => {
        setPeriods([]);
        setPeriodRef('');
        setError(errorMessage('企业分摊期间加载失败', loadError));
      })
      .finally(() => setLoading(false));
  }, [orgId]);

  const loadPeriod = async () => {
    if (!periodRef) {
      setSummary(null);
      setRuns([]);
      setJournals([]);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const [nextSummary, runResponse, journalResponse] = await Promise.all([
        api.getEnterpriseAllocationSummary(periodRef, orgId),
        api.getEnterpriseAllocationRuns(periodRef, orgId),
        api.getEnterpriseAdjustmentJournals(periodRef, orgId),
      ]) as any[];
      setSummary(nextSummary || null);
      setRuns(runResponse?.data || []);
      setJournals(journalResponse?.data || []);
    } catch (loadError) {
      setSummary(null);
      setRuns([]);
      setJournals([]);
      setError(errorMessage('企业分摊与关账证据加载失败', loadError));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadPeriod(); }, [periodRef, orgId]);

  const updateJournalForm = (key: string, value: string) => setJournalForm(current => ({ ...current, [key]: value }));

  const createJournal = async () => {
    const valueMinor = yuanToMicrosString(journalForm.amount_yuan || '');
    if (!valueMinor || !journalForm.journal_ref?.trim() || !journalForm.request_ref?.trim() ||
      !journalForm.target_ref?.trim() || !journalForm.source_allocation_run_ref?.trim() || !journalForm.reason?.trim()) {
      Toast.error('请完整填写流水、请求、归属、金额、来源分摊运行和原因');
      return;
    }
    if (journalForm.entry_type === 'reversal' && !journalForm.reversal_of_journal_ref?.trim()) {
      Toast.error('冲销流水必须填写原批准调整流水');
      return;
    }
    setSubmitting(true);
    try {
      await api.createEnterpriseAdjustmentJournal({
        org_id: orgId,
        journal_ref: journalForm.journal_ref.trim(), fiscal_period_ref: periodRef,
        request_ref: journalForm.request_ref.trim(), entry_type: journalForm.entry_type || 'adjustment',
        reversal_of_journal_ref: journalForm.reversal_of_journal_ref?.trim() || '',
        target: { kind: journalForm.target_kind || 'cost_center', ref: journalForm.target_ref.trim() },
        amount: { value_minor: valueMinor, currency: 'CNY', scale: 1_000_000 },
        reason: journalForm.reason.trim(),
        source_allocation_run_ref: journalForm.source_allocation_run_ref.trim(),
      });
      Toast.success('关账后调整草稿已创建');
      setJournalVisible(false);
      setJournalForm({ entry_type: 'adjustment', target_kind: 'cost_center' });
      await loadPeriod();
    } catch (createError) {
      Toast.error(errorMessage('调整流水创建失败', createError));
    } finally {
      setSubmitting(false);
    }
  };

  const approveJournal = async (record: JournalRecord) => {
    setSubmitting(true);
    try {
      await api.approveEnterpriseAdjustmentJournal(record.journal.journal_ref, {
        org_id: orgId, expected_version: record.journal.version,
      });
      Toast.success('调整流水已追加批准版本');
      await loadPeriod();
    } catch (approvalError) {
      Toast.error(errorMessage('调整流水批准失败', approvalError));
    } finally {
      setSubmitting(false);
    }
  };

  const periodOptions = useMemo(() => periods.map(item => ({
    value: item.period_ref,
    label: `${item.period_ref} · ${periodStateLabels[item.state] || item.state}`,
  })), [periods]);

  const runColumns = [
    { title: '运行', render: (_: unknown, record: AllocationRun) => record.request.allocation_run_ref, width: 170 },
    { title: 'Source 修订', render: (_: unknown, record: AllocationRun) => record.request.source_revision, width: 180 },
    { title: '规则修订', render: (_: unknown, record: AllocationRun) => `${record.request.rule_set_ref} / ${record.request.rule_set_revision}`, width: 190 },
    { title: '直接 / 规则', render: (_: unknown, record: AllocationRun) => `${record.direct_count} / ${record.rule_based_count}`, width: 120 },
    {
      title: '未决', render: (_: unknown, record: AllocationRun) => (
        <StatusTag status={record.unallocated_count + record.conflict_count === 0 ? 'success' : 'warning'}>
          {record.unallocated_count} 未分摊 / {record.conflict_count} 冲突
        </StatusTag>
      ), width: 180,
    },
    { title: 'Source 总额', render: (_: unknown, record: AllocationRun) => exactAmount(record.source_total), width: 140 },
    { title: '创建时间', render: (_: unknown, record: AllocationRun) => formatDateTime(record.created_at), width: 180 },
  ];

  const journalColumns = [
    { title: '流水', render: (_: unknown, record: JournalRecord) => record.journal.journal_ref, width: 170 },
    { title: '类型', render: (_: unknown, record: JournalRecord) => record.journal.entry_type === 'reversal' ? '冲销' : '调整', width: 90 },
    { title: '请求', render: (_: unknown, record: JournalRecord) => record.journal.request_ref, width: 180 },
    { title: '归属', render: (_: unknown, record: JournalRecord) => `${record.journal.target.kind} / ${record.journal.target.ref}` },
    { title: '金额', render: (_: unknown, record: JournalRecord) => exactAmount(record.journal.amount), width: 140 },
    { title: '状态', render: (_: unknown, record: JournalRecord) => <StatusTag status={record.journal.state === 'approved' ? 'success' : 'pending'}>{record.journal.state === 'approved' ? '已批准' : '草稿'}</StatusTag>, width: 100 },
    { title: 'GL 导出', render: () => <Tag color="grey">未配置</Tag>, width: 110 },
    {
      title: '操作', width: 110,
      render: (_: unknown, record: JournalRecord) => canManage && record.journal.state === 'draft'
        ? <Button size="small" loading={submitting} onClick={() => approveJournal(record)}>复核批准</Button>
        : <Typography.Text type="tertiary">—</Typography.Text>,
    },
  ];

  return (
    <Surface
      title="企业成本分摊与关账"
      description="分摊只改变内部报表归属，不产生客户欠款、不改请求结算；所有 Source 请求必须显式归为直接、规则、未分摊或冲突。"
      actions={(
        <Space>
          <Select
            aria-label="企业财务期间"
            placeholder="选择财务期间"
            value={periodRef || undefined}
            optionList={periodOptions}
            onChange={value => setPeriodRef(String(value || ''))}
            style={{ width: 240 }}
          />
          <Button size="small" icon={<IconRefresh />} onClick={loadPeriod}>刷新</Button>
        </Space>
      )}
    >
      <Banner
        type="info"
        description="期间有支出时，未分摊、冲突、漏项或金额不一致都会阻止关账。关账后只允许追加双人复核的调整/冲销版本；ERP/总账导出保持未配置。"
        style={{ marginBottom: 16 }}
      />
      {error ? <Banner type="danger" description={error} style={{ marginBottom: 16 }} /> : null}
      {!periodRef && !loading ? (
        <EmptyState title="尚未建立财务期间" description="先在“预算与成本中心 → 企业预算控制”建立企业财务期间。" minHeight={220} />
      ) : (
        <>
          <div className="ur-kpi-grid">
            <MetricTile label="Source 请求" value={summary?.source_record_count || 0} tone="blue" meta={exactAmount(summary?.source_total)} />
            <MetricTile label="覆盖状态" value={summary?.coverage_state || (loading ? '加载中' : '无运行')} tone={summary?.close_ready ? 'green' : 'amber'} />
            <MetricTile label="未分摊 / 冲突" value={`${summary?.latest_run?.unallocated_count || 0} / ${summary?.latest_run?.conflict_count || 0}`} tone={(summary?.latest_run?.unallocated_count || 0) + (summary?.latest_run?.conflict_count || 0) === 0 ? 'green' : 'red'} />
            <MetricTile label="关账就绪" value={summary?.close_ready ? '是' : '否'} tone={summary?.close_ready ? 'green' : 'red'} meta="结算账与客户欠款动作均为 none" />
          </div>
          <Surface title="不可变分摊运行">
            {loading || runs.length > 0
              ? <Table rowKey={(row?: AllocationRun) => row ? row.request.allocation_run_ref : 'missing'} columns={runColumns} dataSource={runs} loading={loading} pagination={false} scroll={{ x: 1160 }} />
              : <EmptyState title="尚无分摊运行" description="不会自动猜测成本中心；须用完整期间 Source 事实和明确规则修订提交受控运行。" minHeight={180} />}
          </Surface>
          <Surface
            title="关账后调整与冲销"
            description="每条调整都绑定原请求和分摊运行；冲销必须精确匹配原批准流水。"
            actions={canManage && summary?.period_state === 'closed'
              ? <Button size="small" type="primary" onClick={() => setJournalVisible(true)}>新建调整/冲销</Button>
              : undefined}
          >
            {loading || journals.length > 0
              ? <Table rowKey={(row?: JournalRecord) => row ? `${row.journal.journal_ref}:${row.journal.version}` : 'missing'} columns={journalColumns} dataSource={journals} loading={loading} pagination={false} scroll={{ x: 1000 }} />
              : <EmptyState title="暂无调整流水" description="正常状态下无需创建；关账历史不会被原地编辑。" minHeight={160} />}
          </Surface>
        </>
      )}
      <Modal
        title="新建关账后调整/冲销草稿"
        visible={journalVisible}
        onCancel={() => { setJournalVisible(false); setJournalForm({ entry_type: 'adjustment', target_kind: 'cost_center' }); }}
        onOk={createJournal}
        confirmLoading={submitting}
        okText="保存草稿"
      >
        <Space vertical align="start" style={{ width: '100%' }}>
          <Select
            aria-label="调整流水类型"
            value={journalForm.entry_type || 'adjustment'}
            onChange={value => updateJournalForm('entry_type', String(value || ''))}
            optionList={[{ value: 'adjustment', label: '调整' }, { value: 'reversal', label: '冲销' }]}
            style={{ width: '100%' }}
          />
          <Input aria-label="调整流水标识" value={journalForm.journal_ref || ''} onChange={value => updateJournalForm('journal_ref', value)} placeholder="稳定 journal_ref" />
          {journalForm.entry_type === 'reversal' ? (
            <Input aria-label="原调整流水标识" value={journalForm.reversal_of_journal_ref || ''} onChange={value => updateJournalForm('reversal_of_journal_ref', value)} placeholder="必须是已批准且尚未冲销的调整" />
          ) : null}
          <Input aria-label="调整请求标识" value={journalForm.request_ref || ''} onChange={value => updateJournalForm('request_ref', value)} placeholder="原 request_ref" />
          <Select
            aria-label="调整归属类型"
            value={journalForm.target_kind || 'cost_center'}
            onChange={value => updateJournalForm('target_kind', String(value || ''))}
            optionList={[
              { value: 'legal_entity', label: '法人实体' }, { value: 'business_unit', label: '事业部' },
              { value: 'department', label: '部门' }, { value: 'cost_center', label: '成本中心' },
              { value: 'project', label: '项目' },
            ]}
            style={{ width: '100%' }}
          />
          <Input aria-label="调整归属标识" value={journalForm.target_ref || ''} onChange={value => updateJournalForm('target_ref', value)} placeholder="稳定 scope ref" />
          <Input aria-label="调整金额元" value={journalForm.amount_yuan || ''} onChange={value => updateJournalForm('amount_yuan', value)} placeholder="CNY，最多六位小数" />
          <Select
            aria-label="来源分摊运行"
            value={journalForm.source_allocation_run_ref || undefined}
            onChange={value => updateJournalForm('source_allocation_run_ref', String(value || ''))}
            optionList={runs.map(item => ({ value: item.request.allocation_run_ref, label: item.request.allocation_run_ref }))}
            style={{ width: '100%' }}
          />
          <Input aria-label="调整原因" value={journalForm.reason || ''} onChange={value => updateJournalForm('reason', value)} placeholder="月结工单与业务原因" />
          <Typography.Text type="tertiary">调整只影响内部报表归属；不会修改客户账，也不会自动导出到 ERP/GL。</Typography.Text>
        </Space>
      </Modal>
    </Surface>
  );
}
