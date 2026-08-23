import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Input, Modal, Table, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { ApiError } from '../../api/client';
import {
  providerCostApi,
  type PendingProviderCost,
  type ProviderCostReconciliationResult,
} from '../../api/providerCosts';
import { EmptyState, StatusTag, Surface } from '../../ui';
import { formatInteger } from '../../utils/formatNumber';

function exactAmount(value: string | undefined, scale: number, currency: string) {
  if (value === undefined) return '未写入';
  return `${value} / ${formatInteger(scale)} ${currency}`;
}

function reconciliationError(error: unknown) {
  if (error instanceof ApiError) return `协调失败（${error.status}）：${error.message}`;
  return '协调失败：未决供应商成本状态不可用。';
}

export function ProviderCostResolutionWorkspace({ orgId }: { orgId: number }) {
  const [items, setItems] = useState<PendingProviderCost[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [selected, setSelected] = useState<PendingProviderCost | null>(null);
  const [reason, setReason] = useState('');
  const [evidenceRef, setEvidenceRef] = useState('');
  const [mutating, setMutating] = useState(false);
  const [lastResult, setLastResult] = useState<ProviderCostReconciliationResult | null>(null);

  const load = useCallback(() => {
    if (!orgId) return;
    setLoading(true);
    setLoadError('');
    providerCostApi.listPending(orgId)
      .then(response => setItems(response.data || []))
      .catch((error: unknown) => {
        setItems([]);
        setLoadError(error instanceof ApiError
          ? `未决成本加载失败（${error.status}）：${error.message}`
          : '未决成本加载失败。');
      })
      .finally(() => setLoading(false));
  }, [orgId]);

  useEffect(() => { load(); }, [load]);

  const openRefund = (item: PendingProviderCost) => {
    setSelected(item);
    setReason('上游响应未交付；在 cost_observation 模式下解除未决冻结并生成零收费协调收据。');
    setEvidenceRef(`provider-attempt:${item.attempt_id}`);
  };

  const close = () => {
    if (mutating) return;
    setSelected(null);
    setReason('');
    setEvidenceRef('');
  };

  const reconcile = async () => {
    if (!selected || mutating) return;
    if (!reason.trim() || !evidenceRef.trim()) {
      Toast.warning({ content: '原因和证据引用均为必填项。' });
      return;
    }
    setMutating(true);
    try {
      const result = await providerCostApi.reconcile({
        org_id: selected.org_id,
        request_id: selected.request_id,
        attempt_id: selected.attempt_id,
        action: 'refund',
        reason: reason.trim(),
        evidence_ref: evidenceRef.trim(),
      });
      setLastResult(result);
      setSelected(null);
      setReason('');
      setEvidenceRef('');
      Toast.success({ content: '未决冻结已解除，零收费观察收据已生成。' });
      load();
    } catch (error) {
      Toast.error({ content: reconciliationError(error) });
    } finally {
      setMutating(false);
    }
  };

  const columns = [
    {
      title: '请求 / 尝试',
      key: 'identity',
      width: 260,
      render: (_: unknown, item: PendingProviderCost) => (
        <div>
          <Typography.Text code>{item.request_id}</Typography.Text>
          <br />
          <Typography.Text type="tertiary">{item.attempt_id}</Typography.Text>
        </div>
      ),
    },
    {
      title: '供应商 / 通道',
      key: 'provider',
      width: 180,
      render: (_: unknown, item: PendingProviderCost) => (
        <span>{item.provider}<br /><Typography.Text type="tertiary">{item.channel}</Typography.Text></span>
      ),
    },
    {
      title: '状态',
      key: 'state',
      width: 180,
      render: (_: unknown, item: PendingProviderCost) => (
        <span><StatusTag status="warning">{item.state}</StatusTag><br /><Typography.Text type="tertiary">{item.outcome_certainty}</Typography.Text></span>
      ),
    },
    {
      title: '冻结时计费模式',
      dataIndex: 'billing_mode',
      width: 170,
      render: (value: string) => <Tag color={value === 'cost_observation' ? 'blue' : 'orange'}>{value}</Tag>,
    },
    {
      title: '供应商成本 / 客户收费',
      key: 'amounts',
      width: 230,
      render: (_: unknown, item: PendingProviderCost) => (
        <span>
          成本：{exactAmount(item.calculated_cost_micros, item.amount_scale, item.currency)}
          <br />
          收费：{exactAmount(item.charged_cost_micros, item.amount_scale, item.currency)}
        </span>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      fixed: 'right' as const,
      render: (_: unknown, item: PendingProviderCost) => item.billing_mode === 'cost_observation'
        ? <Button type="primary" theme="light" onClick={() => openRefund(item)}>零收费协调</Button>
        : <Typography.Text type="tertiary">转账务审批</Typography.Text>,
    },
  ];

  return (
    <Surface title="未决供应商成本">
      <Banner
        type="warning"
        closeIcon={null}
        description="这里只允许平台管理员处理正式后端报告的未决项。cost_observation 的 refund 不接收金额、不产生客户扣费或钱包交易，只解除冻结并生成不可变观察收据；其他计费模式不会在这里提供快捷操作。"
        style={{ marginBottom: 12 }}
      />
      {lastResult && (
        <Banner
          type="success"
          closeIcon={null}
          description={`最近完成：${lastResult.request_id} / ${lastResult.attempt_id}；观察收据 #${lastResult.terminal_observation_receipt_id || '—'}；客户收费 ${lastResult.customer_charged_micros}，退款 ${lastResult.customer_refund_micros}（${lastResult.currency}/${formatInteger(lastResult.amount_scale)}）。`}
          style={{ marginBottom: 12 }}
        />
      )}
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
        <Button icon={<IconRefresh />} onClick={load} loading={loading}>刷新未决项</Button>
      </div>
      {loadError && <Banner type="danger" description={loadError} closeIcon={null} style={{ marginBottom: 12 }} />}
      {!loading && !loadError && items.length === 0 ? (
        <EmptyState title="没有未决供应商成本" description="当前客户组织不存在需要人工协调的供应商成本冻结。" minHeight={180} />
      ) : (
        <div className="ur-table-shell">
          <Table
            columns={columns}
            dataSource={items}
            loading={loading}
            rowKey={(item?: PendingProviderCost) => item ? `${item.request_id}:${item.attempt_id}` : 'missing'}
            pagination={false}
            scroll={{ x: 1180 }}
          />
        </div>
      )}
      <Modal
        title="确认零收费供应商成本协调"
        visible={Boolean(selected)}
        onCancel={close}
        onOk={reconcile}
        okText="解除冻结并生成收据"
        cancelText="取消"
        okButtonProps={{ 'aria-label': '解除冻结并生成收据' }}
        cancelButtonProps={{ 'aria-label': '取消零收费协调' }}
        confirmLoading={mutating}
        closeOnEsc={!mutating}
        maskClosable={!mutating}
      >
        {selected && (
          <>
            <Banner
              type="info"
              closeIcon={null}
              description={`请求 ${selected.request_id}；尝试 ${selected.attempt_id}。该操作固定为 refund，服务端不接受金额。`}
              style={{ marginBottom: 12 }}
            />
            <Typography.Text strong>协调原因</Typography.Text>
            <TextArea
              aria-label="协调原因"
              value={reason}
              onChange={setReason}
              maxCount={500}
              autosize={{ minRows: 3, maxRows: 5 }}
              style={{ marginTop: 6, marginBottom: 12 }}
            />
            <Typography.Text strong>证据引用</Typography.Text>
            <Input
              aria-label="证据引用"
              value={evidenceRef}
              onChange={setEvidenceRef}
              maxLength={255}
              style={{ marginTop: 6 }}
            />
          </>
        )}
      </Modal>
    </Surface>
  );
}
