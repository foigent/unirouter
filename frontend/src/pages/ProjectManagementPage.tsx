import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Form, Input, Modal, Select, SideSheet, Space, Table, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { ApiError, api } from '../api/client';
import { isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';

const { Text } = Typography;
const { TabPane } = Tabs;
const maxProjectBudgetYuan = 21474836.47;

interface Project {
  id: number;
  org_id: number;
  org_name?: string;
  name: string;
  slug: string;
  description?: string;
  status: string;
  budget_cents: number;
  budget_period: string;
  key_count?: number;
}

interface Organization {
  id: number;
  name: string;
}

interface ProjectMember {
  user_id: number;
  username: string;
  email?: string;
  auth_provider: string;
  user_role: string;
  status: string;
  membership_role: string;
}

interface EligibleUser {
  user_id: number;
  username: string;
  email?: string;
  user_role: string;
  membership_role: string;
  status: string;
}

interface ModelSet {
  id: number;
  name: string;
  slug: string;
  status: string;
}

const roleNames: Record<string, string> = {
  project_admin: '项目管理员',
  project_developer: '开发者',
  project_viewer: '只读成员',
};

const periodOptions = [
  { label: '不限制', value: 'none' },
  { label: '每日', value: 'daily' },
  { label: '每月', value: 'monthly' },
];

function yuanFromCents(cents: number) {
  return Number((Number(cents || 0) / 100).toFixed(2));
}

function centsFromYuan(yuan: unknown) {
  const value = Number(yuan || 0);
  return Number.isFinite(value) && value >= 0 && value <= maxProjectBudgetYuan ? Math.round(value * 100) : -1;
}

export default function ProjectManagementPage() {
  const currentUser = useAuthStore(state => state.user);
  const role = currentUser?.role || '';
  const platform = isPlatformAdminRole(role);
  const canCreateProject = platform || role === 'org_admin';
  const canManageMembers = platform || role === 'org_admin' || role === 'project_admin';
  const canEditProject = canManageMembers;
  const licenseModelGovernance = currentUser?.license_access?.features?.model_governance === true;

  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(currentUser?.org_id);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectTotal, setProjectTotal] = useState(0);
  const [projectPage, setProjectPage] = useState(1);
  const [projectPageSize, setProjectPageSize] = useState(20);
  const [projectLoading, setProjectLoading] = useState(false);
  const [selectedProject, setSelectedProject] = useState<Project | null>(null);
  const [activeTab, setActiveTab] = useState('overview');
  const [createSheet, setCreateSheet] = useState(false);
  const [createFormApi, setCreateFormApi] = useState<any>(null);
  const [overviewFormApi, setOverviewFormApi] = useState<any>(null);

  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [memberTotal, setMemberTotal] = useState(0);
  const [memberPage, setMemberPage] = useState(1);
  const [memberPageSize, setMemberPageSize] = useState(20);
  const [eligibleUsers, setEligibleUsers] = useState<EligibleUser[]>([]);
  const [selectedCandidateID, setSelectedCandidateID] = useState<number>();
  const [candidateQuery, setCandidateQuery] = useState('');
  const [membersLoading, setMembersLoading] = useState(false);

  const [modelSets, setModelSets] = useState<ModelSet[]>([]);
  const [assignedModelSetIDs, setAssignedModelSetIDs] = useState<number[]>([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelError, setModelError] = useState('');
  const detailRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!platform) return;
    loadAllPages<Organization>((page, size) => api.getOrganizations(page, size))
      .then(items => {
        setOrganizations(items);
        if (!selectedOrgID && items[0]) setSelectedOrgID(items[0].id);
      })
      .catch(() => setOrganizations([]));
  }, [platform, selectedOrgID]);

  const loadProjects = useCallback((page = projectPage, pageSize = projectPageSize) => {
    if (platform && !selectedOrgID) return Promise.resolve();
    setProjectLoading(true);
    return api.getProjects(page, pageSize, { org_id: platform ? selectedOrgID : undefined })
      .then((response: any) => {
        const nextProjects = response.data || [];
        setProjects(nextProjects);
        setProjectTotal(response.total || 0);
        if (selectedProject) {
          const refreshed = nextProjects.find((project: Project) => project.id === selectedProject.id);
          if (refreshed) setSelectedProject(refreshed);
        }
      })
      .catch(() => {
        setProjects([]);
        setProjectTotal(0);
        Toast.error('项目列表加载失败');
      })
      .finally(() => setProjectLoading(false));
  }, [platform, projectPage, projectPageSize, selectedOrgID, selectedProject]);

  useEffect(() => {
    setSelectedProject(null);
    setProjectPage(1);
    void loadProjects(1, projectPageSize);
  }, [selectedOrgID]);

  const loadMembers = useCallback((projectID: number, page = 1, pageSize = 20, query = '') => {
    setMembersLoading(true);
    return api.getProjectMembers(projectID, page, pageSize, query)
      .then((response: any) => {
        setMembers(response.data || []);
        setMemberTotal(response.total || 0);
        setEligibleUsers(response.eligible_users || []);
      })
      .catch(() => {
        setMembers([]);
        setEligibleUsers([]);
        Toast.error('项目成员加载失败');
      })
      .finally(() => setMembersLoading(false));
  }, []);

  const loadModels = useCallback(async (project: Project) => {
    setModelsLoading(true);
    setModelError('');
    try {
      const [sets, assigned] = await Promise.all([
        loadAllPages<ModelSet>((page, size) => api.getModelSets(page, size, { org_id: project.org_id })),
        api.getProjectModelSets(project.id),
      ]);
      setModelSets(sets.filter(item => item.status === 'active'));
      setAssignedModelSetIDs((assigned.data || assigned.model_set_ids || []).map((item: any) => Number(item.model_set_id ?? item.id ?? item)));
    } catch (error) {
      setModelSets([]);
      setAssignedModelSetIDs([]);
      setModelError(error instanceof ApiError && error.status === 403
        ? '当前角色或商业授权不允许维护项目模型范围。'
        : '模型范围加载失败，请稍后重试。');
    } finally {
      setModelsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!selectedProject) return;
    overviewFormApi?.setValues({
      name: selectedProject.name,
      slug: selectedProject.slug,
      description: selectedProject.description || '',
      status: selectedProject.status,
      budget_yuan: yuanFromCents(selectedProject.budget_cents),
      budget_period: selectedProject.budget_period,
    });
  }, [overviewFormApi, selectedProject]);

  useEffect(() => {
    if (!selectedProject) return;
    const timer = window.setTimeout(() => {
      detailRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'start' });
      detailRef.current?.focus({ preventScroll: true });
    }, 0);
    return () => window.clearTimeout(timer);
  }, [selectedProject]);

  useEffect(() => {
    if (!selectedProject) return;
    if (activeTab === 'members' && canManageMembers) void loadMembers(selectedProject.id, 1, memberPageSize);
    if (activeTab === 'models' && canManageMembers && licenseModelGovernance) void loadModels(selectedProject);
  }, [activeTab, canManageMembers, licenseModelGovernance, loadMembers, loadModels, memberPageSize, selectedProject]);

  const openProject = (project: Project) => {
    setSelectedProject(project);
    setActiveTab('overview');
    setMemberPage(1);
    setSelectedCandidateID(undefined);
    setCandidateQuery('');
  };

  const submitCreate = async (values: any) => {
    const budgetCents = centsFromYuan(values.budget_yuan);
    if (budgetCents < 0) {
      Toast.error(`预算金额需在 0 至 ${maxProjectBudgetYuan.toFixed(2)} 元之间`);
      return;
    }
    try {
      await api.createProject({
        org_id: Number(values.org_id),
        name: values.name,
        slug: values.slug,
        description: values.description || '',
        budget_cents: budgetCents,
        budget_period: values.budget_period || 'monthly',
      });
      Toast.success('项目已创建');
      setCreateSheet(false);
      await loadProjects(1, projectPageSize);
    } catch {
      Toast.error('项目创建失败');
    }
  };

  const submitOverview = async (values: any) => {
    if (!selectedProject) return;
    const budgetCents = centsFromYuan(values.budget_yuan);
    if (budgetCents < 0) {
      Toast.error(`预算金额需在 0 至 ${maxProjectBudgetYuan.toFixed(2)} 元之间`);
      return;
    }
    try {
      await api.updateProject(selectedProject.id, {
        name: values.name,
        slug: values.slug,
        description: values.description || '',
        status: values.status,
        budget_cents: budgetCents,
        budget_period: values.budget_period,
      });
      Toast.success('项目已更新');
      await loadProjects(projectPage, projectPageSize);
    } catch {
      Toast.error('项目更新失败');
    }
  };

  const addMember = async () => {
    if (!selectedProject || !selectedCandidateID) return;
    const candidate = eligibleUsers.find(item => item.user_id === selectedCandidateID);
    if (!candidate) return;
    try {
      await api.upsertProjectMember(selectedProject.id, candidate.user_id, candidate.membership_role);
      Toast.success('成员已加入项目');
      setSelectedCandidateID(undefined);
      await loadMembers(selectedProject.id, memberPage, memberPageSize, candidateQuery);
    } catch {
      Toast.error('添加成员失败');
    }
  };

  const removeMember = (member: ProjectMember) => {
    if (!selectedProject) return;
    Modal.confirm({
      title: '移除项目成员',
      content: `确认从 ${selectedProject.name} 移除 ${member.username}？启用中的项目角色必须保留至少一个项目。`,
      onOk: async () => {
        try {
          await api.deleteProjectMember(selectedProject.id, member.user_id);
          Toast.success('成员已移除');
          await loadMembers(selectedProject.id, memberPage, memberPageSize, candidateQuery);
        } catch (error) {
          Toast.error(error instanceof ApiError ? error.message : '移除成员失败');
        }
      },
    });
  };

  const saveModels = async () => {
    if (!selectedProject) return;
    try {
      await api.replaceProjectModelSets(selectedProject.id, assignedModelSetIDs);
      Toast.success('项目模型范围已更新');
    } catch {
      Toast.error('项目模型范围更新失败');
    }
  };

  const columns = [
    { title: '项目', dataIndex: 'name', render: (name: string, project: Project) => <Button theme="borderless" onClick={() => openProject(project)}>{name}</Button> },
    { title: '组织', dataIndex: 'org_name', render: (name: string, project: Project) => name || `组织 #${project.org_id}` },
    { title: '标识', dataIndex: 'slug' },
    { title: '状态', dataIndex: 'status', render: (status: string) => <StatusTag status={status}>{status === 'active' ? '启用' : status === 'disabled' ? '停用' : '归档'}</StatusTag> },
    { title: '预算', key: 'budget', render: (_: unknown, project: Project) => project.budget_period === 'none' || !project.budget_cents ? '不限制' : `¥${yuanFromCents(project.budget_cents).toFixed(2)} / ${project.budget_period === 'daily' ? '日' : '月'}` },
    { title: '凭证数', dataIndex: 'key_count', render: (count: number) => Number(count || 0) },
    { title: '操作', render: (_: unknown, project: Project) => <Button onClick={() => openProject(project)}>查看与管理</Button> },
  ];

  const memberColumns = [
    { title: '用户', dataIndex: 'username', render: (username: string, member: ProjectMember) => <span>{username}{member.email ? <Text type="tertiary"> · {member.email}</Text> : null}</span> },
    { title: '项目角色', dataIndex: 'membership_role', render: (value: string) => <Tag>{roleNames[value] || value}</Tag> },
    { title: '账号来源', dataIndex: 'auth_provider', render: (value: string) => value === 'oidc' ? '企业 OIDC' : '本地账号' },
    { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag status={value}>{value === 'active' ? '启用' : '停用'}</StatusTag> },
    {
      title: '操作',
      render: (_: unknown, member: ProjectMember) => (
        <Button
          type="danger"
          theme="borderless"
          icon={<IconDelete />}
          disabled={role === 'project_admin' && member.membership_role === 'project_admin'}
          onClick={() => removeMember(member)}
        >移除</Button>
      ),
    },
  ];

  const candidateOptions = useMemo(() => eligibleUsers.map(candidate => ({
    value: candidate.user_id,
    label: `${candidate.username} · ${roleNames[candidate.membership_role] || candidate.user_role}${candidate.status === 'active' ? '' : '（已停用）'}`,
  })), [eligibleUsers]);

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="访问与安全"
        title="项目与成员"
        description="选择项目后，可在同一工作区维护项目资料、预算、成员角色与可用模型范围。"
        actions={<Space>
          <Button icon={<IconRefresh />} onClick={() => loadProjects(projectPage, projectPageSize)}>刷新</Button>
          {canCreateProject && <Button type="primary" icon={<IconPlus />} onClick={() => setCreateSheet(true)}>新建项目</Button>}
        </Space>}
      />

      {platform && (
        <Surface>
          <Space>
            <Text strong>目标组织</Text>
            <Select
              value={selectedOrgID}
              optionList={organizations.map(org => ({ label: `${org.name} (#${org.id})`, value: org.id }))}
              onChange={value => setSelectedOrgID(Number(value))}
              style={{ width: 280 }}
            />
          </Space>
        </Surface>
      )}

      <Surface>
        <div className="ur-table-shell">
          <Table
            rowKey="id"
            columns={columns}
            dataSource={projects}
            loading={projectLoading}
            pagination={{
              currentPage: projectPage,
              pageSize: projectPageSize,
              total: projectTotal,
              showSizeChanger: true,
              pageSizeOpts: [20, 50, 100],
              onPageChange: page => { setProjectPage(page); void loadProjects(page, projectPageSize); },
              onPageSizeChange: size => { setProjectPage(1); setProjectPageSize(size); void loadProjects(1, size); },
            }}
          />
        </div>
      </Surface>

      {selectedProject ? (
        <div ref={detailRef} className="ur-project-detail" tabIndex={-1} aria-label={`项目详情：${selectedProject.name}`}>
        <Surface>
          <PageHeader
            eyebrow={`当前项目 · ${selectedProject.org_name || `组织 #${selectedProject.org_id}`}`}
            title={selectedProject.name}
            description={selectedProject.description || '暂无项目说明'}
          />
          <Tabs activeKey={activeTab} onChange={setActiveTab}>
            <TabPane tab="项目概况" itemKey="overview">
              <Form getFormApi={setOverviewFormApi} onSubmit={submitOverview} style={{ maxWidth: 720 }}>
                <Form.Input field="name" label="项目名称" disabled={!canEditProject} rules={[{ required: true, message: '必填' }]} />
                <Form.Input field="slug" label="项目标识" disabled={!canEditProject} rules={[{ required: true, message: '必填' }]} />
                <Form.TextArea field="description" label="项目说明" disabled={!canEditProject} />
                <Form.Select field="status" label="项目状态" disabled={!canEditProject} optionList={[
                  { label: '启用', value: 'active' }, { label: '停用', value: 'disabled' }, { label: '归档', value: 'archived' },
                ]} />
                <Form.InputNumber field="budget_yuan" label="预算金额（元）" min={0} max={maxProjectBudgetYuan} precision={2} extraText="0 表示不限制；结算和告警以所选预算周期为准。" style={{ width: '100%' }} />
                <Form.Select field="budget_period" label="预算周期" optionList={periodOptions} style={{ width: '100%' }} />
                <Button type="primary" htmlType="submit">保存项目</Button>
              </Form>
            </TabPane>

            {canManageMembers && (
              <TabPane tab="成员与角色" itemKey="members">
                <div className="ur-project-member-controls">
                  <Input
                    value={candidateQuery}
                    placeholder="按用户名或邮箱搜索候选人"
                    onChange={setCandidateQuery}
                    onEnterPress={() => loadMembers(selectedProject.id, 1, memberPageSize, candidateQuery)}
                    className="ur-project-member-search"
                  />
                  <Button onClick={() => loadMembers(selectedProject.id, 1, memberPageSize, candidateQuery)}>搜索</Button>
                  <Select
                    value={selectedCandidateID}
                    placeholder="选择同组织的项目角色用户"
                    optionList={candidateOptions}
                    onChange={value => setSelectedCandidateID(Number(value))}
                    filter
                    className="ur-project-member-select"
                  />
                  <Button type="primary" disabled={!selectedCandidateID} onClick={addMember}>加入项目</Button>
                </div>
                <div className="ur-table-shell">
                  <Table
                    rowKey="user_id"
                    columns={memberColumns}
                    dataSource={members}
                    loading={membersLoading}
                    pagination={{
                      currentPage: memberPage,
                      pageSize: memberPageSize,
                      total: memberTotal,
                      showSizeChanger: true,
                      pageSizeOpts: [20, 50, 100],
                      onPageChange: page => { setMemberPage(page); void loadMembers(selectedProject.id, page, memberPageSize, candidateQuery); },
                      onPageSizeChange: size => { setMemberPage(1); setMemberPageSize(size); void loadMembers(selectedProject.id, 1, size, candidateQuery); },
                    }}
                  />
                </div>
              </TabPane>
            )}

            {canManageMembers && (
              <TabPane tab="模型范围" itemKey="models">
                {!licenseModelGovernance ? (
                  <EmptyState title="当前授权未包含模型治理" description="项目其他功能仍可使用；平台管理员可在商业授权中更新能力。" />
                ) : modelError ? (
                  <Banner type="danger" description={modelError} />
                ) : (
                  <Space vertical align="start" spacing="medium" style={{ width: '100%' }}>
                    <Text>选择该项目允许使用的模型集合。模型集合本身在“模型治理”中维护。</Text>
                    <Select
                      multiple
                      filter
                      loading={modelsLoading}
                      value={assignedModelSetIDs}
                      optionList={modelSets.map(set => ({ label: `${set.name} (${set.slug})`, value: set.id }))}
                      onChange={value => setAssignedModelSetIDs((value as number[]) || [])}
                      style={{ width: '100%' }}
                    />
                    <Button type="primary" onClick={saveModels}>保存模型范围</Button>
                  </Space>
                )}
              </TabPane>
            )}
          </Tabs>
        </Surface>
        </div>
      ) : (
        <EmptyState title="请选择项目" description="从上方项目列表进入资料、预算、成员或模型范围。" />
      )}

      <SideSheet title="新建项目" visible={createSheet} onCancel={() => setCreateSheet(false)} width={520}>
        <Form getFormApi={setCreateFormApi} onSubmit={submitCreate}>
          <Form.Select
            field="org_id"
            label="所属组织"
            initValue={selectedOrgID || currentUser?.org_id}
            disabled={!platform}
            optionList={(platform ? organizations : [{ id: currentUser?.org_id || 0, name: currentUser?.org_name || '当前组织' }]).map(org => ({ label: `${org.name} (#${org.id})`, value: org.id }))}
            rules={[{ required: true, message: '请选择组织' }]}
            style={{ width: '100%' }}
          />
          <Form.Input field="name" label="项目名称" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="slug" label="项目标识（可留空自动生成）" />
          <Form.TextArea field="description" label="项目说明" />
          <Form.InputNumber field="budget_yuan" label="预算金额（元）" min={0} max={maxProjectBudgetYuan} precision={2} initValue={0} style={{ width: '100%' }} />
          <Form.Select field="budget_period" label="预算周期" optionList={periodOptions} initValue="monthly" style={{ width: '100%' }} />
          <Space style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
            <Button onClick={() => setCreateSheet(false)}>取消</Button>
            <Button type="primary" onClick={() => createFormApi?.submitForm()}>创建</Button>
          </Space>
        </Form>
      </SideSheet>
    </div>
  );
}
