import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Descriptions,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { api, ApiError } from '../api/client';
import { normalizeRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, PageHeader } from '../ui';

const { Text, Title } = Typography;

interface Project {
  id: number;
  org_id: number;
  name: string;
  slug?: string;
  status: string;
}

interface Lifecycle {
  admission: string;
  queue: string;
  lease: string;
  execution: string;
  provider_outcome: string;
  cancellation: string;
  object: string;
  settlement: string;
}

interface UnitObservation {
  class: string;
  quantity: string;
  authority: string;
  finality: string;
  charge_state: string;
}

interface MediaJob {
  contract_version: string;
  job_id: string;
  scope: { org_id: number; project_id: number };
  request_id: string;
  client_request_id?: string;
  operation: string;
  profile_id: string;
  requested_model: string;
  routing_model: string;
  mapped_model?: string;
  request_sha256: string;
  lifecycle: Lifecycle;
  attempt: number;
  max_attempts: number;
  lease_fence: number;
  lease_expires_at?: string;
  next_attempt_at: string;
  output?: {
    content_sha256: string;
    media_type: string;
    size_bytes: number;
    persisted_at?: string;
    expires_at?: string;
    purged_at?: string;
  };
  charge_fact?: {
    fact_id: string;
    trigger: string;
    amount_state: string;
    amount_micros?: number;
    units: UnitObservation[];
    recorded_at: string;
  };
  error_code?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  terminal_at?: string;
}

interface RuntimeContract {
  contract_version: string;
  profile_id: string;
  retention: string;
  video_enabled: boolean;
  runtime_enabled: boolean;
  admission_enabled: boolean;
  charge_trigger: string;
  amount_state: string;
}

function responseData<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value as T[];
  if (value && typeof value === 'object') {
    const data = (value as { data?: unknown }).data;
    if (Array.isArray(data)) return data as T[];
  }
  return [];
}

function displayTime(value?: string) {
  if (!value) return '—';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function lifecycleColor(value: string) {
  if (['succeeded', 'persisted', 'charge_fact_recorded'].includes(value)) return 'green';
  if (['failed', 'purged', 'not_created'].includes(value)) return 'red';
  if (['requested', 'queued', 'leased', 'running', 'purge_pending'].includes(value)) return 'orange';
  return 'grey';
}

function outputReadable(job: MediaJob, nowMilliseconds: number) {
  if (job.lifecycle.object !== 'persisted' || !job.output?.expires_at) return false;
  const expiresAt = Date.parse(job.output.expires_at);
  return Number.isFinite(expiresAt) && nowMilliseconds < expiresAt;
}

export default function MediaJobsPage() {
  const role = normalizeRole(useAuthStore((state) => state.user?.role));
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState<number>();
  const [contract, setContract] = useState<RuntimeContract | null>(null);
  const [jobs, setJobs] = useState<MediaJob[]>([]);
  const [selectedID, setSelectedID] = useState('');
  const [loading, setLoading] = useState(false);
  const [contentLoading, setContentLoading] = useState(false);
  const [outputURL, setOutputURL] = useState('');
  const [error, setError] = useState('');
  const [clock, setClock] = useState(() => Date.now());
  const selected = useMemo(
    () => jobs.find((job) => job.job_id === selectedID) || jobs[0] || null,
    [jobs, selectedID],
  );
  const selectedProject = projects.find((project) => project.id === projectID);
  const outputURLRef = useRef('');
  const canCancel = role !== 'viewer' && selected?.lifecycle.execution !== 'terminal';
  const selectedOutputReadable = Boolean(selected && outputReadable(selected, clock));
  const nextOutputExpiry = useMemo(() => {
    const futureExpiries = jobs
      .map((job) => Date.parse(job.output?.expires_at || ''))
      .filter((expiresAt) => Number.isFinite(expiresAt) && expiresAt > clock);
    return futureExpiries.length > 0 ? Math.min(...futureExpiries) : 0;
  }, [clock, jobs]);

  const clearOutput = useCallback(() => {
    if (outputURLRef.current) URL.revokeObjectURL(outputURLRef.current);
    outputURLRef.current = '';
    setOutputURL('');
  }, []);

  const loadJobs = useCallback(async (silent = false) => {
    if (!selectedProject) return;
    if (!silent) setLoading(true);
    try {
      const [nextContract, nextJobs] = await Promise.all([
        api.getMediaJobContract(selectedProject.id, selectedProject.org_id),
        api.getMediaJobs(selectedProject.id, selectedProject.org_id),
      ]);
      const data = responseData<MediaJob>(nextJobs);
      setContract(nextContract as RuntimeContract);
      setJobs(data);
      setClock(Date.now());
      setSelectedID((current) => (
        data.some((job) => job.job_id === current) ? current : data[0]?.job_id || ''
      ));
      setError('');
    } catch (loadError) {
      setError(loadError instanceof ApiError
        ? `任务读取失败（${loadError.status}）：${loadError.message}`
        : '暂时无法读取图像任务。');
      if (!silent) {
        setContract(null);
        setJobs([]);
        setSelectedID('');
      }
    } finally {
      if (!silent) setLoading(false);
    }
  }, [selectedProject]);

  useEffect(() => {
    let active = true;
    api.getProjects(1, 100, { status: 'active' })
      .then((value: unknown) => {
        if (!active) return;
        const next = responseData<Project>(value);
        setProjects(next);
        const queryProject = Number(new URLSearchParams(window.location.search).get('project_id'));
        setProjectID(next.some((project) => project.id === queryProject)
          ? queryProject
          : next[0]?.id);
      })
      .catch((loadError: unknown) => {
        if (!active) return;
        setError(loadError instanceof ApiError
          ? `项目读取失败（${loadError.status}）：${loadError.message}`
          : '暂时无法读取项目。');
      });
    return () => {
      active = false;
      clearOutput();
    };
  }, [clearOutput]);

  useEffect(() => {
    clearOutput();
    loadJobs();
  }, [clearOutput, loadJobs]);

  useEffect(() => {
    if (!nextOutputExpiry) return undefined;
    const delay = Math.min(
      Math.max(0, nextOutputExpiry - Date.now()) + 25,
      2_147_000_000,
    );
    const timer = window.setTimeout(
      () => setClock(Date.now()),
      delay,
    );
    return () => window.clearTimeout(timer);
  }, [clock, nextOutputExpiry]);

  useEffect(() => {
    if (!selectedOutputReadable) clearOutput();
  }, [clearOutput, selected?.job_id, selectedOutputReadable]);

  useEffect(() => {
    if (!selectedProject || !jobs.some((job) => job.lifecycle.execution !== 'terminal')) return undefined;
    const timer = window.setInterval(() => loadJobs(true), 4000);
    return () => window.clearInterval(timer);
  }, [jobs, loadJobs, selectedProject]);

  const cancelSelected = async () => {
    if (!selected || !selectedProject || !canCancel) return;
    setLoading(true);
    try {
      await api.cancelMediaJob(selected.job_id, selectedProject.id, selectedProject.org_id);
      await loadJobs(true);
    } catch (cancelError) {
      setError(cancelError instanceof ApiError
        ? `取消失败（${cancelError.status}）：${cancelError.message}`
        : '暂时无法请求取消任务。');
    } finally {
      setLoading(false);
    }
  };

  const loadOutput = async () => {
    if (!selected?.output || !selectedProject || !selectedOutputReadable) return;
    setContentLoading(true);
    clearOutput();
    try {
      const blob = await api.getMediaJobContent(
        selected.job_id,
        selectedProject.id,
        selectedProject.org_id,
      );
      const nextURL = URL.createObjectURL(blob as Blob);
      outputURLRef.current = nextURL;
      setOutputURL(nextURL);
    } catch (contentError) {
      setError(contentError instanceof ApiError
        ? `输出读取失败（${contentError.status}）：${contentError.message}`
        : '暂时无法读取图像输出。');
    } finally {
      setContentLoading(false);
    }
  };

  const columns = [
    {
      key: 'job',
      title: '任务',
      dataIndex: 'job_id',
      render: (value: string, record: MediaJob) => (
        <button
          type="button"
          className={`ur-media-job-link${record.job_id === selected?.job_id ? ' is-active' : ''}`}
          onClick={() => {
            clearOutput();
            setSelectedID(record.job_id);
          }}
        >
          <strong>{value.slice(0, 18)}…</strong>
          <span>{record.requested_model}</span>
        </button>
      ),
    },
    {
      key: 'execution',
      title: '执行',
      dataIndex: 'lifecycle',
      width: 126,
      render: (value: Lifecycle) => <Tag color={lifecycleColor(value.execution)}>{value.execution}</Tag>,
    },
    {
      key: 'outcome',
      title: '结果',
      dataIndex: 'lifecycle',
      width: 126,
      render: (value: Lifecycle) => <Tag color={lifecycleColor(value.provider_outcome)}>{value.provider_outcome}</Tag>,
    },
    {
      key: 'created_at',
      title: '创建时间',
      dataIndex: 'created_at',
      width: 170,
      render: displayTime,
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="异步执行控制面"
        title="图像任务"
        description="GPT Image V1 的租户隔离任务、持久化输出、取消与结算事实。右侧检查器始终可见。"
        actions={(
          <Space>
            <Select
              aria-label="选择项目"
              value={projectID}
              placeholder="选择项目"
              style={{ width: 220 }}
              optionList={projects.map((project) => ({
                value: project.id,
                label: `${project.name} · ${project.id}`,
              }))}
              onChange={(value) => setProjectID(Number(value))}
            />
            <Button onClick={() => loadJobs()} loading={loading}>刷新</Button>
          </Space>
        )}
      />

      {error && <Banner type="danger" description={error} closeIcon={null} />}
      {contract && !contract.runtime_enabled && (
        <Banner
          type="warning"
          description="当前部署未启用图像任务 worker 与对象存储；仅任务元数据和合同证据可读取。"
          closeIcon={null}
        />
      )}
      {contract?.runtime_enabled && !contract.admission_enabled && (
        <Banner
          type="warning"
          description="新任务受理已暂停；现有任务的恢复、到期读取关闭与对象清理仍继续。"
          closeIcon={null}
        />
      )}

      <div className="ur-kpi-grid ur-media-kpis">
        <MetricTile label="本页任务" value={String(jobs.length)} meta="当前项目最近 50 条" />
        <MetricTile
          label="进行中"
          value={String(jobs.filter((job) => job.lifecycle.execution !== 'terminal').length)}
          meta="队列、租约与运行状态独立追踪"
        />
        <MetricTile
          label="输出可用"
          value={String(jobs.filter((job) => outputReadable(job, clock)).length)}
          meta="到期后读取立即关闭"
        />
        <MetricTile label="固定保留" value={contract?.retention || 'P7D'} meta="七天后读取关闭并进入删除" />
      </div>

      <div className="ur-context-explorer ur-media-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Title heading={5}>任务队列</Title>
              <Text type="tertiary">只展示当前项目范围；Prompt、凭据和对象定位符不会返回。</Text>
            </div>
          </div>
          {projects.length === 0 ? (
            <EmptyState title="没有可见项目" description="当前账号没有可读取的项目范围。" />
          ) : jobs.length === 0 && !loading ? (
            <EmptyState title="暂无图像任务" description="通过项目 API Key 提交任务后，状态会在这里出现。" />
          ) : (
            <Table
              rowKey="job_id"
              columns={columns}
              dataSource={jobs}
              loading={loading}
              pagination={false}
              size="small"
            />
          )}
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="图像任务检查器">
          <div className="ur-panel-title">
            <div>
              <Title heading={5}>任务检查器</Title>
              <Text type="tertiary">状态轴、输出保留与计费事实</Text>
            </div>
            {selected && (
              <Button
                type="danger"
                theme="light"
                disabled={!canCancel}
                loading={loading}
                onClick={cancelSelected}
              >
                请求取消
              </Button>
            )}
          </div>
          {!selected ? (
            <EmptyState title="等待任务" description="检查器会常驻；有任务后自动选择最新一条。" />
          ) : (
            <>
              <Descriptions
                align="left"
                size="small"
                data={[
                  { key: '任务 ID', value: selected.job_id },
                  { key: '请求 ID', value: selected.request_id },
                  { key: '请求模型', value: selected.requested_model },
                  { key: '路由模型', value: selected.routing_model },
                  { key: '供应商模型', value: selected.mapped_model || '—' },
                  { key: '尝试', value: `${selected.attempt} / ${selected.max_attempts}` },
                  { key: '请求摘要', value: selected.request_sha256 },
                  { key: '终态时间', value: displayTime(selected.terminal_at) },
                ]}
              />
              <div className="ur-media-state-grid">
                {Object.entries(selected.lifecycle).map(([axis, value]) => (
                  <div key={axis}>
                    <span>{axis.replace('provider_outcome', 'outcome')}</span>
                    <Tag color={lifecycleColor(value)}>{value}</Tag>
                  </div>
                ))}
              </div>

              <section className="ur-media-detail-section">
                <Title heading={6}>输出</Title>
                {selected.output ? (
                  <>
                    <Descriptions
                      align="left"
                      size="small"
                      data={[
                        { key: '类型', value: selected.output.media_type },
                        { key: '大小', value: `${selected.output.size_bytes.toLocaleString()} bytes` },
                        { key: '内容摘要', value: selected.output.content_sha256 },
                        { key: '持久化', value: displayTime(selected.output.persisted_at) },
                        { key: '到期', value: displayTime(selected.output.expires_at) },
                        { key: '清除', value: displayTime(selected.output.purged_at) },
                      ]}
                    />
                    {selectedOutputReadable && (
                      <Button loading={contentLoading} onClick={loadOutput}>鉴权查看输出</Button>
                    )}
                    {outputURL && (
                      <img className="ur-media-output" src={outputURL} alt={`任务 ${selected.job_id} 的图像输出`} />
                    )}
                  </>
                ) : <Text type="tertiary">尚无持久化输出。</Text>}
              </section>

              <section className="ur-media-detail-section">
                <Title heading={6}>计费事实</Title>
                {selected.charge_fact ? (
                  <>
                    <Banner
                      type="info"
                      description="已记录收费资格事实，但当前媒体价格表尚未冻结，因此没有扣减钱包或推断金额。"
                      closeIcon={null}
                    />
                    <Descriptions
                      align="left"
                      size="small"
                      data={[
                        { key: '事实 ID', value: selected.charge_fact.fact_id },
                        { key: '金额状态', value: selected.charge_fact.amount_state },
                        { key: '记录时间', value: displayTime(selected.charge_fact.recorded_at) },
                      ]}
                    />
                    <div className="ur-media-units">
                      {selected.charge_fact.units.map((unit) => (
                        <div key={unit.class}>
                          <span>{unit.class}</span>
                          <strong>{unit.quantity}</strong>
                        </div>
                      ))}
                    </div>
                  </>
                ) : <Text type="tertiary">只有供应商终态成功且输出已持久化，才会记录事实。</Text>}
              </section>
            </>
          )}
        </aside>
      </div>
    </div>
  );
}
