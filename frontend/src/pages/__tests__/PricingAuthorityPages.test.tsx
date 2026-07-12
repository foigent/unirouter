import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ModelPricesPage from '../ModelPricesPage';
import OrgModelPricesPage from '../OrgModelPricesPage';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getModelPriceList: vi.fn(),
  getModelNames: vi.fn(),
  getOrgModelPrices: vi.fn(),
  createModelPrice: vi.fn(),
  updateModelPrice: vi.fn(),
  deleteModelPrice: vi.fn(),
  createOrgModelPrice: vi.fn(),
  updateOrgModelPrice: vi.fn(),
  deleteOrgModelPrice: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));

describe('pricing authority pages', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getModelPriceList.mockResolvedValue({
      data: [{ id: 7, model_name: 'gpt-4o', input_price_cents: 5, output_price_cents: 15, cache_read_ratio: 0.1, cache_creation_ratio: 1.25, updated_at: '2026-07-11T00:00:00Z' }],
      total: 1,
    });
    mocks.getModelNames.mockResolvedValue({ data: ['gpt-4o'] });
    mocks.getOrgModelPrices.mockResolvedValue({
      data: [{ id: 9, org_id: 22, org_name: 'Acme', model_name: 'gpt-4o', multiplier: 1.5, updated_at: '2026-07-11T00:00:00Z' }],
      total: 1,
      org_id: 22,
    });
  });

  it('keeps global prices visible but read-only for organization administrators', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org', role: 'org_admin', org_id: 22 } });

    render(<ModelPricesPage />);

    expect(await screen.findByText('模型定价')).toBeTruthy();
    await waitFor(() => expect(mocks.getModelPriceList).toHaveBeenCalledWith(1, 20));
    expect(screen.getByText('只读查看平台统一维护的基础模型价格与供应商 Prompt Cache 倍率。')).toBeTruthy();
    expect(screen.getByText('0.100×')).toBeTruthy();
    expect(screen.getByText('1.250×')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /新增价格/ })).toBeNull();
    expect(screen.queryByText('操作')).toBeNull();
  });

  it('shows global price mutation controls only to platform administrators', async () => {
    useAuthStore.setState({ user: { id: 1, username: 'platform', role: 'platform_admin', org_id: 11 } });

    render(<ModelPricesPage />);

    expect(await screen.findByRole('button', { name: /新增价格/ })).toBeTruthy();
    expect(screen.getByText('操作')).toBeTruthy();
  });

  it('loads an organization administrator own multiplier without a hard-coded org id', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org', role: 'org_admin', org_id: 22 } });

    render(<OrgModelPricesPage />);

    expect(await screen.findByText('组织计费倍率')).toBeTruthy();
    await waitFor(() => expect(mocks.getOrgModelPrices).toHaveBeenCalledWith(1, 20, undefined));
    expect(await screen.findByText('1.5×')).toBeTruthy();
    expect(screen.getByText('Acme（ID 22）')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /新增倍率/ })).toBeNull();
    expect(screen.queryByLabelText('目标组织 ID')).toBeNull();
  });

  it('lets a platform administrator explicitly select a target organization', async () => {
    useAuthStore.setState({ user: { id: 1, username: 'platform', role: 'platform_admin', org_id: 11 } });

    render(<OrgModelPricesPage />);

    const input = await screen.findByLabelText('目标组织 ID');
    fireEvent.change(input, { target: { value: '22' } });
    fireEvent.click(screen.getByRole('button', { name: '查看目标组织' }));

    await waitFor(() => expect(mocks.getOrgModelPrices).toHaveBeenCalledWith(1, 20, 22));
    expect(screen.getByRole('button', { name: /新增倍率/ })).toBeTruthy();
  });
});
