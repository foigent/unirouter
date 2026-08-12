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
  getOrganizations: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));

describe('pricing authority pages', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getModelPriceList.mockResolvedValue({
		data: [{ id: 7, model_name: 'gpt-4o', input_price_cny_per_million_tokens: '50', output_price_cny_per_million_tokens: '150', cache_read_ratio: '0.1', cache_creation_ratio: '1.25', updated_at: '2026-07-11T00:00:00Z' }],
      total: 1,
    });
    mocks.getModelNames.mockResolvedValue({ data: ['gpt-4o'] });
    mocks.getOrgModelPrices.mockResolvedValue({
		data: [{ id: 9, org_id: 22, org_name: 'Acme', model_name: 'gpt-4o', multiplier: '1.5', updated_at: '2026-07-11T00:00:00Z' }],
      total: 1,
      org_id: 22,
    });
    mocks.getOrganizations.mockResolvedValue({
      data: [
        { id: 11, name: 'Platform' },
        { id: 22, name: 'Acme' },
      ],
    });
  });

  it('keeps global prices visible but read-only for organization administrators', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'org', role: 'org_admin', org_id: 22 } });

    render(<ModelPricesPage />);

    expect(await screen.findByText('模型定价')).toBeTruthy();
    await waitFor(() => expect(mocks.getModelPriceList).toHaveBeenCalledWith(1, 20));
    expect(screen.getByText('只读查看平台统一维护的 CNY/百万 tokens 基础价格与供应商 Prompt Cache 倍率。')).toBeTruthy();
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

  it('keeps the model identity immutable while editing and omits it from updates', async () => {
    useAuthStore.setState({ user: { id: 1, username: 'platform', role: 'platform_admin', org_id: 11 } });

    render(<ModelPricesPage />);

    fireEvent.click(await screen.findByRole('button', { name: '编辑价格' }));
    const modelName = await screen.findByLabelText('模型名称') as HTMLInputElement;
    await waitFor(() => expect(modelName.value).toBe('gpt-4o'));
    expect(modelName.disabled).toBe(true);
    expect(screen.getByText(/模型名称是计费、组织倍率、路由和模型治理共同使用的稳定标识/)).toBeTruthy();
	const inputPrice = screen.getByLabelText('输入价格（CNY/百万 tokens）') as HTMLInputElement;
	await waitFor(() => expect(inputPrice.value).toBe('50'));

    fireEvent.click(screen.getByRole('button', { name: '更新' }));
    await waitFor(() => expect(mocks.updateModelPrice).toHaveBeenCalledWith(7, {
      input_price_cny_per_million_tokens: '50',
      output_price_cny_per_million_tokens: '150',
      cache_read_ratio: '0.1',
      cache_creation_ratio: '1.25',
    }));
    expect(mocks.updateModelPrice.mock.calls[0][1]).not.toHaveProperty('model_name');
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

  it('binds a platform administrator multiplier to the canonical customer', async () => {
    useAuthStore.setState({ user: {
      id: 1, username: 'platform', role: 'platform_admin', org_id: 11,
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
      identity_scope: 'deployment_operator',
    } });

    render(<OrgModelPricesPage />);

    expect(await screen.findByText('客户范围：Acme')).toBeTruthy();
    expect(screen.queryByText('目标组织')).toBeNull();
    await waitFor(() => expect(mocks.getOrgModelPrices).toHaveBeenCalledWith(1, 20, 22));
    expect(screen.getByRole('button', { name: /新增倍率/ })).toBeTruthy();
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
  });

  it('distinguishes a global price load failure from an empty price catalog', async () => {
    useAuthStore.setState({ user: { id: 1, username: 'platform', role: 'platform_admin', org_id: 11 } });
    mocks.getModelPriceList.mockRejectedValueOnce(new Error('unavailable'));

    render(<ModelPricesPage />);

    expect(await screen.findByRole('heading', { name: '模型价格加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载模型价格' })).toBeTruthy();
  });
});
