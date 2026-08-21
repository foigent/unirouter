import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import ConnectionModelPanel, { modelIsPublished, type ModelConnectionChannel } from '../ConnectionModelPanel';

const channel: ModelConnectionChannel = {
  id: 12,
  name: 'DeepSeek',
  provider: 'deepseek',
  models: 'deepseek-v4-flash,deepseek-v4-pro',
  status: 'staged',
  routing_epoch: 4,
};

function renderPanel(selectedChannel = channel, modelsPublishedElsewhere = new Set<string>()) {
  const props = {
    channel: selectedChannel,
    modelsPublishedElsewhere,
    onOpenRouteManagement: vi.fn(),
    onDiagnose: vi.fn(),
  };
  render(<ConnectionModelPanel {...props} />);
  return props;
}

describe('ConnectionModelPanel', () => {
  it('is a read-only retirement notice with no legacy detection or credential action', () => {
    renderPanel();

    expect(screen.getByText(/旧连接检测、端点启用和凭证写入口已退役并固定返回 410/)).toBeTruthy();
    expect(screen.getAllByRole('button', { name: '前往模型发布工作台' })).toHaveLength(2);
    expect(screen.queryByRole('button', { name: /检测/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /凭证/ })).toBeNull();
  });

  it('forwards replacement and retained-release recovery intents to the canonical workspace', () => {
    const replacement = renderPanel(channel, new Set(['deepseek-v4-pro']));
    const replacementRow = screen.getByText('deepseek-v4-pro').closest('tr')!;
    fireEvent.click(within(replacementRow).getByRole('button', { name: '前往模型发布工作台' }));
    expect(replacement.onOpenRouteManagement).toHaveBeenCalledWith('deepseek-v4-pro', 'replace');

    const retainedChannel = {
      ...channel,
      status: 'disabled',
      effective_status: 'disabled',
      published_models: 'deepseek-v4-flash',
    };
    const retained = renderPanel(retainedChannel);
    const retainedRows = screen.getAllByText('deepseek-v4-flash');
    const retainedRow = retainedRows[retainedRows.length - 1].closest('tr')!;
    fireEvent.click(within(retainedRow).getByRole('button', { name: '前往模型发布工作台' }));
    expect(retained.onOpenRouteManagement).toHaveBeenCalledWith('deepseek-v4-flash', 'recover');
  });

  it('keeps legacy publication detection as a read-only parser only', () => {
    expect(modelIsPublished({ published_models: 'deepseek-v4-flash' }, 'deepseek-v4-flash')).toBe(true);
    expect(modelIsPublished({ published_models: 'deepseek-v4-flash' }, 'deepseek-v4-pro')).toBe(false);
  });
});
