import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import IntegrationsPage from '../IntegrationsPage';

const dataExchangeMount = vi.hoisted(() => vi.fn());
vi.mock('../DataExchangePage', () => ({ default: () => { dataExchangeMount(); return <div>X-Insight 连接工作区</div>; } }));

describe('IntegrationsPage', () => {
  it('keeps the Reference Connector isolated and does not probe it until selected', () => {
    render(<MemoryRouter><IntegrationsPage /></MemoryRouter>);

    expect(screen.getByRole('heading', { name: '集成管理' })).toBeTruthy();
    expect(screen.getByText('X-Insight completed-execution Feed')).toBeTruthy();
    expect(screen.getByText(/首个 Reference Connector/)).toBeTruthy();
    expect(screen.getByText('飞书企业机器人')).toBeTruthy();
    expect(screen.getByText(/不合并为高权限万能插件/)).toBeTruthy();
    expect(screen.getAllByText(/不进入 UniRouter 网关进程/).length).toBeGreaterThan(0);
    expect(screen.getByText('可配置')).toBeTruthy();
    expect(dataExchangeMount).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('tab', { name: 'X-Insight' }));
    expect(screen.getByText('X-Insight 连接工作区')).toBeTruthy();
    expect(dataExchangeMount).toHaveBeenCalledTimes(1);
  });
});
