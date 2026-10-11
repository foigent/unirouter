import '../i18n/integrations';
import { translate as t, useI18n } from '../i18n';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Descriptions, Form, Popconfirm, Select, SideSheet, Table, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconEyeOpened, IconPlus, IconRefresh, IconSendStroked, IconUpload } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { canonicalOrganizationRole, hasLicensedFeature, licenseRestrictionMessage } from '../navigation';
import { useAuthStore } from '../store/auth';
import { formatDateTime } from '../utils/formatDate';
import { PageHeader, StatusTag, Surface } from '../ui';
import XInsightLiveConnection from './XInsightLiveConnection';

const { Text } = Typography;
const { TabPane } = Tabs;

type TabKey = 'live' | 'approvals' | 'ledger';

interface ExchangeItem {
  id: number;
  approval_mode: 'exact_request' | 'controlled_scenario';
  request_id?: string | null;
  scenario_label: string;
  source_instance: string;
  data_classes: string[];
  safety_profile: string;
  approval_status: 'pending' | 'approved' | 'unavailable';
  reason: string;
  authorized_operator?: string | null;
  retention_until?: string | null;
  approved_at?: string | null;
  created_at: string;
}

interface ExchangeRecord {
  id: number;
  exchange_id: string;
  downstream_system: string;
  source_instance: string;
  schema_version: string;
  approval_manifest_sha256: string;
  artifact_sha256: string;
  artifact_ref: string;
  workflow_status: string;
  receipt_status: string;
  request_count: number;
  input_event_count: number;
  item_ids: number[];
  delivery_count: number;
  last_delivery_at?: string | null;
  warning_codes: string[];
  import_job_id?: string | null;
  deep_link?: string | null;
  retention_until?: string | null;
  generated_at: string;
  artifact_deleted_at?: string | null;
}

interface ExchangeActivity {
  members: Array<{ id: number; request_id?: string; scenario_label: string; approval_status: string }>;
  deliveries: Array<{ id: number; action: string; handoff_ref: string; note: string; delivered_at: string }>;
  receipts: Array<{ id: number; receipt_status: string; input_events: number; accepted: number; rerun_duplicate_noop: number; rejected: number; warning_codes: string[]; checked_at: string }>;
}

function parseIDs(value?: string) {
  return [...new Set((value || '').split(',').map(item => Number(item.trim())).filter(id => Number.isInteger(id) && id > 0))];
}

function optionalISO(value?: string) {
  if (!value) return undefined;
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) throw new Error(t("时间格式无效"));
  return parsed.toISOString();
}

export function receiptPayload(raw: string) {
  const receipt = JSON.parse(raw);
  if (Object.prototype.hasOwnProperty.call(receipt, 'import_job_id') || Object.prototype.hasOwnProperty.call(receipt, 'deep_link')) {
    throw new Error(t("X-Insight 尚未提供 durable import job 或 deep link 合同"));
  }
  const result = receipt?.result || {};
  return {
    exchange_id: receipt?.exchange_id,
    schema_version: receipt?.schema_version,
    source_system: receipt?.source?.system,
    source_instance: receipt?.source?.instance,
    artifact_schema_version: receipt?.artifact?.schema_version,
    artifact_sha256: receipt?.artifact?.sha256,
    approval_manifest_sha256: receipt?.artifact?.approval_manifest_sha256 || '',
    result_status: result.status,
    request_ids: Number(result.request_ids || 0),
    input_events: Number(result.input_events || 0),
    accepted: Number(result.accepted || 0),
    duplicate_noop: Number(result.duplicate_noop || 0),
    rerun_duplicate_noop: Number(result.rerun_duplicate_noop || 0),
    rejected: Number(result.rejected || 0),
    quarantined: Number(result.quarantined || 0),
    capture_gap_events: Number(result.capture_gap_events || 0),
    persisted_inserted: Number(result.persisted_inserted || 0),
    persisted_duplicate_noop: Number(result.persisted_duplicate_noop || 0),
    persistence_conflicts: Number(result.persistence_conflicts || 0),
    readback_events: Number(result.readback_events || 0),
    readback_capture_gaps: Number(result.readback_capture_gaps || 0),
    warning_codes: Array.isArray(receipt?.warning_codes) ? receipt.warning_codes : [],
    checked_at: receipt?.checked_at,
  };
}

export default function DataExchangePage() {
  useI18n();
  const user = useAuthStore(state => state.user);
  if (canonicalOrganizationRole(user?.organization_role, user?.role) !== 'org_admin') return <HistoricalDataExchangePage />;
  const licensed = hasLicensedFeature(user?.license_access, 'data_exchange');
  const restriction = user?.license_access && !licensed ? licenseRestrictionMessage(user.license_access, 'data_exchange') : '';
  return <div className="ur-page ur-page-wide ur-integration-workspace"><PageHeader eyebrow={t("系统与集成")} title={t("X-Insight 接入")} description={t("确认两端设置、供数范围和后续管理权限；正文仅按明确授权同步。")} /><Surface><XInsightLiveConnection expansionAllowed={licensed} expansionRestriction={restriction} /></Surface></div>;
}

function HistoricalDataExchangePage() {
  useI18n();
  const licenseAccess = useAuthStore((state) => state.user?.license_access);
  const dataExchangeLicensed = hasLicensedFeature(licenseAccess, 'data_exchange');
  const dataExchangeRestriction = licenseAccess && !dataExchangeLicensed
    ? licenseRestrictionMessage(licenseAccess, 'data_exchange')
    : '';
  const [activeTab, setActiveTab] = useState<TabKey>('live');
  const [items, setItems] = useState<ExchangeItem[]>([]);
  const [itemStatus, setItemStatus] = useState('');
  const [itemsLoading, setItemsLoading] = useState(false);
  const [exchanges, setExchanges] = useState<ExchangeRecord[]>([]);
  const [exchangesLoading, setExchangesLoading] = useState(false);
  const [candidateOpen, setCandidateOpen] = useState(false);
  const [approvalItem, setApprovalItem] = useState<ExchangeItem | null>(null);
  const [exchangeOpen, setExchangeOpen] = useState(false);
  const [deliveryExchange, setDeliveryExchange] = useState<ExchangeRecord | null>(null);
  const [receiptExchange, setReceiptExchange] = useState<ExchangeRecord | null>(null);
  const [previewExchange, setPreviewExchange] = useState<ExchangeRecord | null>(null);
  const [activity, setActivity] = useState<ExchangeActivity | null>(null);
  const [activityLoading, setActivityLoading] = useState(false);

  const loadItems = useCallback(async () => {
    setItemsLoading(true);
    try {
      const data = await api.getDataExchangeItems(1, 100, itemStatus || undefined);
      setItems((data?.data || []) as ExchangeItem[]);
    } catch (err: any) {
      Toast.error(t("审批候选加载失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    } finally {
      setItemsLoading(false);
    }
  }, [itemStatus]);

  const loadExchanges = useCallback(async () => {
    setExchangesLoading(true);
    try {
      const data = await api.getDataExchanges(1, 100);
      setExchanges((data?.data || []) as ExchangeRecord[]);
    } catch (err: any) {
      Toast.error(t("交换账本加载失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    } finally {
      setExchangesLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeTab === 'approvals') loadItems();
  }, [activeTab, loadItems]);
  useEffect(() => {
    if (activeTab === 'ledger') loadExchanges();
  }, [activeTab, loadExchanges]);

  const approvedItemIDs = useMemo(() => items.filter(item => item.approval_status === 'approved' && item.request_id).map(item => item.id), [items]);

  const saveCandidate = async (values: any) => {
    try {
      await api.createDataExchangeItem({
        approval_mode: values.approval_mode,
        request_id: values.request_id || '',
        scenario_label: values.scenario_label,
        source_instance: values.source_instance,
        data_classes: values.data_classes || [],
        reason: values.reason || '',
        authorized_operator: values.authorized_operator || '',
        retention_until: optionalISO(values.retention_until),
      });
      Toast.success(t("审批候选已创建"));
      setCandidateOpen(false);
      loadItems();
    } catch (err: any) {
      Toast.error(t("创建失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    }
  };

  const saveApproval = async (values: any) => {
    if (!approvalItem) return;
    try {
      await api.updateDataExchangeItem(approvalItem.id, {
        approval_status: values.approval_status,
        reason: values.reason || '',
        request_id: values.request_id || '',
      });
      Toast.success(t("审批状态已更新"));
      setApprovalItem(null);
      loadItems();
    } catch (err: any) {
      Toast.error(t("更新失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    }
  };

  const saveExchange = async (values: any) => {
    try {
      const itemIDs = parseIDs(values.item_ids);
      if (itemIDs.length === 0) throw new Error(t("至少选择一个已批准候选"));
      await api.createDataExchange({
        exchange_id: values.exchange_id,
        source_instance: values.source_instance,
        schema_version: values.schema_version,
        approval_manifest_sha256: values.approval_manifest_sha256,
        artifact_sha256: values.artifact_sha256,
        artifact_ref: values.artifact_ref,
        item_ids: itemIDs,
        request_count: Number(values.request_count || 0),
        input_event_count: Number(values.input_event_count || 0),
        generated_at: optionalISO(values.generated_at),
        retention_until: optionalISO(values.retention_until),
      });
      Toast.success(t("交换制品已登记"));
      setExchangeOpen(false);
      setActiveTab('ledger');
      loadExchanges();
    } catch (err: any) {
      Toast.error(t("登记失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    }
  };

  const saveDelivery = async (values: any) => {
    if (!deliveryExchange) return;
    try {
      await api.createDataExchangeDelivery(deliveryExchange.id, {
        action: values.action,
        handoff_ref: values.handoff_ref,
        note: values.note || '',
        delivered_at: optionalISO(values.delivered_at),
      });
      Toast.success(values.action === 'redelivered' ? t("重投已登记") : t("交付已登记"));
      setDeliveryExchange(null);
      loadExchanges();
    } catch (err: any) {
      Toast.error(t("登记失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    }
  };

  const saveReceipt = async (values: any) => {
    if (!receiptExchange) return;
    try {
      const payload = receiptPayload(values.receipt_json || '');
      if (payload.exchange_id !== receiptExchange.exchange_id) throw new Error(t("回执 exchange_id 与当前账本不一致"));
      await api.createDataExchangeReceipt(receiptExchange.id, payload);
      Toast.success(t("X-Insight 回执已登记"));
      setReceiptExchange(null);
      loadExchanges();
    } catch (err: any) {
      Toast.error(t("回执登记失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    }
  };

  const markDeleted = async (record: ExchangeRecord) => {
    try {
      await api.markDataExchangeArtifactDeleted(record.id);
      Toast.success(t("账本已标记制品删除；未执行文件系统删除"));
      loadExchanges();
    } catch (err: any) {
      Toast.error(t("更新失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    }
  };

  const openPreview = async (record: ExchangeRecord) => {
    setPreviewExchange(record);
    setActivity(null);
    setActivityLoading(true);
    try {
      setActivity(await api.getDataExchangeActivity(record.id) as ExchangeActivity);
    } catch (err: any) {
      Toast.error(t("交换历史加载失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    } finally {
      setActivityLoading(false);
    }
  };

  const itemColumns = [
    { title: 'ID', dataIndex: 'id', width: 70 },
    { title: t("模式"), dataIndex: 'approval_mode', width: 132, render: (v: string) => v === 'exact_request' ? t("精确请求") : t("受控场景") },
    { title: 'Request ID', dataIndex: 'request_id', width: 180, render: (v: string) => v ? <Text code>{v}</Text> : '-' },
    { title: t("场景"), dataIndex: 'scenario_label', width: 150 },
    { title: t("来源实例"), dataIndex: 'source_instance', width: 190, ellipsis: true },
    { title: t("数据类"), dataIndex: 'data_classes', width: 220, render: (v: string[]) => (v || []).map(item => <Tag key={item} size="small">{item}</Tag>) },
    { title: t("状态"), dataIndex: 'approval_status', width: 110, render: (v: string) => <StatusTag status={v}>{v === 'approved' ? t("已批准") : v === 'unavailable' ? t("不可用") : t("待审批")}</StatusTag> },
    { title: t("原因"), dataIndex: 'reason', ellipsis: true, render: (v: string) => v || '-' },
    { title: t("操作"), width: 86, render: (_: unknown, row: ExchangeItem) => <Button size="small" onClick={() => setApprovalItem(row)}>{t("审批")}</Button> },
  ];

  const exchangeColumns = [
    { title: 'Exchange ID', dataIndex: 'exchange_id', width: 230, render: (v: string) => <Text code>{v}</Text> },
    { title: t("源状态"), dataIndex: 'workflow_status', width: 120, render: (v: string) => <StatusTag status={v}>{v}</StatusTag> },
    { title: t("X-Insight 回执"), dataIndex: 'receipt_status', width: 140, render: (v: string) => <StatusTag status={v}>{v}</StatusTag> },
    { title: t("请求 / 事件"), width: 120, render: (_: unknown, row: ExchangeRecord) => `${row.request_count} / ${row.input_event_count}` },
    { title: t("投递"), dataIndex: 'delivery_count', width: 82 },
    { title: t("制品引用"), dataIndex: 'artifact_ref', ellipsis: true },
    { title: t("生成时间"), dataIndex: 'generated_at', width: 180, render: (v: string) => formatDateTime(v) },
    {
      title: t("操作"), width: 210, render: (_: unknown, row: ExchangeRecord) => (
        <div style={{ display: 'flex', gap: 4 }}>
          <Button icon={<IconEyeOpened />} size="small" aria-label={t("查看交换")} onClick={() => openPreview(row)} />
          {row.workflow_status !== 'artifact_deleted' && <Button icon={<IconSendStroked />} size="small" aria-label={t("登记交付")} onClick={() => setDeliveryExchange(row)} />}
          <Button icon={<IconUpload />} size="small" aria-label={t("登记回执")} onClick={() => setReceiptExchange(row)} />
          {row.workflow_status !== 'artifact_deleted' && (
            <Popconfirm title={t("仅标记账本状态，不删除服务器文件。确认继续？")} onConfirm={() => markDeleted(row)}>
              <Button icon={<IconDelete />} size="small" type="danger" aria-label={t("标记制品删除")} />
            </Popconfirm>
          )}
        </div>
      ),
    },
  ];

  return (
    <div className="ur-page ur-page-wide ur-integration-workspace">
      <PageHeader
        eyebrow={t("系统与集成")}
        title={t("X-Insight 数据交换")}
        description={t("I-01 完成请求摘要通道是 UniRouter → X-Insight 的 metadata feed，不含原始正文；正文设置另行管理。旧审批与制品账本保留为独立的历史人工流程。")}
        actions={(
          <Button
            icon={<IconRefresh />}
            disabled={!dataExchangeLicensed}
            onClick={() => { loadItems(); loadExchanges(); }}
          >

            {t("刷新历史账本")}
          </Button>
        )}
      />

      <Surface>
        <div className="ur-chip-row" style={{ marginBottom: 12 }}>
          <Tag color="blue">{t("实时连接：私有 pull feed")}</Tag>
          <Tag color="green">I-01 metadata only</Tag>
          <Tag>{t("I-01 无原始 prompt/response")}</Tag>
          <Tag>feed-only service credential</Tag>
          <Tag>{t("无原始 IP")}</Tag>
          <Tag>{t("无原始上游错误")}</Tag>
        </div>
        {!dataExchangeLicensed && (
          <div style={{ padding: 12, marginBottom: 12, border: '1px solid var(--semi-color-warning-light-default)', borderRadius: 8 }}>
            <Tag color="orange">{t("Data Exchange 授权当前不可用")}</Tag>
            <Text type="tertiary">
              {dataExchangeRestriction}  {t("实时连接的状态、暂停、取消与不可逆撤销仍可使用；扩张动作和历史人工流程已锁定。")}
            </Text>
          </div>
        )}
        <Tabs
          activeKey={activeTab}
          onChange={key => {
            const next = key as TabKey;
            if (dataExchangeLicensed || next === 'live') setActiveTab(next);
          }}
        >
          <TabPane tab={t("实时连接")} itemKey="live">
            <XInsightLiveConnection
              expansionAllowed={dataExchangeLicensed}
              expansionRestriction={dataExchangeRestriction}
            />
          </TabPane>
          <TabPane
            tab={dataExchangeLicensed ? t("历史人工审批") : t("历史人工审批（授权锁定）")}
            itemKey="approvals"
            disabled={!dataExchangeLicensed}
          >
            <div className="ur-toolbar">
              <Select
                value={itemStatus || undefined}
                placeholder={t("审批状态")}
                style={{ width: 160 }}
                onChange={value => setItemStatus((value as string) || '')}
                optionList={[
                  { label: t("待审批"), value: 'pending' },
                  { label: t("已批准"), value: 'approved' },
                  { label: t("不可用"), value: 'unavailable' },
                ]}
              />
              <div style={{ flex: 1 }} />
              <Button icon={<IconPlus />} type="primary" onClick={() => setCandidateOpen(true)}>{t("新增候选")}</Button>
              <Button disabled={approvedItemIDs.length === 0} onClick={() => setExchangeOpen(true)}>{t("登记交换制品")}</Button>
            </div>
            <div className="ur-table-shell">
              <Table columns={itemColumns} dataSource={items} rowKey="id" loading={itemsLoading} pagination={false} scroll={{ x: 1320 }} />
            </div>
          </TabPane>
          <TabPane
            tab={dataExchangeLicensed ? t("历史交换账本") : t("历史交换账本（授权锁定）")}
            itemKey="ledger"
            disabled={!dataExchangeLicensed}
          >
            <div className="ur-table-shell">
              <Table columns={exchangeColumns} dataSource={exchanges} rowKey="id" loading={exchangesLoading} pagination={false} scroll={{ x: 1320 }} />
            </div>
          </TabPane>
        </Tabs>
      </Surface>

      <SideSheet title={t("新增审批候选")} visible={candidateOpen} onCancel={() => setCandidateOpen(false)} width="min(520px, 100vw)">
        <Form onSubmit={saveCandidate} initValues={{ approval_mode: 'exact_request', data_classes: ['request_trace_events', 'call_logs', 'capture_gaps'] }}>
          <Form.Select field="approval_mode" label={t("审批模式")} optionList={[{ label: t("已有请求精确 ID"), value: 'exact_request' }, { label: t("新受控场景授权"), value: 'controlled_scenario' }]} />
          <Form.Input field="request_id" label="Request ID" placeholder={t("受控场景可暂不填写")} />
          <Form.Input field="scenario_label" label={t("场景标签")} required />
          <Form.Input field="source_instance" label={t("来源实例")} required />
          <Form.Select field="data_classes" label={t("批准数据类")} multiple optionList={[
            { label: 'request_trace_events', value: 'request_trace_events' },
            { label: 'call_logs', value: 'call_logs' },
            { label: 'capture_gaps', value: 'capture_gaps' },
          ]} />
          <Form.Input field="authorized_operator" label={t("授权操作人")} />
          <Form.Input field="retention_until" label={t("保留截止时间")} placeholder={t("ISO 8601 或本地日期时间")} />
          <Form.TextArea field="reason" label={t("审批上下文")} maxCount={500} />
          <div className="ur-form-actions"><Button htmlType="submit" type="primary">{t("创建")}</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={t("更新审批状态")} visible={Boolean(approvalItem)} onCancel={() => setApprovalItem(null)} width={480}>
        <Form onSubmit={saveApproval} initValues={{ approval_status: approvalItem?.approval_status || 'pending', reason: approvalItem?.reason || '', request_id: approvalItem?.request_id || '' }} key={approvalItem?.id || 0}>
          <Form.Select field="approval_status" label={t("状态")} optionList={[{ label: t("待审批"), value: 'pending' }, { label: t("已批准"), value: 'approved' }, { label: t("不可用"), value: 'unavailable' }]} />
          {approvalItem?.approval_mode === 'controlled_scenario' && <Form.Input field="request_id" label={t("受控运行返回的 Request ID")} placeholder={t("运行后封存；批准场景时可留空")} />}
          <Form.TextArea field="reason" label={t("原因")} maxCount={500} />
          <div className="ur-form-actions"><Button htmlType="submit" type="primary">{t("保存")}</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={t("登记交换制品")} visible={exchangeOpen} onCancel={() => setExchangeOpen(false)} width="min(560px, 100vw)">
        <Form onSubmit={saveExchange} initValues={{ item_ids: approvedItemIDs.join(','), schema_version: 'unirouter.prd5.xinsight.snapshot.v2', generated_at: new Date().toISOString() }} key={approvedItemIDs.join(',')}>
          <Form.Input field="exchange_id" label="Exchange ID" required />
          <Form.Input field="source_instance" label={t("来源实例")} required />
          <Form.Input field="schema_version" label={t("制品 Schema")} required />
          <Form.Input field="approval_manifest_sha256" label={t("审批清单 SHA-256")} required />
          <Form.Input field="artifact_sha256" label={t("制品 SHA-256")} required />
          <Form.Input field="artifact_ref" label={t("制品引用")} required />
          <Form.Input field="item_ids" label={t("已批准候选 ID")} required />
          <Form.InputNumber field="request_count" label={t("请求数")} min={0} required />
          <Form.InputNumber field="input_event_count" label={t("输入事件数")} min={0} required />
          <Form.Input field="generated_at" label={t("生成时间")} required />
          <Form.Input field="retention_until" label={t("保留截止时间")} />
          <div className="ur-form-actions"><Button htmlType="submit" type="primary">{t("登记")}</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={t("登记交付")} visible={Boolean(deliveryExchange)} onCancel={() => setDeliveryExchange(null)} width={500}>
        <Form onSubmit={saveDelivery} initValues={{ action: (deliveryExchange?.delivery_count || 0) > 0 ? 'redelivered' : 'handed_off', delivered_at: new Date().toISOString() }} key={deliveryExchange?.id || 0}>
          <Form.Select field="action" label={t("动作")} optionList={[{ label: t("首次交付"), value: 'handed_off' }, { label: t("重新投递"), value: 'redelivered' }]} />
          <Form.Input field="handoff_ref" label={t("交付引用")} required />
          <Form.Input field="delivered_at" label={t("交付时间")} required />
          <Form.TextArea field="note" label={t("备注")} maxCount={500} />
          <div className="ur-form-actions"><Button htmlType="submit" type="primary">{t("登记")}</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={t("登记 X-Insight 回执")} visible={Boolean(receiptExchange)} onCancel={() => setReceiptExchange(null)} width={620}>
        <Form onSubmit={saveReceipt} key={receiptExchange?.id || 0}>
          <Form.TextArea field="receipt_json" label="xinsight.import-receipt.v1 JSON" required autosize={{ minRows: 18, maxRows: 28 }} />
          <div className="ur-form-actions"><Button htmlType="submit" type="primary">{t("校验并登记")}</Button></div>
        </Form>
      </SideSheet>

      <SideSheet title={t("交换账本详情")} visible={Boolean(previewExchange)} onCancel={() => setPreviewExchange(null)} width={620}>
        {previewExchange && (
          <Descriptions row data={[
            { key: 'Exchange ID', value: previewExchange.exchange_id },
            { key: t("来源实例"), value: previewExchange.source_instance },
            { key: t("制品 Schema"), value: previewExchange.schema_version },
            { key: t("审批清单 SHA-256"), value: previewExchange.approval_manifest_sha256 },
            { key: t("制品 SHA-256"), value: previewExchange.artifact_sha256 },
            { key: t("制品引用"), value: previewExchange.artifact_ref },
            { key: t("候选 ID"), value: previewExchange.item_ids.join(', ') },
            { key: t("请求 / 事件"), value: `${previewExchange.request_count} / ${previewExchange.input_event_count}` },
            { key: t("源工作流"), value: previewExchange.workflow_status },
            { key: t("下游回执"), value: previewExchange.receipt_status },
            { key: t("警告代码"), value: previewExchange.warning_codes.join(', ') || '-' },
            { key: 'Import Job ID', value: previewExchange.import_job_id || t("X-Insight 尚未提供") },
            { key: 'Deep Link', value: previewExchange.deep_link ? <a href={previewExchange.deep_link} target="_blank" rel="noreferrer">{t("打开 X-Insight")}</a> : t("X-Insight 尚未提供") },
          ]} />
        )}
        <div style={{ display: 'grid', gap: 18, marginTop: 20 }}>
          <div>
            <Text strong>{t("纳入候选")}</Text>
            <Table
              size="small" pagination={false} loading={activityLoading} rowKey="id"
              dataSource={activity?.members || []}
              columns={[
                { title: 'ID', dataIndex: 'id', width: 64 },
                { title: 'Request ID', dataIndex: 'request_id', render: (value: string) => value ? <Text code>{value}</Text> : '-' },
                { title: t("场景"), dataIndex: 'scenario_label' },
                { title: t("审批"), dataIndex: 'approval_status', width: 100, render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
              ]}
            />
          </div>
          <div>
            <Text strong>{t("交付历史")}</Text>
            <Table
              size="small" pagination={false} loading={activityLoading} rowKey="id"
              dataSource={activity?.deliveries || []}
              columns={[
                { title: t("动作"), dataIndex: 'action', width: 120 },
                { title: t("引用"), dataIndex: 'handoff_ref' },
                { title: t("时间"), dataIndex: 'delivered_at', width: 180, render: (value: string) => formatDateTime(value) },
              ]}
            />
          </div>
          <div>
            <Text strong>{t("回执历史")}</Text>
            <Table
              size="small" pagination={false} loading={activityLoading} rowKey="id"
              dataSource={activity?.receipts || []}
              columns={[
                { title: t("状态"), dataIndex: 'receipt_status', width: 130, render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
                { title: t("输入 / 接受 / 重跑重复 / 拒绝"), render: (_: unknown, row: ExchangeActivity['receipts'][number]) => `${row.input_events} / ${row.accepted} / ${row.rerun_duplicate_noop} / ${row.rejected}` },
                { title: t("检查时间"), dataIndex: 'checked_at', width: 180, render: (value: string) => formatDateTime(value) },
              ]}
            />
          </div>
        </div>
      </SideSheet>
    </div>
  );
}
