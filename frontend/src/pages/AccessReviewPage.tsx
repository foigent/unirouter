import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Modal, Select, Space, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useSearchParams } from 'react-router-dom';
import { accessApi, type AccessRequest } from '../api/access';
import { ApiError, api } from '../api/client';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { canonicalProjectMembershipRole, isOrgAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { formatDateTime } from '../utils/formatDate';
import { IntentRequestIDs } from '../utils/intentRequestId';
import { loadAllPages } from '../utils/loadAllPages';

const { Text } = Typography;

type Project = { id: number; org_id: number; name: string; status: string; current_user_membership_role?: string };

export default function AccessReviewPage() {
  const user = useAuthStore((state) => state.user);
  const organizationRole = organizationRoleForUser(user);
  const intents = useRef(new IntentRequestIDs()).current;
  const [search, setSearch] = useSearchParams();
  const selectedProjectID = Number(search.get('project_id') || 0) || undefined;
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsLoading, setProjectsLoading] = useState(true);
  const [projectsError, setProjectsError] = useState('');
  const [requests, setRequests] = useState<AccessRequest[]>([]);
  const [cursor, setCursor] = useState<number>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [mutatingRef, setMutatingRef] = useState('');

  const replaceProject = useCallback((projectID?: number) => {
    const next = new URLSearchParams(search);
    if (projectID) next.set('project_id', String(projectID)); else next.delete('project_id');
    setSearch(next, { replace: true });
  }, [search, setSearch]);

  const loadProjects = useCallback(async () => {
    setProjectsLoading(true);
    setProjectsError('');
    try {
      const items = await loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active' }));
      const reviewable = isOrgAdminRole(organizationRole)
        ? items
        : items.filter((project) => canonicalProjectMembershipRole(project.current_user_membership_role) === 'project_admin');
      setProjects(reviewable);
      if (!selectedProjectID && reviewable[0]) replaceProject(reviewable[0].id);
    } catch {
      setProjects([]);
      setProjectsError('无法读取当前账号可审核的项目。');
    } finally {
      setProjectsLoading(false);
    }
  }, [organizationRole, replaceProject, selectedProjectID]);

  const loadQueue = useCallback(async (append = false) => {
    if (!selectedProjectID) {
      setRequests([]);
      setCursor(undefined);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const page = await accessApi.listProjectRequests(selectedProjectID, {
        status: 'needs_review', limit: 25, after_id: append ? cursor : undefined,
      });
      setRequests((current) => append ? [...current, ...page.data] : page.data);
      setCursor(page.pagination.has_more ? page.pagination.next_after_id : undefined);
    } catch {
      if (!append) setRequests([]);
      setError('无法读取此项目的审核队列。');
    } finally {
      setLoading(false);
    }
  }, [cursor, selectedProjectID]);

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadQueue(); }, [selectedProjectID]);

  const decide = (request: AccessRequest, decision: 'approve' | 'reject') => {
    const verb = decision === 'approve' ? '批准' : '拒绝';
    Modal.confirm({
      title: `${verb}访问请求`,
      content: `将按请求 revision ${request.revision} 提交${verb}，后端会再次校验当前角色、项目、主体与策略原因。`,
      onOk: async () => {
        const intent = `decision:${request.request_ref}:${request.revision}:${decision}`;
        setMutatingRef(request.request_ref);
        try {
          await accessApi.decideRequest(request.request_ref, {
            expected_revision: request.revision, decision,
            reason_codes: [decision === 'approve' ? 'manual_approved' : 'manual_rejected'],
          }, intents.get(intent));
          intents.complete(intent);
          Toast.success(`请求已${verb}`);
          await loadQueue();
        } catch (decisionError) {
          intents.settleError(intent, decisionError);
          if (decisionError instanceof ApiError && decisionError.status === 409) {
            Toast.warning('请求已被其他操作更新，已刷新审核队列。');
            await loadQueue();
          } else {
            Toast.error(`无法${verb}此请求`);
          }
        } finally {
          setMutatingRef('');
        }
      },
    });
  };

  const columns = useMemo(() => [
    { title: '请求', dataIndex: 'request_ref', render: (value: string) => <Text code>{value}</Text> },
    { title: '主体', render: (_: unknown, row: AccessRequest) => row.subject.type === 'user' ? `用户 #${row.subject.user_id}` : `Workload #${row.subject.workload_application_id}` },
    { title: '请求范围', render: (_: unknown, row: AccessRequest) => <Space vertical align="start"><Text>{row.requested_scope.project_role} · {row.requested_scope.scope_mode}</Text>{row.production_use && <Tag color="red">生产用途</Tag>}</Space> },
    { title: '策略原因', render: (_: unknown, row: AccessRequest) => <Space wrap>{row.policy_reason_codes.map((reason) => <Tag key={reason}>{reason}</Tag>)}</Space> },
    { title: '状态', render: () => <StatusTag status="needs_review">待审核</StatusTag> },
    { title: '提交时间', dataIndex: 'submitted_at', render: (value: string) => formatDateTime(value) },
    {
      title: '实时操作', width: 180,
      render: (_: unknown, row: AccessRequest) => row.actions?.can_approve || row.actions?.can_reject ? (
        <Space>
          <Button size="small" type="primary" loading={mutatingRef === row.request_ref} disabled={!row.actions.can_approve} onClick={() => decide(row, 'approve')}>批准</Button>
          <Button size="small" type="danger" loading={mutatingRef === row.request_ref} disabled={!row.actions.can_reject} onClick={() => decide(row, 'reject')}>拒绝</Button>
        </Space>
      ) : <Text type="tertiary">{row.actions?.reason_code || '当前角色不可操作'}</Text>,
    },
  ], [mutatingRef]);

  return (
    <div className="ur-page">
      <PageHeader eyebrow="访问与安全" title="访问审核" description="仅展示所选项目中服务端过滤后的 needs_review 队列；操作能力由同一决策规则投影，提交时仍执行 commit-time 复核。" actions={<Button icon={<IconRefresh />} onClick={() => loadQueue()}>刷新</Button>} />
      <Surface title="审核项目" description="平台/组织管理员按组织范围选择；项目管理员只看当前实时担任 project_admin 的项目。">
        {projectsError ? <EmptyState title="项目范围暂时不可用" description={projectsError} actions={<Button onClick={loadProjects}>重试</Button>} /> : (
          <Select loading={projectsLoading} value={selectedProjectID} style={{ width: 320 }} placeholder="选择审核项目" optionList={projects.map((project) => ({ value: project.id, label: `${project.name} (#${project.id})` }))} onChange={(value) => replaceProject(Number(value))} />
        )}
      </Surface>
      <Surface title="待审核请求" description="状态筛选在 PostgreSQL cursor limit 之前完成；非 actionable 项不会靠尝试写入探测权限。">
        {!selectedProjectID ? <EmptyState title="暂无可审核项目" description="当前账号没有可发现的实时项目管理员范围。" /> : error ? <EmptyState title="审核队列暂时不可用" description={error} actions={<Button onClick={() => loadQueue()}>重试</Button>} /> : (
          <><Table columns={columns} dataSource={requests} rowKey="id" pagination={false} loading={loading && requests.length === 0} empty={<EmptyState title="当前项目没有待审核请求" />} />{cursor && <Button block loading={loading} onClick={() => loadQueue(true)}>加载更多待审核请求</Button>}</>
        )}
      </Surface>
    </div>
  );
}
