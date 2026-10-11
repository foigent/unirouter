import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from '../../test/MemoryRouter';
import { describe, expect, it, vi } from 'vitest';
import IntegrationsPage from '../IntegrationsPage';

const dataExchangeRender = vi.hoisted(() => vi.fn());
vi.mock('../DataExchangePage', () => ({ default: () => { dataExchangeRender(); return <div>X-Insight 连接工作区</div>; } }));

describe('IntegrationsPage', () => {
  it('keeps the Reference Connector isolated and does not render it until selected', () => {
    render(<MemoryRouter><IntegrationsPage /></MemoryRouter>);

    expect(screen.getByRole('heading', { name: '集成管理' })).toBeTruthy();
    expect(screen.getByText('X-Insight 执行记录订阅')).toBeTruthy();
    expect(screen.getByText(/UniRouter 提供来源事实/)).toBeTruthy();
    expect(screen.getByText(/X-Insight 异步接收已授权事实/)).toBeTruthy();
    expect(screen.getByText(/已部署的受信 X-Insight/)).toBeTruthy();
    expect(screen.getByText(/可配置不代表公开托管服务或已经连接/)).toBeTruthy();
    expect(screen.getByText('飞书企业机器人')).toBeTruthy();
    expect(screen.getByText(/不会自动获得模型、管理员或财务权限/)).toBeTruthy();
    expect(screen.getByText(/凭据与事件处理留在 Adapter/)).toBeTruthy();
    expect(screen.getByText('可配置')).toBeTruthy();
    expect(dataExchangeRender).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('tab', { name: 'X-Insight' }));
    expect(screen.getByText('X-Insight 连接工作区')).toBeTruthy();
    expect(dataExchangeRender).toHaveBeenCalled();
  });
});
