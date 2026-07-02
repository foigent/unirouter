import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import LicensePage from '../LicensePage';

const mockGetLicense = vi.fn();
const mockUpdateLicense = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getLicense: (...args: any[]) => mockGetLicense(...args),
    updateLicense: (...args: any[]) => mockUpdateLicense(...args),
  },
}));

const ACTIVE_LICENSE = {
  status: 'active',
  valid: true,
  signature_valid: true,
  warning: false,
  days_until_expiry: 180,
  checked_at: '2026-07-02T00:00:00Z',
  verifier_configured: true,
  license: {
    customer: 'Acme Corp',
    deployment_id: 'prod-cn-1',
    tier: 'enterprise',
    issued_at: '2026-07-01T00:00:00Z',
    expires_at: '2026-12-31T00:00:00Z',
    features: ['projects', 'native_adapters'],
    limits: { projects: 20 },
  },
};

describe('LicensePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetLicense.mockResolvedValue(ACTIVE_LICENSE);
    mockUpdateLicense.mockResolvedValue(ACTIVE_LICENSE);
  });

  it('renders license status and metadata', async () => {
    render(<LicensePage />);

    expect(mockGetLicense).toHaveBeenCalled();
    expect(await screen.findByText('Acme Corp')).toBeTruthy();
    expect(screen.getByText('Active')).toBeTruthy();
    expect(screen.getByText('enterprise')).toBeTruthy();
    expect(screen.getByText('projects, native_adapters')).toBeTruthy();
  });

  it('submits pasted offline license JSON', async () => {
    render(<LicensePage />);

    await screen.findByText('Acme Corp');
    const textarea = screen.getByPlaceholderText('{"payload": {...}, "signature": "..."}');
    fireEvent.change(textarea, {
      target: { value: '{"payload":{"customer":"Acme Corp"},"signature":"sig"}' },
    });
    fireEvent.click(screen.getByText('导入'));

    await waitFor(() => {
      expect(mockUpdateLicense).toHaveBeenCalledWith({
        payload: { customer: 'Acme Corp' },
        signature: 'sig',
      });
    });
  });
});
