import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ProjectBudgetPanel from '../ProjectBudgetPanel';

const mocks = vi.hoisted(() => ({
  getProjects: vi.fn(),
  updateProjectBudget: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));

describe('ProjectBudgetPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getProjects.mockResolvedValue({
      data: [{ id: 31, name: '核心项目', slug: 'core', status: 'active', budget_cents: 123400, budget_period: 'monthly' }],
      total: 1,
    });
    mocks.updateProjectBudget.mockResolvedValue({ project_id: 31, budget_cents: 123400, budget_period: 'monthly' });
  });

  it('exposes only project budget fields to the finance surface', async () => {
    render(<ProjectBudgetPanel />);

    expect(await screen.findByText('核心项目')).toBeTruthy();
    expect(screen.getByText('¥1234.00 / 月')).toBeTruthy();
    fireEvent.click(screen.getByText('调整预算'));
    expect(await screen.findByText('调整预算 · 核心项目')).toBeTruthy();
    expect(screen.getByLabelText('预算金额（元）')).toBeTruthy();
    expect(screen.getByLabelText('预算周期')).toBeTruthy();
    expect(screen.queryByText('项目名称')).toBeNull();

    fireEvent.click(screen.getByText('保存预算'));
    await waitFor(() => expect(mocks.updateProjectBudget).toHaveBeenCalledWith(31, {
      budget_cents: 123400,
      budget_period: 'monthly',
    }));
  });
});
