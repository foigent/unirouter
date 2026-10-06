import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import XInsightCredentialRecoveryPage from '../XInsightCredentialRecoveryPage';
import { xInsightSetupApi } from '../../api/xInsightSetup';
import { useAuthStore } from '../../store/auth';

vi.mock('../../api/xInsightSetup', () => ({
  xInsightSetupApi: {
    recoveryScope: vi.fn(), recoveryPreview: vi.fn(), recoveryConfirm: vi.fn(), status: vi.fn(),
  },
}));

const installation = `xci_${'A'.repeat(22)}`;
const recovery = `xrr_${'B'.repeat(22)}`;
const ticket = `xirt_${'C'.repeat(43)}`;
const fragment = '#' + new URLSearchParams({
  installation_id: installation, recovery_id: recovery,
  xinsight_origin: 'https://xi.example', recovery_ticket: ticket,
});

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ user: { id: 19, username: 'technical-admin', role: 'platform_admin', org_id: 1 },
    token: 'synthetic', isAuthenticated: true });
  window.history.replaceState(null, '', `/connectors/x-insight/recover${fragment}`);
});
afterEach(() => { window.history.replaceState(null, '', '/'); });

describe('original organization recovery handoff', () => {
  it('uses the server-authorized business organization instead of the platform actor technical org', async () => {
    vi.mocked(xInsightSetupApi.recoveryScope).mockResolvedValue({
      schema_version: 'unirouter.xinsight.credential-recovery-scope.v1',
      installation_id: installation, org_id: 42,
    });
    vi.mocked(xInsightSetupApi.recoveryPreview).mockResolvedValue({
      schema_version: 'unirouter.xinsight.credential-recovery-preview.v1',
      installation_id: installation, deployment_id: `xid_${'D'.repeat(22)}`,
      project_id: 'project.original', final_event_scope: ['model.request.completed'],
      source_binding_sha256: `sha256:${'a'.repeat(64)}`,
      xinsight_origin: 'https://xi.example', unirouter_origin: 'https://ur.example',
      recovery_id: recovery, expires_at: '2030-01-01T00:00:00Z',
      credential: { state: 'expired_recovery_required', current_token_sha256: '', current_expires_at: '' },
    });
    render(<XInsightCredentialRecoveryPage />);
    await screen.findByText('原 X-Insight 连接（沿用原授权范围）');
    expect(xInsightSetupApi.recoveryScope).toHaveBeenCalledWith(installation);
    expect(xInsightSetupApi.recoveryPreview).toHaveBeenCalledWith(42, installation, ticket);
    expect(xInsightSetupApi.recoveryPreview).not.toHaveBeenCalledWith(1, installation, ticket);
    expect(window.location.hash).toBe('');
  });

  it('does not send a recovery ticket when local installation scope is denied', async () => {
    vi.mocked(xInsightSetupApi.recoveryScope).mockRejectedValue(new Error('scope denied'));
    render(<XInsightCredentialRecoveryPage />);
    await waitFor(() => expect(xInsightSetupApi.recoveryScope).toHaveBeenCalledWith(installation));
    await screen.findByText('无法核对原连接、管理员权限或恢复票据。已撤销的连接不能恢复；请从 X-Insight 查看原因并重新发起。');
    expect(xInsightSetupApi.recoveryPreview).not.toHaveBeenCalled();
    expect(xInsightSetupApi.recoveryConfirm).not.toHaveBeenCalled();
  });
});
