import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import IntegrationsPage from '../IntegrationsPage';

describe('IntegrationsPage', () => {
  it('keeps the Reference Connector and isolated-adapter boundaries explicit', () => {
    render(<MemoryRouter><IntegrationsPage /></MemoryRouter>);

    expect(screen.getByRole('heading', { name: '集成管理' })).toBeTruthy();
    expect(screen.getByText('X-Insight completed-execution Feed')).toBeTruthy();
    expect(screen.getByText('Reference Connector')).toBeTruthy();
    expect(screen.getByText('飞书企业机器人')).toBeTruthy();
    expect(screen.getByText(/不合并为高权限万能插件/)).toBeTruthy();
    expect(screen.getAllByText(/不进入 UniRouter 网关进程/).length).toBeGreaterThan(0);
  });
});
