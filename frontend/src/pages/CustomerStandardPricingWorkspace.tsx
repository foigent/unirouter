import { useState } from 'react';
import { Banner, Button, Input, Select, Space, Typography } from '@douyinfe/semi-ui';
import {
  enterpriseFinanceApi,
  type CustomerPriceBinding,
  type CustomerPriceEntry,
  type CustomerPriceTable,
} from '../api/enterpriseFinance';
import { ApiError } from '../api/transport';
import type { MessageKey } from '../i18n/catalog';
import { useI18n } from '../i18n';
import { useAuthStore } from '../store/auth';
import { Surface, formatMoney } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { buildRequestRateComponents, requestPriceMeters } from '../utils/rateCardPriceBands';

const operations = [
  { value: 'openai.chat_completions.v1', label: 'Chat Completions' },
  { value: 'openai.embeddings.v1', label: 'Embeddings' },
  { value: 'openai.responses.v1', label: 'Responses' },
  { value: 'anthropic.messages.v1', label: 'Messages' },
];

const blankEntry = () => ({ operation: operations[0].value, public_model: '' } as Record<string, string>);

function parseInteger(value: string, min: number): number | null {
  if (!/^\d+$/.test(value.trim())) return null;
  const number = Number(value);
  return Number.isSafeInteger(number) && number >= min ? number : null;
}

export default function CustomerStandardPricingWorkspace({ orgId, canManage }: { orgId: number; canManage: boolean }) {
  const { t } = useI18n();
  const userId = useAuthStore(state => state.user?.id);
  const [tableRef, setTableRef] = useState('');
  const [tableVersion, setTableVersion] = useState('1');
  const [table, setTable] = useState<CustomerPriceTable | null>(null);
  const [draftOpen, setDraftOpen] = useState(false);
  const [effectiveFrom, setEffectiveFrom] = useState('');
  const [effectiveTo, setEffectiveTo] = useState('');
  const [entries, setEntries] = useState([blankEntry()]);
  const [scope, setScope] = useState('0');
  const [binding, setBinding] = useState<{ projectId: number; value?: CustomerPriceBinding } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const run = async (action: () => Promise<void>) => {
    setError('');
    setBusy(true);
    try {
      await action();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('操作失败'));
    } finally {
      setBusy(false);
    }
  };
  const ref = tableRef.trim();
  const version = parseInteger(tableVersion, 1);
  const projectId = parseInteger(scope, 0);
  const selected = table?.table_ref === ref && table.version === version ? table : null;
  const scopedBinding = binding && binding.projectId === projectId ? binding : null;

  const openTable = () => run(async () => {
    if (!ref || version === null) return;
    setTable(null);
    setTable(await enterpriseFinanceApi.getCustomerPriceTable(orgId, ref, version));
  });

  const save = () => run(async () => {
    if (!ref || version === null) return;
    const from = new Date(effectiveFrom);
    const to = effectiveTo ? new Date(effectiveTo) : undefined;
    if (!Number.isFinite(from.getTime()) || (to && (!Number.isFinite(to.getTime()) || to <= from))) {
      throw new Error(t('请核对价表生效时间和结束时间'));
    }
    const rates: CustomerPriceEntry[] = entries.map(entry => {
      const components = buildRequestRateComponents(entry);
      const model = entry.public_model.trim();
      if (!model || !components) throw new Error(t('每条价目需要模型、操作和四项 CNY 费率'));
      return { public_model: model, operation: entry.operation, components };
    });
    setTable(await enterpriseFinanceApi.createCustomerPriceTable(orgId, {
      table_ref: ref, version, effective_from: from.toISOString(), effective_to: to?.toISOString(), entries: rates,
    }));
    setDraftOpen(false);
  });

  const readBinding = () => run(async () => {
    if (projectId === null) return;
    setBinding(null);
    try {
      const value = await enterpriseFinanceApi.getCustomerPriceBinding(orgId, projectId);
      if (value.org_id !== orgId || value.project_id !== projectId) throw new Error(t('当前绑定不属于所选范围'));
      setBinding({ projectId, value });
    } catch (failure) {
      if (!(failure instanceof ApiError) || failure.status !== 404) throw failure;
      setBinding({ projectId });
    }
  });

  const changeEntry = (index: number, key: string, value: string) => {
    setEntries(current => current.map((entry, n) => n === index ? { ...entry, [key]: value } : entry));
  };

  return <Surface title={t('Standard 客户价表')}>
    <fieldset disabled={busy} style={{ border: 0, padding: 0, minWidth: 0 }}>
      <Space wrap>
        <Input aria-label={t('客户价表标识')} placeholder={t('客户价表标识')} value={tableRef} onChange={value => { setTableRef(value); setBinding(null); }} maxLength={160} />
        <Input aria-label={t('客户价表版本')} value={tableVersion} onChange={value => { setTableVersion(value); setBinding(null); }} inputMode="numeric" style={{ width: 95 }} />
        <Button onClick={openTable} disabled={!ref || version === null}>{t('查阅价表')}</Button>
        {canManage && <Button onClick={() => setDraftOpen(value => !value)}>{t('新建客户价表草稿')}</Button>}
      </Space>
      {canManage && draftOpen && (
        <div style={{ marginTop: 16 }}>
          <Space wrap>
            <label>{t('生效时间（本地时区）')} <input type="datetime-local" value={effectiveFrom} onChange={event => setEffectiveFrom(event.target.value)} /></label>
            <label>{t('结束时间（可选）')} <input type="datetime-local" value={effectiveTo} onChange={event => setEffectiveTo(event.target.value)} /></label>
          </Space>
          {entries.map((entry, index) => (
            <div key={index} style={{ marginBlock: 12 }}>
              <Space wrap>
                <Input aria-label={t('模型标识')} placeholder={t('模型标识')} value={entry.public_model} onChange={value => changeEntry(index, 'public_model', value)} />
                <Select aria-label={t('操作')} value={entry.operation} optionList={operations} onChange={value => changeEntry(index, 'operation', String(value))} />
                {requestPriceMeters.map(meter => (
                  <label key={meter.key}>
                    {t(meter.label as MessageKey)}
                    <Input aria-label={t(meter.label as MessageKey)} value={entry[meter.key] ?? ''} onChange={value => changeEntry(index, meter.key, value)} placeholder="CNY / 1,000,000" />
                  </label>
                ))}
                {entries.length > 1 && <Button onClick={() => setEntries(rows => rows.filter((_, n) => n !== index))}>{t('删除')}</Button>}
              </Space>
            </div>
          ))}
          <Space>
            <Button onClick={() => setEntries(rows => [...rows, blankEntry()])}>{t('添加价目')}</Button>
            <Button type="primary" onClick={save} disabled={!ref || version === null || !effectiveFrom}>{t('保存草稿')}</Button>
          </Space>
        </div>
      )}
      {selected && (
        <div style={{ marginBlock: 16 }}>
          <Typography.Paragraph>
            {selected.table_ref} v{selected.version} · {selected.approved_at ? t('已批准') : t('待复核')} · {formatDateTime(selected.effective_from)} → {selected.effective_to ? formatDateTime(selected.effective_to) : t('长期有效')}
          </Typography.Paragraph>
          <ul>
            {selected.entries.map(entry => (
              <li key={entry.public_model + ':' + entry.operation}>
                {entry.public_model} / {entry.operation}
                {' · '}{entry.components.map(component => `${component.meter} ${formatMoney(component.amount.value_minor)}`).join(' · ')}
              </li>
            ))}
          </ul>
          {canManage && !selected.approved_at && (
            <Button disabled={selected.created_by === 'user:' + userId} onClick={() => run(async () => {
              await enterpriseFinanceApi.approveCustomerPriceTable(orgId, selected.table_ref, selected.version);
              setTable(await enterpriseFinanceApi.getCustomerPriceTable(orgId, selected.table_ref, selected.version));
            })}>{t('批准客户价表')}</Button>
          )}
        </div>
      )}
      {selected?.approved_at && (
        <div style={{ marginTop: 12 }}>
          <Space wrap>
            <Input aria-label={t('绑定项目 ID，0 为组织')} value={scope} onChange={value => { setScope(value); setBinding(null); }} inputMode="numeric" />
            <Button onClick={readBinding} disabled={projectId === null}>{t('查阅当前绑定')}</Button>
          </Space>
          {scopedBinding && (
            <div style={{ marginTop: 8 }}>
              <Typography.Paragraph>
                {scopedBinding.projectId === 0 ? t('组织范围') : t('项目 ID') + ' ' + scopedBinding.projectId}
                {' · '}{scopedBinding.value ? scopedBinding.value.table_ref + ' v' + scopedBinding.value.table_version : t('尚无绑定')}
              </Typography.Paragraph>
              {canManage && (
                <>
                  <Banner type="info" closeIcon={null} description={t('绑定后新请求按该版本的模型、操作和有效期核验报价。')} />
                  <Button
                    type="primary"
                    style={{ marginTop: 8 }}
                    disabled={scopedBinding.value?.table_ref === selected.table_ref && scopedBinding.value.table_version === selected.version}
                    onClick={() => run(async () => {
                      setBinding(null);
                      const value = await enterpriseFinanceApi.bindCustomerPriceTable(
                        orgId, scopedBinding.projectId, selected, scopedBinding.value?.revision ?? 0,
                      );
                      setBinding({ projectId: scopedBinding.projectId, value });
                    })}
                  >{t('确认绑定已批准版本')}</Button>
                </>
              )}
            </div>
          )}
        </div>
      )}
      {error && <Banner type="danger" closeIcon={null} description={error} style={{ marginTop: 12 }} />}
    </fieldset>
  </Surface>;
}
