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
import { accessReasonLabel, accessScopeLabel } from '../utils/accessDisplay';
import { IntentRequestIDs } from '../utils/intentRequestId';
import { loadAllPages } from '../utils/loadAllPages';
import { projectDisplayName, projectOption } from '../utils/projectDisplay';

const { Text } = Typography;

type Project = { id: number; org_id: number; name: string; slug?: string; status: string; current_user_membership_role?: string };

function accessSubjectLabel(request: AccessRequest) {
  const displayName = request.subject_display_name?.trim();
  if (displayName) return displayName;
  return request.subject.type === 'user'
    ? `用户 #${request.subject.user_id}`
    : `应用 #${request.subject.workload_application_id}`;
}

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
	const [queueProjectID, setQueueProjectID] = useState<number>();
	const [mutation, setMutation] = useState<{ projectID: number; requestRef: string } | null>(null);
	const currentProjectID = useRef<number | undefined>(selectedProjectID);
	const queueGeneration = useRef(0);
	currentProjectID.current = selectedProjectID;
	const queueIsCurrent = !!selectedProjectID && queueProjectID === selectedProjectID;
	const visibleRequests = queueIsCurrent ? requests : [];
	const visibleCursor = queueIsCurrent ? cursor : undefined;
	const visibleLoading = queueIsCurrent ? loading : !!selectedProjectID;
	const visibleError = queueIsCurrent ? error : '';
	const mutatingRef = mutation && mutation.projectID === selectedProjectID ? mutation.requestRef : '';

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
	const projectID = selectedProjectID;
	if (currentProjectID.current !== projectID) return;
	const generation = ++queueGeneration.current;
	if (!projectID) {
	  setQueueProjectID(undefined);
      setRequests([]);
      setCursor(undefined);
	  setLoading(false);
      return;
    }
	setQueueProjectID(projectID);
	if (!append) {
		setRequests([]);
		setCursor(undefined);
	}
    setLoading(true);
    setError('');
    try {
		const page = await accessApi.listProjectRequests(projectID, {
        status: 'needs_review', limit: 25, after_id: append ? cursor : undefined,
      });
		if (currentProjectID.current !== projectID || queueGeneration.current !== generation) return;
      setRequests((current) => append ? [...current, ...page.data] : page.data);
      setCursor(page.pagination.has_more ? page.pagination.next_after_id : undefined);
    } catch {
		if (currentProjectID.current !== projectID || queueGeneration.current !== generation) return;
      if (!append) setRequests([]);
      setError('无法读取此项目的审核队列。');
    } finally {
		if (currentProjectID.current === projectID && queueGeneration.current === generation) setLoading(false);
    }
  }, [cursor, selectedProjectID]);

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadQueue(); }, [selectedProjectID]);

	const decide = useCallback((request: AccessRequest, decision: 'approve' | 'reject') => {
		const projectID = currentProjectID.current;
		if (!projectID || request.project_id !== projectID) {
			Toast.warning('审核范围已切换；未提交旧项目请求。');
			return;
		}
    const verb = decision === 'approve' ? '批准' : '拒绝';
    Modal.confirm({
      title: `${verb}访问请求`,
      content: `确认${verb}${accessSubjectLabel(request)}的访问申请？系统会在保存前再次核对你的权限、项目状态和申请范围。`,
      onOk: async () => {
		if (currentProjectID.current !== projectID || request.project_id !== projectID) {
			Toast.warning('审核范围已切换；未提交旧项目请求。');
			return;
		}
        const intent = `decision:${request.request_ref}:${request.revision}:${decision}`;
		setMutation({ projectID, requestRef: request.request_ref });
        try {
          await accessApi.decideRequest(request.request_ref, {
            expected_revision: request.revision, decision,
            reason_codes: [decision === 'approve' ? 'manual_approved' : 'manual_rejected'],
          }, intents.get(intent));
          intents.complete(intent);
		  if (currentProjectID.current !== projectID) return;
          Toast.success(`请求已${verb}`);
		  await loadQueue();
        } catch (decisionError) {
          intents.settleError(intent, decisionError);
		  if (currentProjectID.current !== projectID) return;
          if (decisionError instanceof ApiError && decisionError.status === 409) {
            Toast.warning('请求已被其他操作更新，已刷新审核队列。');
            await loadQueue();
          } else {
            Toast.error(`无法${verb}此请求`);
          }
        } finally {
		  setMutation((current) => current?.projectID === projectID && current.requestRef === request.request_ref ? null : current);
        }
      },
    });
	}, [intents, loadQueue]);

  const columns = useMemo(() => [
    { title: '申请对象', render: (_: unknown, row: AccessRequest) => <><Text strong>{accessSubjectLabel(row)}</Text><br /><Text type="tertiary">{row.subject.type === 'user' ? '人员账号' : '应用或 Agent'}</Text><details><summary>高级审计标识</summary><Text code>{row.request_ref}</Text></details></> },
    { title: '申请范围', render: (_: unknown, row: AccessRequest) => <Space vertical align="start"><Text>{accessScopeLabel(row.requested_scope.project_role, row.requested_scope.scope_mode)}</Text>{row.production_use && <Tag color="red">生产用途</Tag>}</Space> },
    { title: '需要审核的原因', render: (_: unknown, row: AccessRequest) => <Space wrap>{row.policy_reason_codes.map((reason) => <Tag key={reason}>{accessReasonLabel(reason)}</Tag>)}</Space> },
    { title: '状态', render: () => <StatusTag status="needs_review">待审核</StatusTag> },
    { title: '提交时间', dataIndex: 'submitted_at', render: (value: string) => formatDateTime(value) },
    {
      title: '实时操作', width: 180,
      render: (_: unknown, row: AccessRequest) => row.actions?.can_approve || row.actions?.can_reject ? (
        <Space>
          <Button size="small" type="primary" loading={mutatingRef === row.request_ref} disabled={!row.actions.can_approve} onClick={() => decide(row, 'approve')}>批准</Button>
          <Button size="small" type="danger" loading={mutatingRef === row.request_ref} disabled={!row.actions.can_reject} onClick={() => decide(row, 'reject')}>拒绝</Button>
        </Space>
      ) : <Text type="tertiary">{row.actions?.reason_code ? accessReasonLabel(row.actions.reason_code) : '当前账号不可处理'}</Text>,
    },
	], [decide, mutatingRef]);

  return (
    <div className="ur-page">
      <PageHeader eyebrow="访问与安全" title="访问审核" description="查看需要人工确认的人员和应用访问申请。批准或拒绝前，系统会再次核对项目、主体和你的当前权限。" actions={<Button icon={<IconRefresh />} onClick={() => loadQueue()}>刷新</Button>} />
      <Surface title="审核项目" description="组织管理员可审核当前客户项目；项目管理员只会看到自己负责的项目。">
        {projectsError ? <EmptyState title="项目范围暂时不可用" description={projectsError} actions={<Button onClick={loadProjects}>重试</Button>} /> : (
          projectsLoading ? <Text type="tertiary">正在读取审核范围…</Text> : projects.length === 1
            ? <Text strong>{projectDisplayName(projects[0])}</Text>
            : <Select value={selectedProjectID} style={{ width: 320 }} placeholder="选择审核项目" optionList={projects.map(projectOption)} onChange={(value) => replaceProject(Number(value))} />
        )}
      </Surface>
      <Surface title="待审核申请" description="这里只显示真正需要你处理的申请；已处理或无权处理的项目不会混入队列。">
        {projectsLoading ? <EmptyState title="正在读取待审核范围" description="正在确认当前账号可审核的项目，请稍候。" />
          : !selectedProjectID ? <EmptyState title="暂无可审核项目" description="当前账号没有可发现的实时项目管理员范围。" />
            : visibleError ? <EmptyState title="审核队列暂时不可用" description={visibleError} actions={<Button onClick={() => loadQueue()}>重试</Button>} />
              : visibleLoading && visibleRequests.length === 0 ? <EmptyState title="正在读取待审核申请" description="正在读取当前项目的实时审核队列，请稍候。" /> : (
                <><Table columns={columns} dataSource={visibleRequests} rowKey="id" pagination={false} loading={visibleLoading} empty={<EmptyState title="当前项目没有待审核请求" />} />{visibleCursor && <Button block loading={visibleLoading} onClick={() => loadQueue(true)}>加载更多待审核请求</Button>}</>
        )}
      </Surface>
    </div>
  );
}
