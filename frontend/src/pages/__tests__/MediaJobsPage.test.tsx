import { Blob as NodeBlob } from 'node:buffer';
import { LanguageSwitcher, setLocale } from '../../i18n';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
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

function providerUnit(meterClass: string, quantity?: string | null) {
  return {
    class: meterClass,
    quantity,
    authority: 'provider_reported',
    finality: 'final',
    charge_state: 'provider_observed_not_customer_chargeable',
  };
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
    units: [
      providerUnit('input_tokens', '50'),
      providerUnit('input_text_tokens', '10'),
      providerUnit('input_image_tokens', '40'),
      providerUnit('output_image_tokens', '50'),
    ],
    recorded_at: '2026-07-31T08:01:01Z',
  },
  created_at: '2026-07-31T08:00:00Z',
  updated_at: '2026-07-31T08:01:01Z',
  terminal_at: '2026-07-31T08:01:01Z',
};

function setOutput(blob: NodeBlob, mediaType = blob.type, operation = JOB.operation) {
  mocks.getMediaJobs.mockResolvedValue({
    data: [{ ...JOB, operation, output: { ...JOB.output, media_type: mediaType, size_bytes: blob.size } }],
  });
  mocks.getMediaJobContent.mockResolvedValue(blob);
}

describe('MediaJobsPage', () => {
  const createObjectURL = vi.fn(() => 'blob:media-output');
  const revokeObjectURL = vi.fn();

  beforeEach(() => {
    setLocale('zh-CN');
    vi.clearAllMocks();
    vi.stubGlobal('URL', class extends URL {
      static createObjectURL = createObjectURL;
      static revokeObjectURL = revokeObjectURL;
    });
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

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('keeps the right inspector visible and exposes metadata without raw prompt or object locator', async () => {
    render(<MediaJobsPage />);

    expect(screen.getByLabelText('异步模型任务检查器')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '项目' })).toBeTruthy();
    expect((await screen.findAllByText('company-image')).length).toBeGreaterThan(0);
    const billing = within(screen.getByRole('region', { name: '计费事实' }));
    expect(billing.getByText('待媒体价目表')).toBeTruthy();
    expect(billing.getByText('供应商终态成功且输出已保存')).toBeTruthy();
    expect(billing.getByText('输入总 Token').parentElement).toHaveTextContent('50');
    expect(billing.getByText('输入文本 Token').parentElement).toHaveTextContent('10');
    expect(billing.getByText('输入图像 Token').parentElement).toHaveTextContent('40');
    expect(billing.getByText('输出图像 Token').parentElement).toHaveTextContent('50');
    expect(billing.getAllByText('计量来源：供应商报告')).toHaveLength(4);
    expect(billing.queryByText(/¥|CNY|钱包/)).toBeNull();
    expect(screen.getByRole('button', { name: '请求取消' })).toBeDisabled();
    expect(screen.queryByText(/private prompt/)).toBeNull();
    expect(document.body.textContent).not.toContain('object_key');
    expect(mocks.getMediaJobs).toHaveBeenCalledWith(7, 1);
  });

  it('shows Qwen image counts including an explicit zero for a project viewer in both languages', async () => {
    useAuthStore.setState({ user: { id: 2, username: 'project-reader', role: 'member', org_id: 1 } });
    mocks.getProjects.mockResolvedValue({ data: [
      { id: 7, org_id: 1, name: 'Own project', status: 'active', current_user_membership_role: 'project_viewer' },
      { id: 8, org_id: 1, name: 'Other project', status: 'active' },
    ] });
    mocks.getMediaJobs.mockResolvedValue({ data: [
      {
        ...JOB, requested_model: 'qwen-image-3.0-fixture',
        charge_fact: { ...JOB.charge_fact, units: [providerUnit('input_images', '0'), providerUnit('output_images', '1')] },
      },
      { ...JOB, scope: { org_id: 1, project_id: 8 }, requested_model: 'outside-project-fixture' },
    ] });
    render(<><LanguageSwitcher /><MediaJobsPage /></>);
    const billing = within(await screen.findByRole('region', { name: '计费事实' }));
    expect(billing.getByText('输入图片数').parentElement).toHaveTextContent('0');
    expect(billing.getByText('输出图片数').parentElement).toHaveTextContent('1');
    expect(billing.getAllByText('计量来源：供应商报告')).toHaveLength(2);
    expect(billing.getByText('待媒体价目表')).toBeTruthy();
    expect(billing.queryByText('用量未记录')).toBeNull();
    expect(screen.queryByText('outside-project-fixture')).toBeNull();
    expect(screen.getByRole('button', { name: '请求取消' })).toBeDisabled();
    expect(mocks.getMediaJobs).toHaveBeenCalledWith(7, 1);
    expect(mocks.getMediaJobs).not.toHaveBeenCalledWith(8, 1);
    fireEvent.click(screen.getByRole('button', { name: 'English' }));
    const english = within(screen.getByRole('region', { name: 'Charge facts' }));
    expect(english.getByText('Input image count').parentElement).toHaveTextContent('0');
    expect(english.getByText('Output image count').parentElement).toHaveTextContent('1');
    expect(english.getAllByText('Metering source: Provider reported')).toHaveLength(2);
    expect(english.getByText('Awaiting media pricing')).toBeTruthy();
    expect(mocks.getMediaJobs).toHaveBeenCalledTimes(1);
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
  });

  it.each([
    {
      sample: 'MiniMax TTS',
      units: [providerUnit('input_billable_characters', '18')],
      chinese: [['计价输入字符数', '18']],
      english: [['Billable input characters', '18']],
    },
    {
      sample: 'Qwen 3.1 ASR',
      units: [
        providerUnit('input_tokens', '78'),
        providerUnit('output_tokens', '16'),
        providerUnit('input_audio_milliseconds', '6000'),
      ],
      chinese: [['输入总 Token', '78'], ['输出总 Token', '16'], ['输入音频时长', '6,000 毫秒']],
      english: [['Total input tokens', '78'], ['Total output tokens', '16'], ['Input audio duration', '6,000 ms']],
    },
    {
      sample: 'gpt-transcribe',
      units: [
        providerUnit('input_tokens', '14'),
        providerUnit('input_audio_tokens', '14'),
        providerUnit('input_text_tokens', '0'),
        providerUnit('output_tokens', '45'),
      ],
      chinese: [['输入总 Token', '14'], ['输入音频 Token', '14'], ['输入文本 Token', '0'], ['输出总 Token', '45']],
      english: [['Total input tokens', '14'], ['Input audio tokens', '14'], ['Input text tokens', '0'], ['Total output tokens', '45']],
    },
  ])('shows supplier-reported $sample usage for a project viewer in both languages', async ({ units, chinese, english }) => {
    useAuthStore.setState({ user: { id: 2, username: 'project-reader', role: 'member', org_id: 1 } });
    mocks.getProjects.mockResolvedValue({ data: [
      { id: 7, org_id: 1, name: 'Own project', status: 'active', current_user_membership_role: 'project_viewer' },
    ] });
    mocks.getMediaJobs.mockResolvedValue({ data: [{ ...JOB, charge_fact: { ...JOB.charge_fact, units } }] });
    render(<><LanguageSwitcher /><MediaJobsPage /></>);

    const billing = within(await screen.findByRole('region', { name: '计费事实' }));
    for (const [label, value] of chinese) {
      expect(billing.getByText(label).parentElement?.querySelector('strong')?.textContent).toBe(value);
    }
    expect(billing.getAllByText('计量来源：供应商报告')).toHaveLength(units.length);
    expect(billing.getByText('待媒体价目表')).toBeTruthy();
    expect(billing.queryByText(/未识别的计量项|输出图像 Token|^(?:59|94)$|¥|CNY/)).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'English' }));
    const localized = within(screen.getByRole('region', { name: 'Charge facts' }));
    for (const [label, value] of english) {
      expect(localized.getByText(label).parentElement?.querySelector('strong')?.textContent).toBe(value);
    }
    expect(localized.getAllByText('Metering source: Provider reported')).toHaveLength(units.length);
    expect(localized.getByText('Awaiting media pricing')).toBeTruthy();
    expect(localized.queryByText(/Unrecognized usage metric|Output image tokens|^(?:59|94)$|¥|CNY/)).toBeNull();
    expect(mocks.getMediaJobs).toHaveBeenCalledExactlyOnceWith(7, 1);
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
  });

  it.each([
    { label: 'empty', units: [] },
    { label: 'null', units: null },
    { label: 'missing', units: undefined },
  ])('shows missing usage without substituting zero for $label units', async ({ units }) => {
    setLocale('en-US');
    mocks.getMediaJobs.mockResolvedValue({ data: [{ ...JOB, charge_fact: { ...JOB.charge_fact, units } }] });
    render(<MediaJobsPage />);
    const billing = within(await screen.findByRole('region', { name: 'Charge facts' }));
    expect(billing.getByText('Usage not recorded')).toBeTruthy();
    expect(billing.getByText('Awaiting media pricing')).toBeTruthy();
    expect(billing.queryByText('0', { exact: true })).toBeNull();
    expect(billing.queryByText(/¥|CNY|Provider reported/)).toBeNull();
  });

  it('preserves int64 precision and leaves unavailable quantities or unsupported pricing unconfirmed', async () => {
    mocks.getMediaJobs.mockResolvedValue({ data: [{
      ...JOB,
      charge_fact: {
        ...JOB.charge_fact,
        amount_state: 'charged',
        amount_micros: 0,
        units: [
          providerUnit('input_audio_milliseconds', '26325'),
          providerUnit('recognized_pages', '9007199254740993'),
          providerUnit('unrecognized_future_metric', '3'),
          providerUnit('output_images', undefined),
          providerUnit('input_tokens', '01'),
        ],
      },
    }] });
    render(<MediaJobsPage />);
    const billing = within(await screen.findByRole('region', { name: '计费事实' }));
    expect(billing.getByText('26,325 毫秒')).toBeTruthy();
    expect(billing.getByText('9,007,199,254,740,993')).toBeTruthy();
    expect(billing.getByText('未识别的计量项').parentElement).toHaveTextContent('3');
    expect(billing.getAllByText('用量未记录')).toHaveLength(2);
    expect(billing.getByText('计价状态未确认')).toBeTruthy();
    expect(billing.queryByText(/¥|CNY|charged|待媒体价目表/)).toBeNull();
  });

  it('does not infer a charge fact for jobs that lack one', async () => {
    mocks.getMediaJobs.mockResolvedValue({ data: [{ ...JOB, charge_fact: undefined }] });
    render(<MediaJobsPage />);
    const billing = within(await screen.findByRole('region', { name: '计费事实' }));
    expect(billing.getByText('暂无计费事实')).toBeTruthy();
    expect(billing.queryByText(/待媒体价目表|供应商报告/)).toBeNull();
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

  it('loads a legacy image on explicit action and localizes its job type', async () => {
    const pixel = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jFPcAAAAASUVORK5CYII=', 'base64');
    setOutput(new NodeBlob([pixel], { type: 'image/png' }));

    render(<><LanguageSwitcher /><MediaJobsPage /></>);
    const button = await screen.findByRole('button', { name: '鉴权查看输出' });
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
    expect(screen.getByText('查看当前项目的异步任务、执行状态和已保留的输出。')).toBeTruthy();
    expect(screen.getByText('文生图', { exact: true })).toBeTruthy();

    fireEvent.click(button);
    await waitFor(() => {
      expect(mocks.getMediaJobContent).toHaveBeenCalledWith(JOB.job_id, 7, 1);
    });
    expect(await screen.findByAltText(`任务 ${JOB.job_id} 的图像输出`)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'English' }));
    expect(screen.getByText('Review asynchronous jobs, execution status, and retained outputs for this project.')).toBeTruthy();
    expect(screen.getByText('Text to image', { exact: true })).toBeTruthy();
    expect(screen.getByAltText(`Image output for job ${JOB.job_id}`)).toBeTruthy();
    expect(screen.queryByText(/Image V1|异步任务运行时未启用|runtime is disabled/)).toBeNull();
    expect(mocks.getMediaJobContent).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['video/mp4', 'video', '视频'],
    ['audio/mpeg', 'audio', '音频'],
    ['audio/wav', 'audio', '音频'],
  ])('uses native playback controls for %s after the explicit read', async (type, tag, label) => {
    setOutput(new NodeBlob(['synthetic output bytes'], { type }));
    render(<MediaJobsPage />);
    const button = await screen.findByRole('button', { name: '鉴权查看输出' });
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
    fireEvent.click(button);
    const player = await screen.findByLabelText(`任务 ${JOB.job_id} 的${label}输出`);
    expect(player.tagName.toLowerCase()).toBe(tag);
    expect(player).toHaveAttribute('controls');
    expect(player).toHaveAttribute('src', 'blob:media-output');
    expect(player).not.toHaveAttribute('autoplay');
  });

  it('renders a retained transcript and its actual job type without implying runtime availability', async () => {
    const text = '合成转写 <a href="https://example.test/">not a link</a>\nSecond line';
    setOutput(new NodeBlob([text], { type: 'text/plain; charset=utf-8' }), 'text/plain', 'speech_to_text');
    mocks.getMediaJobContract.mockResolvedValueOnce({ runtime_enabled: false, admission_enabled: false });
    setLocale('en-US');
    render(<><LanguageSwitcher /><MediaJobsPage /></>);
    expect(await screen.findByText('The asynchronous job runtime is disabled on this deployment.')).toBeTruthy();
    expect(screen.getByText('Job type')).toBeTruthy();
    expect(screen.getByText('Speech to text', { exact: true })).toBeTruthy();
    expect(screen.queryByText(/Image V1|New admissions are paused/)).toBeNull();
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole('button', { name: 'View authorized output' }));
    expect((await screen.findByLabelText(`Transcript for job ${JOB.job_id}`)).textContent).toBe(text);
    expect(screen.queryByRole('link', { name: 'not a link' })).toBeNull();
    expect(createObjectURL).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '中文' }));
    expect(screen.getByLabelText(`任务 ${JOB.job_id} 的转写文本`).textContent).toBe(text);
    expect(screen.getByText('当前部署未启用异步任务运行时。')).toBeTruthy();
    expect(screen.getByText('任务类型')).toBeTruthy();
    expect(screen.getByText('语音转写', { exact: true })).toBeTruthy();
    expect(mocks.getMediaJobContent).toHaveBeenCalledExactlyOnceWith(JOB.job_id, 7, 1);
  });

  it.each([
    ['text_to_image', '文生图', 'Text to image'],
    ['text_to_video', '文生视频', 'Text to video'],
    ['image_to_text', '图片文字识别（OCR）', 'Image to text (OCR)'],
    ['speech_to_text', '语音转写', 'Speech to text'],
    ['text_to_speech', '语音合成', 'Text to speech'],
    ['future_media_operation', 'future_media_operation', 'future_media_operation'],
  ])('shows the recorded operation %s in both languages for a project viewer', async (operation, chinese, english) => {
    useAuthStore.setState({ user: { id: 2, username: 'project-reader', role: 'member', org_id: 1 } });
    mocks.getProjects.mockResolvedValue({
      data: [{ id: 7, org_id: 1, name: 'Own project', status: 'active', current_user_membership_role: 'project_viewer' }],
    });
    mocks.getMediaJobs.mockResolvedValue({ data: [{ ...JOB, operation }] });
    render(<><LanguageSwitcher /><MediaJobsPage /></>);

    const inspector = within(screen.getByLabelText('异步模型任务检查器'));
    expect(await inspector.findByText(chinese, { exact: true })).toBeInTheDocument();
    const reads = mocks.getMediaJobs.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'English' }));
    expect(within(screen.getByLabelText('Model job inspector')).getByText(english, { exact: true })).toBeInTheDocument();
    expect(mocks.getMediaJobs).toHaveBeenCalledTimes(reads);
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
  });

  it.each([
    ['image/svg+xml', 32],
    ['text/plain; charset=utf-16', 32],
    ['text/plain', 1024 * 1024 + 1],
    ['video/mp4', 32 * 1024 * 1024 + 1],
  ])('does not request an unsupported output %s with %d bytes', async (media_type, size_bytes) => {
    mocks.getMediaJobs.mockResolvedValue({ data: [{ ...JOB, output: { ...JOB.output, media_type, size_bytes } }] });
    render(<MediaJobsPage />);
    expect(await screen.findByText('暂不支持预览此输出类型或大小。')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '鉴权查看输出' })).toBeNull();
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
  });

  it.each([
    ['text/html', 'legacy'],
    ['image/png', 'wrong size'],
  ])('rejects a response that differs from the authorized metadata (%s)', async (type, body) => {
    setOutput(new NodeBlob(['legacy'], { type: 'image/png' }));
    mocks.getMediaJobContent.mockResolvedValue(new NodeBlob([body], { type }));
    render(<MediaJobsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '鉴权查看输出' }));
    expect(await screen.findByText('输出内容与任务记录不符，无法预览。')).toBeTruthy();
    expect(createObjectURL).not.toHaveBeenCalled();
  });

  it.each([403, 404, 410])('shows the %d denial without exposing response details or an earlier preview', async (status) => {
    setOutput(new NodeBlob(['audio'], { type: 'audio/wav' }));
    render(<MediaJobsPage />);
    const button = await screen.findByRole('button', { name: '鉴权查看输出' });
    fireEvent.click(button);
    await screen.findByLabelText(`任务 ${JOB.job_id} 的音频输出`);
    mocks.getMediaJobContent.mockRejectedValueOnce(new ApiError('https://example.test/private-output', status, null));
    fireEvent.click(screen.getByRole('button', { name: '鉴权查看输出' }));
    expect(await screen.findByText(/输出读取失败/)).toBeTruthy();
    expect(screen.queryByLabelText(`任务 ${JOB.job_id} 的音频输出`)).toBeNull();
    expect(document.body.textContent).not.toContain('https://example.test/private-output');
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:media-output');
  });

  it('filters a list result from another project or organization before showing a preview action', async () => {
    mocks.getMediaJobs.mockResolvedValue({ data: [
      { ...JOB, scope: { org_id: 1, project_id: 8 } },
      { ...JOB, scope: { org_id: 2, project_id: 7 } },
    ] });
    render(<MediaJobsPage />);
    expect(await screen.findByRole('heading', { name: '暂无异步模型任务' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: '鉴权查看输出' })).toBeNull();
    expect(mocks.getMediaJobContent).not.toHaveBeenCalled();
  });

  it('discards content that finishes after changing the authorized project', async () => {
    const result = deferred<NodeBlob>();
    mocks.getProjects.mockResolvedValue({ data: [
      { id: 7, org_id: 1, name: 'First project', status: 'active' },
      { id: 8, org_id: 1, name: 'Second project', status: 'active' },
    ] });
    mocks.getMediaJobs.mockImplementation((id: number) => Promise.resolve({ data: id === 7 ? [JOB] : [] }));
    mocks.getMediaJobContent.mockReturnValueOnce(result.promise);
    render(<MediaJobsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '鉴权查看输出' }));
    fireEvent.click(screen.getByRole('combobox', { name: '项目' }));
    fireEvent.click(await screen.findByText('Second project'));
    await waitFor(() => expect(mocks.getMediaJobs).toHaveBeenCalledWith(8, 1));
    await act(async () => result.resolve(new NodeBlob([new Uint8Array(1024)], { type: 'image/png' })));
    expect(screen.queryByRole('button', { name: '鉴权查看输出' })).toBeNull();
    expect(createObjectURL).not.toHaveBeenCalled();
  });

  it('discards a late response once its output has expired', async () => {
    const result = deferred<NodeBlob>();
    const expiresAt = Date.now() + 60_000;
    mocks.getMediaJobs.mockResolvedValue({ data: [{ ...JOB, output: { ...JOB.output, expires_at: new Date(expiresAt).toISOString() } }] });
    mocks.getMediaJobContent.mockReturnValueOnce(result.promise);
    render(<MediaJobsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '鉴权查看输出' }));
    vi.spyOn(Date, 'now').mockReturnValue(expiresAt);
    await act(async () => result.resolve(new NodeBlob([new Uint8Array(1024)], { type: 'image/png' })));
    expect(createObjectURL).not.toHaveBeenCalled();
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
    expect(await screen.findByText('当前部署未启用异步任务运行时。')).toBeTruthy();
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
    expect(screen.getByText('Awaiting media pricing')).toBeTruthy();
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
