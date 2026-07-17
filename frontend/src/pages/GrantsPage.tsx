import { useState, useEffect, useRef } from 'react';
import { Button, Form, Toast, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { formatDateTime } from '../utils/formatDate';
import { MoneyText, PagedTable, PageHeader, Surface } from '../ui';

interface GrantCreditsPanelProps {
  orgId?: number;
  canCreate?: boolean;
  onChanged?: () => void;
}

export function GrantCreditsPanel({ orgId, canCreate = true, onChanged }: GrantCreditsPanelProps) {
  const [grants, setGrants] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);
  const [clientRequestID, setClientRequestID] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const requestVersion = useRef(0);

  const newClientRequestID = () => {
    const randomPart = globalThis.crypto?.randomUUID?.()
      ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    return `grant-${randomPart}`;
  };

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setGrants([]);
    setTotal(0);
    api.getGrants(p, size, orgId)
      .then((d: any) => {
        if (version !== requestVersion.current) return;
        setGrants(d.data || []);
        setTotal(d.total || 0);
      })
      .catch((error: any) => {
        if (version !== requestVersion.current) return;
        Toast.error(`赠送额度加载失败${error?.message ? `：${error.message}` : ''}`);
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  useEffect(() => {
    setPage(1);
    load(1, pageSize);
  }, [orgId]);

  const handleCreate = async (v: any) => {
    if (submitting) return;
    try {
      const amount = String(v.amount || '').trim();
      const reason = String(v.reason || '').trim();
      const match = /^(\d+)(?:\.(\d{1,2}))?$/.exec(amount);
      if (!match) throw new Error('invalid amount');
      const amountCents = BigInt(match[1]) * 100n + BigInt((match[2] || '').padEnd(2, '0') || '0');
      if (amountCents <= 0n || amountCents > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('amount out of range');
      if (!reason) throw new Error('reason is required');
      const operationID = clientRequestID || newClientRequestID();
      setClientRequestID(operationID);
      setSubmitting(true);
      await api.createGrant({
        org_id: orgId,
        amount_cents: Number(amountCents),
        reason,
        client_request_id: operationID,
      });
      Toast.success('赠送额度已发放');
      setShowDrawer(false);
      setClientRequestID('');
      formApi?.reset();
      load(1);
      onChanged?.();
    } catch (error: any) {
      const message = error?.message === 'reason is required'
        ? '请填写发放原因或工单号'
        : error?.message?.startsWith('invalid amount') || error?.message?.startsWith('amount out of range')
          ? '请输入最多两位小数的正金额'
          : (error?.message || '请稍后重试');
      Toast.error(`发放失败：${message}`);
    } finally {
      setSubmitting(false);
    }
  };

  const openCreate = () => {
    formApi?.reset();
    setClientRequestID(newClientRequestID());
    setShowDrawer(true);
  };

  const closeCreate = () => {
    if (submitting) return;
    setShowDrawer(false);
    setClientRequestID('');
    formApi?.reset();
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '组织', dataIndex: 'org_name', render: (v: string) => v || '-' },
    { title: '金额（元）', dataIndex: 'amount_micros', render: (v: string) => <MoneyText micros={v || '0'} tone="income" /> },
    { title: '原因', dataIndex: 'reason' },
    { title: '请求ID', dataIndex: 'client_request_id', render: (v: string) => <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{v}</span> },
    { title: '时间', dataIndex: 'created_at', render: (v: string) => formatDateTime(v) },
  ];

  return (
    <>
      <Surface
        title="赠送额度记录"
        description="赠送额度是平台发放的正向余额，不等同于现金调整；如需纠错，请通过资金流水和审计记录追踪。"
        actions={canCreate ? <Button icon={<IconPlus />} type="primary" onClick={openCreate}>发放赠送额度</Button> : undefined}
      >
        <PagedTable
          columns={columns}
          dataSource={grants}
          loading={loading}
          rowKey="id"
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={(p: number) => { setPage(p); load(p, pageSize); }}
          onPageSizeChange={(s: number) => { setPageSize(s); setPage(1); load(1, s); }}
        />
      </Surface>

      <SideSheet
        title="发放赠送额度"
        visible={showDrawer}
        onCancel={closeCreate}
        closable={!submitting}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleCreate}>
          <Form.Input field="amount" label="金额（元）" rules={[{ required: true, message: '必填' }]} placeholder="100.00" />
          <Form.Input field="reason" label="原因 / 工单号" placeholder="授信调整 / 商务补偿 / TICKET-001" rules={[{ required: true, message: '必填' }]} />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeCreate} disabled={submitting}>取消</Button>
            <Button type="primary" loading={submitting} disabled={submitting} onClick={() => formApi?.submitForm()}>确认发放</Button>
          </div>
        </Form>
      </SideSheet>
    </>
  );
}

export default function GrantsPage() {
  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="计费与资金"
        title="赠送额度"
        description="查看并发放组织赠送额度。正式入口已合并到资金账户，本路径仅保留兼容。"
      />
      <GrantCreditsPanel />
    </div>
  );
}
