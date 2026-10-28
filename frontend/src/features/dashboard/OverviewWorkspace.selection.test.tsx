import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from '../../test/MemoryRouter';
import { I18nProvider, setLocale } from '../../i18n';
import { useAuthStore } from '../../store/auth';
import type { DashboardStats } from '../../api/dashboard';
import OverviewWorkspace from './OverviewWorkspace';

const stats = (names: string[]) => ({
  total_requests: names.length, today_cost_micros: '0', currency: 'CNY', amount_scale: 1000000,
  avg_latency_ms: 1, p95_latency_ms: 1, p99_latency_ms: 1, success_rate: 1,
  recent_logs: [], daily_trend: [], channel_health: [],
  model_distribution: names.map((model, i) => ({ model, requests: names.length - i,
    cost_micros: '0', success_count: 1, fail_count: 0, success_rate: 1, avg_latency_ms: 1, p95_latency_ms: 1 })),
}) as DashboardStats;
const tree = (names: string[]) => <MemoryRouter><I18nProvider><OverviewWorkspace stats={stats(names)} attention={null} attentionState="idle" /></I18nProvider></MemoryRouter>;
const picker = () => screen.getByRole('combobox', { name: '选择模型' });

beforeEach(() => {
  setLocale('zh-CN');
  useAuthStore.setState({ user: { id: 1, username: 'viewer', org_id: 1, role: 'org_viewer' } });
});
describe('overview model selection across actual data updates', () => {
  it('selects the current leader when an initially empty window gains data', () => {
    const view = render(tree([]));
    expect(picker()).toHaveValue('');
    view.rerender(tree(['first', 'second']));
    expect(picker()).toHaveValue('first');
    view.rerender(tree(['new-leader', 'first']));
    expect(picker()).toHaveValue('new-leader');
  });
  it('retains an explicitly selected model while it still exists', () => {
    const view = render(tree(['first', 'second']));
    fireEvent.change(picker(), { target: { value: 'second' } });
    view.rerender(tree(['new-leader', 'second']));
    expect(picker()).toHaveValue('second');
  });
  it('explains fallback when a selected model disappears and remembers it on return', () => {
    const view = render(tree(['first', 'second']));
    fireEvent.change(picker(), { target: { value: 'second' } });
    view.rerender(tree(['new-leader']));
    expect(picker()).toHaveValue('new-leader');
    expect(screen.getByRole('status')).toHaveTextContent('所选模型 second 在当前窗口暂无数据');
    view.rerender(tree([]));
    expect(picker()).toHaveValue('');
    view.rerender(tree(['first', 'second']));
    expect(picker()).toHaveValue('second');
  });
  it('preserves explicit All models across empty and populated windows', () => {
    const view = render(tree(['first', 'second']));
    fireEvent.change(picker(), { target: { value: '' } });
    view.rerender(tree([]));
    view.rerender(tree(['new-leader', 'first']));
    expect(picker()).toHaveValue('');
  });
});

describe('overview empty-window presentation', () => {
  it('keeps recent requests visible independently of a zero window and omits zero activity charts', () => {
    const snapshot = { ...stats([]), daily_trend: [{ date: '2026-10-28T00:00:00Z', requests: 0, cost_micros: '0' }], recent_logs: [{ request_id: 'older-visible-request', model: 'older-model', status_code: 200, cost_micros: '100', latency_ms: 30, created_at: '2026-10-20T00:00:00Z' }] } as DashboardStats;
    const view = render(<MemoryRouter><OverviewWorkspace stats={snapshot} attention={null} attentionState="idle" /></MemoryRouter>);
    expect(screen.getByText('所选时段暂无请求与费用')).toBeInTheDocument();
    expect(screen.getByText('older-visible-request')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '查看调用指南' })).toBeInTheDocument();
    expect(screen.getByText('所选时段暂无趋势样本')).toBeInTheDocument();
    expect(view.container.querySelector('.recharts-area-curve')).toBeNull();
  });
  it('does not call unavailable model or cost evidence a zero-activity window', () => {
    const snapshot = { ...stats([]), model_distribution: undefined, today_cost_micros: undefined } as unknown as DashboardStats;
    render(<MemoryRouter><OverviewWorkspace stats={snapshot} attention={null} attentionState="idle" /></MemoryRouter>);
    expect(screen.queryByText('所选时段暂无请求与费用')).not.toBeInTheDocument();
    expect(screen.getByText('模型构成暂不可用')).toBeInTheDocument();
    expect(screen.getByText('费用构成暂不可用')).toBeInTheDocument();
  });
});
