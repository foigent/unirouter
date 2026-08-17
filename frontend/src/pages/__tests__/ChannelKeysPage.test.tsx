import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import ChannelKeysPage from '../ChannelKeysPage';

const mockGetChannelKeys = vi.fn();
const mockGetChannels = vi.fn();
const mockUpdateChannelKey = vi.fn();
const mockCreateChannelKey = vi.fn();
const mockRotateChannelKey = vi.fn();

vi.mock('../../api/channels', () => ({
  channelApi: {
    list: (...args: unknown[]) => mockGetChannels(...args),
  },
  providerCredentialApi: {
    list: (...args: unknown[]) => mockGetChannelKeys(...args),
    update: (...args: unknown[]) => mockUpdateChannelKey(...args),
    create: (...args: unknown[]) => mockCreateChannelKey(...args),
    rotate: (...args: unknown[]) => mockRotateChannelKey(...args),
    remove: vi.fn(),
  },
}));

function renderChannelKeys(entry = '/channels?tab=credentials') {
  return render(<MemoryRouter initialEntries={[entry]}><ChannelKeysPage /></MemoryRouter>);
}

describe('ChannelKeysPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetChannelKeys.mockResolvedValue({
      data: [{
        id: 7,
        channel_id: 3,
        channel_name: 'mimo',
        channel_status: 'staged',
        key_prefix: '****c20c',
        allowed_models: 'mimo-v2.5-pro，mimo-v2.5',
        status: 'active',
        state_epoch: 2,
        created_at: '2026-07-01T00:00:00Z',
		updated_at: '2026-08-14T01:59:49+08:00',
      }],
      total: 1,
    });
    mockGetChannels.mockResolvedValue({ data: [{ id: 3, name: 'mimo', status: 'staged', models: 'mimo-v2.5-pro,mimo-v2.5' }] });
    mockUpdateChannelKey.mockResolvedValue({ success: true });
    mockCreateChannelKey.mockResolvedValue({ id: 8 });
    mockRotateChannelKey.mockResolvedValue({ id: 8, probe: { latency_ms: 12 } });
  });

  it('updates model scope without forcing an upstream secret rotation', async () => {
    renderChannelKeys();

    await screen.findByText('mimo-v2.5-pro，mimo-v2.5');
    fireEvent.click(screen.getByRole('button', { name: '编辑供应商凭证 mimo' }));

	expect(await screen.findByLabelText('新的供应商 API Key（可选）')).toHaveAttribute('type', 'password');
    const allowedModels = await screen.findByLabelText('允许模型（留空=全部）');
    fireEvent.change(allowedModels, { target: { value: 'mimo-v2.5-pro；mimo-v2.5;mimo-v2.5-pro' } });
	fireEvent.click(screen.getByText('保存修改'));

    await waitFor(() => expect(mockUpdateChannelKey).toHaveBeenCalledWith(7, {
      channel_id: 3,
      allowed_models: 'mimo-v2.5-pro,mimo-v2.5',
      status: 'active',
    }));
  });

  it('submits a newly entered supplier API key when rotating a staged connection credential', async () => {
    renderChannelKeys();

    await screen.findByText('mimo-v2.5-pro，mimo-v2.5');
    fireEvent.click(screen.getByRole('button', { name: '编辑供应商凭证 mimo' }));

	fireEvent.change(await screen.findByLabelText('新的供应商 API Key（可选）'), { target: { value: 'sk-rotated-test-key' } });
	fireEvent.click(screen.getByText('保存修改'));

    await waitFor(() => expect(mockUpdateChannelKey).toHaveBeenCalledWith(7, {
      channel_id: 3,
      key: 'sk-rotated-test-key',
      allowed_models: 'mimo-v2.5-pro,mimo-v2.5',
      status: 'active',
    }));
	expect(await screen.findByText(/mimo 的新供应商 API Key 已加密保存/)).toBeTruthy();
  });

  it('tests and atomically rotates an active route credential without pausing the endpoint', async () => {
    mockGetChannelKeys.mockResolvedValueOnce({
      data: [{
        id: 7, channel_id: 3, channel_name: 'mimo', channel_status: 'active',
        key_prefix: '****c20c', allowed_models: 'mimo-v2.5-pro,mimo-v2.5',
        status: 'active', state_epoch: 4, created_at: '2026-07-01T00:00:00Z', updated_at: '2026-08-14T01:59:49+08:00',
      }],
      total: 1,
    });
    mockGetChannels.mockResolvedValueOnce({ data: [{ id: 3, name: 'mimo', status: 'active', models: 'mimo-v2.5-pro,mimo-v2.5' }] });
    renderChannelKeys();

    await screen.findByText('mimo-v2.5-pro,mimo-v2.5');
    fireEvent.click(screen.getByRole('button', { name: '轮换供应商凭证 mimo' }));
    expect(await screen.findByText(/只有检测成功且端点\/旧凭证没有并发变化/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText('新的供应商 API Key'), { target: { value: 'sk-new-active-key' } });
    fireEvent.click(screen.getByRole('button', { name: '检测并原子轮换' }));

    await waitFor(() => expect(mockRotateChannelKey).toHaveBeenCalledWith(7, {
      key: 'sk-new-active-key',
      model: 'mimo-v2.5-pro',
      allowed_models: 'mimo-v2.5-pro,mimo-v2.5',
    }));
    expect(await screen.findByText(/路由未暂停。检测延迟 12ms/)).toBeTruthy();
  });

  it('keeps scoped active-endpoint credential management on the atomic rotation path', async () => {
    mockGetChannelKeys.mockResolvedValueOnce({
      data: [{
        id: 7, channel_id: 3, channel_name: 'mimo', channel_status: 'active',
        key_prefix: '****c20c', allowed_models: 'mimo-v2.5-pro',
        status: 'active', state_epoch: 4, created_at: '2026-07-01T00:00:00Z', updated_at: '2026-08-14T01:59:49+08:00',
      }],
      total: 1,
    });
    mockGetChannels.mockResolvedValueOnce({ data: [{ id: 3, name: 'mimo', status: 'active', models: 'mimo-v2.5-pro' }] });

    render(
      <MemoryRouter>
        <ChannelKeysPage embedded scoped channelId={3} model="mimo-v2.5-pro" />
      </MemoryRouter>,
    );

    const createButton = (await screen.findByText('新增供应商凭证')).closest('button');
    expect(createButton).not.toBeNull();
    expect(createButton).toBeDisabled();
    expect(createButton?.parentElement).toHaveAttribute('title', expect.stringContaining('轮换'));
    expect(screen.getByRole('button', { name: '轮换供应商凭证 mimo' })).toBeEnabled();
    fireEvent.click(createButton!);
    expect(screen.queryByRole('heading', { name: '新增供应商凭证' })).toBeNull();
  });

  it('keeps the active credential and route unchanged when replacement probing fails', async () => {
    mockGetChannelKeys.mockResolvedValueOnce({
      data: [{
        id: 7, channel_id: 3, channel_name: 'mimo', channel_status: 'active',
        key_prefix: '****c20c', allowed_models: 'mimo-v2.5-pro',
        status: 'active', state_epoch: 4, created_at: '2026-07-01T00:00:00Z', updated_at: '2026-08-14T01:59:49+08:00',
      }], total: 1,
    });
    mockGetChannels.mockResolvedValueOnce({ data: [{ id: 3, name: 'mimo', status: 'active', models: 'mimo-v2.5-pro' }] });
    mockRotateChannelKey.mockRejectedValueOnce(new Error('上游拒绝新 Key'));
    renderChannelKeys();

    await screen.findByText('mimo-v2.5-pro');
    fireEvent.click(screen.getByRole('button', { name: '轮换供应商凭证 mimo' }));
    fireEvent.change(await screen.findByLabelText('新的供应商 API Key'), { target: { value: 'sk-bad-key' } });
    fireEvent.click(screen.getByRole('button', { name: '检测并原子轮换' }));

    expect((await screen.findAllByText(/旧凭证仍保持启用，未改变当前路由/)).length).toBeGreaterThan(0);
  });

  it('keeps a supplier credential save failure visible in the editor', async () => {
	mockUpdateChannelKey.mockRejectedValueOnce(new Error('数据库暂不可用'));
	renderChannelKeys();

	await screen.findByText('mimo-v2.5-pro，mimo-v2.5');
	fireEvent.click(screen.getByRole('button', { name: '编辑供应商凭证 mimo' }));
	fireEvent.change(await screen.findByLabelText('新的供应商 API Key（可选）'), { target: { value: 'sk-rejected-test-key' } });
	fireEvent.click(screen.getByText('保存修改'));

	expect((await screen.findAllByText('保存失败：数据库暂不可用')).length).toBeGreaterThan(0);
	expect(screen.getByRole('heading', { name: /编辑供应商凭证/ })).toBeTruthy();
  });

  it('shows an explicit retry state when supplier credentials cannot be loaded', async () => {
    mockGetChannelKeys.mockRejectedValueOnce(new Error('unavailable'));

    renderChannelKeys();

    expect(await screen.findByRole('heading', { name: '供应商凭证加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载供应商凭证' })).toBeTruthy();
  });

  it('does not let an unrelated active endpoint block credential creation for the selected staged endpoint', async () => {
    mockGetChannels.mockResolvedValueOnce({
      data: [
        { id: 3, name: 'Deepseek', status: 'staged', models: 'deepseek-v4-pro' },
        { id: 14, name: 'Kimi', status: 'staged', effective_status: 'active', models: 'kimi-k3' },
      ],
    });

    renderChannelKeys();
    fireEvent.click(await screen.findByText('新增供应商凭证'));

    expect(screen.queryByText(/Kimi 正在承载路由/)).toBeNull();
    fireEvent.click(screen.getByRole('combobox', { name: '供应商端点' }));
    const runtimeOption = await screen.findByText('Kimi · 生效（运行时发布）');
    expect(runtimeOption.closest('.semi-select-option')).toHaveClass('semi-select-option-disabled');
    fireEvent.click(screen.getByText('Deepseek · 待发布'));
    fireEvent.change(await screen.findByLabelText('供应商 API Key'), { target: { value: 'sk-deepseek-test' } });
    fireEvent.click(screen.getByRole('button', { name: '创建' }));

    await waitFor(() => expect(mockCreateChannelKey).toHaveBeenCalledWith(expect.objectContaining({
      channel_id: 3,
      key: 'sk-deepseek-test',
    })));
  });

  it('loads and preselects the exact endpoint context from model and endpoint actions', async () => {
    renderChannelKeys('/channels?tab=credentials&channel_id=3&model=mimo-v2.5&from=connections');

    expect(await screen.findByText('当前只显示 mimo · mimo-v2.5 的凭证。')).toBeTruthy();
    expect(mockGetChannelKeys).toHaveBeenCalledWith(1, 20, 3);
    fireEvent.click(screen.getByRole('button', { name: /新增供应商凭证/ }));
    await waitFor(() => expect(screen.getByRole('combobox', { name: '供应商端点' })).toHaveTextContent('mimo'));
    expect(mockCreateChannelKey).not.toHaveBeenCalled();
  });

  it('explains an enabled credential on a disabled channel with no model overlap', async () => {
    mockGetChannelKeys.mockResolvedValueOnce({
      data: [{
        id: 6,
        channel_id: 6,
        channel_name: 'kimi',
        channel_status: 'disabled',
        key_prefix: '****89ec',
        allowed_models: 'Kimi K2.7 Code,kimi-k2.6',
        status: 'active',
        fail_count: 0,
        cooldown_until: '2026-07-01T00:52:45Z',
        created_at: '2026-06-27T05:05:15Z',
      }],
      total: 1,
    });
    mockGetChannels.mockResolvedValueOnce({
      data: [{
        id: 6,
        name: 'kimi',
        status: 'disabled',
        models: 'moonshot-v1-8k,moonshot-v1-32k,moonshot-v1-128k',
      }],
    });

    renderChannelKeys();

    expect(await screen.findByText('凭证启用')).toBeTruthy();
    expect(screen.getByText('端点暂停')).toBeTruthy();
    expect(screen.getByText('模型范围无交集')).toBeTruthy();
    expect(screen.getByText('阻断：模型无交集')).toBeTruthy();
    expect(screen.getByText(/1 条启用凭证绑定待发布\/禁用端点，1 条凭证与端点模型范围无交集/)).toBeTruthy();
    expect(screen.queryByText(/冷却至/)).toBeNull();
  });
});
