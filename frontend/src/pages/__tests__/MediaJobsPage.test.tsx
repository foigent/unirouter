import { LanguageSwitcher, setLocale } from '../../i18n';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useAuthStore } from '../../store/auth';
import MediaJobsPage from '../MediaJobsPage';

const mocks = vi.hoisted(() => ({
  getProjects: vi.fn(),
  getMediaJobContract: vi.fn(),
  getMediaJobs: vi.fn(),
  cancelMediaJob: vi.fn(),
  getMediaJobContent: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: mocks,
  ApiError: class ApiError extends Error {
    status: number;
    payload: unknown;

    constructor(message: string, status: number, payload: unknown = null) {
      super(message);
      this.status = status;
      this.payload = payload;
    }
  },
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => { resolve = complete; });
  return { promise, resolve };
}

const JOB = {
  contract_version: 'unirouter.async-media-runtime.v1',
  job_id: 'mediajob_0123456789abcdef0123456789abcdef',
  scope: { org_id: 1, project_id: 7 },
  request_id: 'op_0123456789abcdef0123456789abcdef',
  operation: 'text_to_image',
  profile_id: 'unirouter.openai.images.gpt-image.v1',
  requested_model: 'company-image',
  routing_model: 'gpt-image-prod',
  mapped_model: 'gpt-image-1',
  request_sha256: `sha256:${'a'.repeat(64)}`,
  lifecycle: {
    admission: 'accepted',
    queue: 'terminal',
    lease: 'released',
    execution: 'terminal',
    provider_outcome: 'succeeded',
    cancellation: 'not_requested',
    object: 'persisted',
    settlement: 'charge_fact_recorded',
  },
  attempt: 1,
  max_attempts: 3,
  lease_fence: 1,
  next_attempt_at: '2026-07-31T08:00:00Z',
  output: {
    content_sha256: `sha256:${'b'.repeat(64)}`,
    media_type: 'image/png',
    size_bytes: 1024,
    persisted_at: '2026-07-31T08:01:00Z',
    expires_at: '2099-08-07T08:01:00Z',
  },
  charge_fact: {
    fact_id: 'mediacharge_0123456789abcdef0123456789abcdef',
    trigger: 'provider_terminal_success_and_output_persisted',
    amount_state: 'pricing_pending_media_rate_card',
    units: [{
      class: 'input_text_tokens',
      quantity: '12',
      authority: 'provider_reported',
      finality: 'final',
      charge_state: 'provider_observed_not_customer_chargeable',
    }],
    recorded_at: '2026-07-31T08:01:01Z',
  },
  created_at: '2026-07-31T08:00:00Z',
  updated_at: '2026-07-31T08:01:01Z',
  terminal_at: '2026-07-31T08:01:01Z',
};

describe('MediaJobsPage', () => {
  beforeEach(() => {
    setLocale('zh-CN');
    vi.clearAllMocks();
    window.history.replaceState(null, '', '/media-jobs');
    useAuthStore.setState({
      user: { id: 1, username: 'org-admin', role: 'org_admin', org_id: 1 },
      token: 'token',
      isAuthenticated: true,
      profileLoading: false,
    });
    mocks.getProjects.mockResolvedValue({
      data: [{ id: 7, org_id: 1, name: 'Production AI', status: 'active' }],
    });
    mocks.getMediaJobContract.mockResolvedValue({
      contract_version: 'unirouter.async-media-runtime.v1',
      profile_id: 'unirouter.openai.images.gpt-image.v1',
      retention: 'P7D',
      video_enabled: false,
      runtime_enabled: true,
      admission_enabled: true,
      charge_trigger: 'provider_terminal_success_and_output_persisted',
      amount_state: 'pricing_pending_media_rate_card',
    });
    mocks.getMediaJobs.mockResolvedValue({ data: [JOB] });
  });

  it('keeps the right inspector visible and exposes metadata without raw prompt or object locator', async () => {
    render(<MediaJobsPage />);

    expect(screen.getByLabelText('异步模型任务检查器')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '项目' })).toBeTruthy();
    expect((await screen.findAllByText('company-image')).length).toBeGreaterThan(0);
    expect(screen.getByText('pricing_pending_media_rate_card')).toBeTruthy();
    expect(screen.getByText(/没有扣减钱包或推断金额/)).toBeTruthy();
    expect(screen.getByRole('button', { name: '请求取消' })).toBeDisabled();
    expect(screen.queryByText(/private prompt/)).toBeNull();
    expect(document.body.textContent).not.toContain('object_key');
    expect(mocks.getMediaJobs).toHaveBeenCalledWith(7, 1);
  });

  it('does not claim there are no visible projects while the project scope is loading', async () => {
    const projectsRequest = deferred<any>();
    mocks.getProjects.mockReturnValue(projectsRequest.promise);

    render(<MediaJobsPage />);

    expect(await screen.findByRole('heading', { name: '正在读取项目范围' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: '没有可见项目' })).toBeNull();
    expect(screen.queryByText('P7D')).toBeNull();

    await act(async () => {
      projectsRequest.resolve({ data: [] });
      await Promise.resolve();
    });
    expect(await screen.findByRole('heading', { name: '没有可见项目' })).toBeTruthy();
    expect(mocks.getMediaJobContract).not.toHaveBeenCalled();
    expect(mocks.getMediaJobs).not.toHaveBeenCalled();
  });

  it('loads output only after an explicit authenticated action', async () => {
    const createObjectURL = vi.fn(() => 'blob:media-output');
    const revokeObjectURL = vi.fn();
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeObjectURL });
    mocks.getMediaJobContent.mockResolvedValue(new Blob(['image'], { type: 'image/png' }));

    render(<MediaJobsPage />);
    const button = await screen.findByRole('button', { name: '鉴权查看输出' });
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();

    fireEvent.click(button);
    await waitFor(() => {
      expect(mocks.getMediaJobContent).toHaveBeenCalledWith(JOB.job_id, 7, 1);
    });
    expect(await screen.findByAltText(`任务 ${JOB.job_id} 的图像输出`)).toBeTruthy();
  });

  it('distinguishes a stopped runtime from paused new-job admission', async () => {
    mocks.getMediaJobContract.mockResolvedValueOnce({
      contract_version: 'unirouter.async-media-runtime.v1',
      profile_id: 'unirouter.openai.images.gpt-image.v1',
      retention: 'P7D',
      video_enabled: false,
      runtime_enabled: false,
      admission_enabled: false,
      charge_trigger: 'provider_terminal_success_and_output_persisted',
      amount_state: 'pricing_pending_media_rate_card',
    });
    render(<MediaJobsPage />);
    expect(await screen.findByText(/未启用 Image V1 异步任务 worker 与对象存储/)).toBeTruthy();
    expect(screen.queryByText(/新任务受理已暂停/)).toBeNull();
    cleanup();

    mocks.getMediaJobContract.mockResolvedValueOnce({
      contract_version: 'unirouter.async-media-runtime.v1',
      profile_id: 'unirouter.openai.images.gpt-image.v1',
      retention: 'P7D',
      video_enabled: false,
      runtime_enabled: true,
      admission_enabled: false,
      charge_trigger: 'provider_terminal_success_and_output_persisted',
      amount_state: 'pricing_pending_media_rate_card',
    });
    render(<MediaJobsPage />);
    expect(await screen.findByText(/新任务受理已暂停/)).toBeTruthy();
    expect(screen.getByText(/现有任务的恢复、到期读取关闭与对象清理仍继续/)).toBeTruthy();
  });

  it('does not offer or count an already expired output', async () => {
    mocks.getMediaJobs.mockResolvedValue({
      data: [{
        ...JOB,
        output: { ...JOB.output, expires_at: '2000-01-01T00:00:00Z' },
      }],
    });
    render(<MediaJobsPage />);

    expect(await screen.findByText('输出可用')).toBeTruthy();
    expect(screen.getByText('到期后读取立即关闭').previousElementSibling?.textContent).toBe('0');
    expect(screen.queryByRole('button', { name: '鉴权查看输出' })).toBeNull();
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
  });
  it('switches locale without rereading a job or its output', async () => {
    setLocale('en-US');
    render(<><LanguageSwitcher /><MediaJobsPage /></>);
    expect(await screen.findByRole('heading', { name: 'Job queue' })).toBeTruthy();
    await screen.findAllByText('company-image');
    expect(screen.getByText('pricing_pending_media_rate_card')).toBeTruthy();
    const calls = mocks.getMediaJobs.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: '中文' }));
    expect(screen.getByRole('heading', { name: '任务队列' })).toBeTruthy();
    expect(mocks.getMediaJobs).toHaveBeenCalledTimes(calls);
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
    expect(mocks.cancelMediaJob).not.toHaveBeenCalled();
  });

  it('discards an old project response after the user changes scope', async () => {
    const oldRequest = deferred<any>();
    mocks.getProjects.mockResolvedValue({ data: [
      { id: 7, org_id: 1, name: 'First project', status: 'active' },
      { id: 8, org_id: 1, name: 'Second project', status: 'active' },
    ] });
    mocks.getMediaJobs.mockImplementation((id: number) => id === 7 ? oldRequest.promise : Promise.resolve({ data: [{ ...JOB, job_id: 'job_second_project', requested_model: 'second-model', scope: { org_id: 1, project_id: 8 } }] }));
    render(<MediaJobsPage />);
    await waitFor(() => expect(mocks.getMediaJobs).toHaveBeenCalledWith(7, 1));
    fireEvent.click(screen.getByRole('combobox', { name: '项目' }));
    fireEvent.click(await screen.findByText('Second project'));
    await waitFor(() => expect(mocks.getMediaJobs).toHaveBeenCalledWith(8, 1));
    await screen.findAllByText('second-model');
    await act(async () => oldRequest.resolve({ data: [JOB] }));
    expect(screen.queryByText('company-image')).toBeNull();
    expect(screen.getByText('job_second_project')).toBeTruthy();
    expect(mocks.cancelMediaJob).not.toHaveBeenCalled();
  });

});
