import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ModelBillingPage from '../ModelBillingPage';

const auth = vi.hoisted(() => ({ user: null as any }));
const mocks = vi.hoisted(() => ({
  organizationPrices: vi.fn(),
  enterpriseRateCard: vi.fn(),
}));

vi.mock('../../store/auth', () => ({
  useAuthStore: (selector: any) => selector({ user: auth.user }),
}));

vi.mock('../ModelPricesPage', () => ({
  default: () => <div>GLOBAL-BASE-PRICES</div>,
}));

vi.mock('../OrgModelPricesPage', () => ({
  default: (props: unknown) => {
    mocks.organizationPrices(props);
    return <div>ORGANIZATION-MULTIPLIER</div>;
  },
}));

vi.mock('../EnterpriseFinanceWorkspaces', () => ({
  EnterpriseRateCardWorkspace: (props: { orgId?: number }) => {
    mocks.enterpriseRateCard(props);
    return <div>RATE-CARD-{props.orgId}</div>;
  },
}));

describe('ModelBillingPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.replaceState(null, '', '/model-prices');
  });

  it('keeps global base prices visible but fails closed for customer pricing when context is ambiguous', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ambiguous' },
    };

    render(<ModelBillingPage />);

    expect(screen.getByText('GLOBAL-BASE-PRICES')).toBeTruthy();
    expect(screen.getByText(/全局基础费率兼容视图/)).toBeTruthy();
    fireEvent.click(screen.getByRole('tab', { name: '组织倍率（兼容视图）' }));
    expect(await screen.findByRole('heading', { name: '客户组织上下文不唯一' })).toBeTruthy();
    expect(mocks.organizationPrices).not.toHaveBeenCalled();
    expect(mocks.enterpriseRateCard).not.toHaveBeenCalled();
  });

  it('binds organization pricing and the enterprise Rate Card to the canonical customer', async () => {
    auth.user = {
      role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
    };

    render(<ModelBillingPage />);

    expect(screen.getByText('客户范围：Acme')).toBeTruthy();
    expect(screen.getByText(/Rate Card 严格模式启用后.*才是请求计价的唯一权威/)).toBeTruthy();

    fireEvent.click(screen.getByRole('tab', { name: '组织倍率（兼容视图）' }));
    expect(await screen.findByText('ORGANIZATION-MULTIPLIER')).toBeTruthy();

    fireEvent.click(screen.getByRole('tab', { name: '企业 Rate Card' }));
    expect(await screen.findByText('RATE-CARD-22')).toBeTruthy();
    expect(mocks.enterpriseRateCard).toHaveBeenCalledWith(expect.objectContaining({ orgId: 22 }));
  });
});
