import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Input, Popconfirm, Typography } from '@douyinfe/semi-ui';
import { sourceBudgetBindingsApi, type SourceBudgetBinding } from '../api/sourceConnections';

export default function SourceBudgetScope({ orgId, projectId, canWrite }: { orgId: number; projectId: number; canWrite: boolean }) {
  const [bindings, setBindings] = useState<SourceBudgetBinding[]>([]);
  const [scopeRef, setScopeRef] = useState('');
  const [next, setNext] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const refresh = useCallback(async (after = '') => {
    setBusy(true);
    try {
      const result = await sourceBudgetBindingsApi.list(orgId, projectId, after);
      setBindings(previous => after ? [...previous, ...result.data] : result.data);
      setNext(result.has_more ? result.next_after : ''); setError('');
    } catch (e) { setError(e instanceof Error ? e.message : '无法读取预算来源范围'); }
    finally { setBusy(false); }
  }, [orgId, projectId]);
  useEffect(() => { void refresh(); }, [refresh]);
  const update = async (ref: string, revision: string, status: 'active' | 'revoked') => {
    setBusy(true); setError('');
    try { await sourceBudgetBindingsApi.set(orgId, projectId, ref, revision, status); setScopeRef(''); await refresh(); }
    catch (e) { setError(e instanceof Error ? e.message : '预算来源范围更新失败'); }
    finally { setBusy(false); }
  };
  return <section aria-label="预算来源范围" style={{ display: 'grid', gap: 8 }}>
    <Typography.Title heading={6}>预算来源范围</Typography.Title>
    <Typography.Text>项目原有网关预算属于本项目。企业预算需明确绑定其项目范围引用；绑定只开放该项目的预算与分摊切片。预算编辑、审批和分摊仍在原企业账务页面完成。</Typography.Text>
    {error && <Banner type="warning" description={error} closeIcon={null} />}
    {!busy && !bindings.some(binding => binding.status === 'active') && <Typography.Text>企业预算来源范围未配置。</Typography.Text>}
    {bindings.map(binding => <div className="ur-toolbar" key={binding.scope_ref}>
      <Typography.Text>{binding.scope_ref} · 版本 {binding.revision} · {binding.status === 'active' ? '已绑定本项目' : '已撤销'}</Typography.Text>
      {canWrite && (binding.status === 'active'
        ? <Popconfirm title="撤销后，源接口不再向本项目提供该范围的企业预算。执行事件的读取进度保持有效。" onConfirm={() => update(binding.scope_ref, binding.revision, 'revoked')}><span><Button disabled={busy}>撤销预算绑定</Button></span></Popconfirm>
        : <Button disabled={busy} onClick={() => void update(binding.scope_ref, binding.revision, 'active')}>重新绑定本项目</Button>)}
    </div>)}
    {next && <Button disabled={busy} onClick={() => void refresh(next)}>加载更多预算绑定</Button>}
    {canWrite && <div className="ur-toolbar">
      <Input aria-label="企业项目范围引用" value={scopeRef} onChange={setScopeRef} maxLength={160} disabled={busy} placeholder="原企业预算的项目范围引用" />
      <Button disabled={busy || !scopeRef.trim() || scopeRef !== scopeRef.trim()} onClick={() => void update(scopeRef, '0', 'active')}>绑定本项目预算来源</Button>
    </div>}
  </section>;
}
