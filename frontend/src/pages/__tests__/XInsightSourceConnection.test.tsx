import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import XInsightSourceConnection from '../XInsightSourceConnection';
import { parseSourceRegistration, sourceConnectionsApi, sourceProfiles, type SourceConnection } from '../../api/sourceConnections';

vi.mock('../../api/sourceConnections', async original => ({ ...await original<typeof import('../../api/sourceConnections')>(), sourceConnectionsApi: { get: vi.fn(), control: vi.fn() } }));
const registration = { schema_version: 'unirouter.xinsight.source-credential-registration.v1', token_format:'ur-xf1-hex256.v1', verifier_scheme:'receiver_sha256_v1', key_prefix:'ur-xf1-aaaaaaaaaaaaa', verifier_sha256:'a'.repeat(64), operation_id:'b'.repeat(32), source_instance:'synthetic-ui', org_id:'1', project_id:'2', profiles:['unirouter.execution-context.v1'] };
const initial: SourceConnection = { status:'not_configured', source_instance:'synthetic-ui' };

describe('source connection control', () => {
  beforeEach(() => { vi.clearAllMocks(); vi.mocked(sourceConnectionsApi.get).mockResolvedValue(initial); vi.mocked(sourceConnectionsApi.control).mockResolvedValue({status:'paused'}); });
  it('requires a scope-bound source registration and rejects bearer or old feed bundles', () => {
    expect(parseSourceRegistration(JSON.stringify(registration),'synthetic-ui',2)).toEqual(registration);
    for (const extra of [{bearer:'synthetic-token'},{project_id:'3'},{schema_version:'unirouter.xinsight.feed-credential-registration.v1'},{profiles:['unirouter.execution-identity.v1','unirouter.execution-context.v1']}]) {
      expect(() => parseSourceRegistration(JSON.stringify({...registration,...extra}),'synthetic-ui',2)).toThrow();
    }
  });
  it('prepares only the selected project and clears registration after real API success', async () => {
    render(<XInsightSourceConnection projectId={2} expansionAllowed />);
    await screen.findByText(/未配置 · 来源 synthetic-ui/);
    fireEvent.change(screen.getByLabelText('来源凭据注册信息'),{target:{value:JSON.stringify(registration)}});
    fireEvent.change(screen.getByLabelText('来源接收端 IP'),{target:{value:'127.0.0.1/32'}});
    fireEvent.click(screen.getByRole('button',{name:'准备来源凭据'}));
    await waitFor(() => expect(sourceConnectionsApi.control).toHaveBeenCalledWith(2,'prepare',{allowed_ip:'127.0.0.1/32',registration}));
    await waitFor(() => expect(screen.getByLabelText('来源凭据注册信息')).toHaveValue(''));
  });
  it('accepts all eleven explicit profiles only when the current server enables them', () => {
    const expanded = { ...registration, profiles: [...sourceProfiles] };
    expect(parseSourceRegistration(JSON.stringify(expanded), 'synthetic-ui', 2, sourceProfiles)).toEqual(expanded);
    expect(() => parseSourceRegistration(JSON.stringify(expanded), 'synthetic-ui', 2, ['unirouter.execution-context.v1'])).toThrow();
    expect(() => parseSourceRegistration(JSON.stringify({ ...expanded, profiles: [...sourceProfiles, sourceProfiles[0]] }), 'synthetic-ui', 2, sourceProfiles)).toThrow();
  });
  it.each([false, true])('does not infer B grants from UI upgrade (server enabled: %s)', async enabled => {
    vi.mocked(sourceConnectionsApi.get).mockResolvedValue({ ...initial, ...(enabled ? { available_profiles: [...sourceProfiles] } : {}) });
    render(<XInsightSourceConnection projectId={2} expansionAllowed />);
    await screen.findByText(/未配置 · 来源 synthetic-ui/);
    const expanded = { ...registration, profiles: ['unirouter.runtime-observation.v1'] };
    fireEvent.change(screen.getByLabelText('来源凭据注册信息'), { target: { value: JSON.stringify(expanded) } });
    fireEvent.change(screen.getByLabelText('来源接收端 IP'), { target: { value: '127.0.0.1/32' } });
    fireEvent.click(screen.getByRole('button', { name: '准备来源凭据' }));
    if (enabled) {
      await waitFor(() => expect(sourceConnectionsApi.control).toHaveBeenCalledWith(2, 'prepare', { allowed_ip: '127.0.0.1/32', registration: expanded }));
    } else {
      await screen.findByText(/注册信息与当前来源、项目或权限不匹配/);
      expect(sourceConnectionsApi.control).not.toHaveBeenCalled();
    }
  });
  it('keeps pause available when expansion entitlement is absent', async () => {
    vi.mocked(sourceConnectionsApi.get).mockResolvedValue({...initial,status:'active',credentials:[{id:'9007199254740993',key_prefix:'ur-xf1-aaaaaaaaaaaaa',status:'feed_active',operation_id:'b'.repeat(32),profiles:['unirouter.execution-context.v1']}]});
    render(<XInsightSourceConnection projectId={2} expansionAllowed={false} />);
    fireEvent.click(await screen.findByRole('button',{name:'暂停来源读取'}));
    await waitFor(() => expect(sourceConnectionsApi.control).toHaveBeenCalledWith(2,'pause',{}));
  });
  it('does not retain a success-looking state after status fails', async () => {
    vi.mocked(sourceConnectionsApi.get).mockRejectedValue(new Error('source capture disabled'));
    render(<XInsightSourceConnection projectId={2} expansionAllowed />);
    expect(await screen.findByText('source capture disabled')).toBeTruthy();
    expect(screen.queryByRole('button',{name:'准备来源凭据'})).toBeNull();
  });
});
