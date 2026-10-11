import '../i18n/people';
import { translate as t, translateLabel, localizeOptions, useI18n } from '../i18n';
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { Banner, Button, Form, Input, Modal, Select, SideSheet, Space, Table, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { ApiError, api } from '../api/client';
import {
  canonicalOrganizationRole,
  canonicalProjectMembershipRole,
  isPlatformAdminRole,
} from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { organizationRoleLabel } from '../utils/displayLabels';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { loadAllPages } from '../utils/loadAllPages';
import { projectDisplayName } from '../utils/projectDisplay';
import {
  deleteLocalView,
  loadSavedViews,
  readPositiveQueryParam,
  replaceControlledQuery,
  saveLocalView,
  type SavedView,
} from '../utils/viewState';

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
  budget_cents?: number;
  budget_period?: string;
  key_count?: number;
  current_user_membership_role?: string;
}

interface ProjectMember {
  user_id: number;
  username: string;
  email?: string;
  auth_provider: string;
  organization_role: string;
  user_role: string;
  status: string;
  membership_role: string;
}

interface EligibleUser {
  user_id: number;
  username: string;
  email?: string;
  organization_role: string;
  user_role: string;
  status: string;
}

interface ModelSet {
  id: number;
  name: string;
  slug: string;
  status: string;
}

type ProjectTab = 'overview' | 'members' | 'models';

interface ProjectSavedState {
  org_id?: number;
  project_id: number;
  project_tab: ProjectTab;
  project_page: number;
  project_page_size: number;
}

const projectQueryKeys = [
  'org_id',
  'project_id',
  'project_tab',
  'project_page',
  'project_page_size',
] as const;

const projectSavedViewStorageKey = 'unirouter.projects.saved-views.v1';

const roleNames: Record<string, string> = {
  project_admin: '项目管理员',
  project_user: '项目使用者（可调用）',
  project_viewer: '项目只读',
  project_developer: '项目使用者（兼容）',
  developer: '项目使用者（兼容）',
  viewer: '项目只读（兼容）',
};

const projectMembershipOptions = [
  { label: '项目管理员', value: 'project_admin' },
  { label: '项目使用者（可调用）', value: 'project_user' },
  { label: '项目只读', value: 'project_viewer' },
];

const periodOptions = [
  { label: '不限制', value: 'none' },
  { label: '每日', value: 'daily' },
  { label: '每月', value: 'monthly' },
];

function normalizeProjectTab(value: unknown): ProjectTab {
  return value === 'members' || value === 'models' ? value : 'overview';
}

function readInitialProjectState() {
  const params = new URLSearchParams(window.location.search);
  const pageSize = readPositiveQueryParam(params, 'project_page_size');
  return {
    orgID: readPositiveQueryParam(params, 'org_id'),
    projectID: readPositiveQueryParam(params, 'project_id'),
    tab: normalizeProjectTab(params.get('project_tab')),
    page: readPositiveQueryParam(params, 'project_page') || 1,
    pageSize: pageSize && [20, 50, 100].includes(pageSize) ? pageSize : 20,
  };
}

function normalizeProjectSavedState(value: unknown): ProjectSavedState | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const source = value as Record<string, unknown>;
  const projectID = Number(source.project_id);
  const orgID = source.org_id === undefined ? undefined : Number(source.org_id);
  const page = Number(source.project_page);
  const pageSize = Number(source.project_page_size);
  if (!Number.isSafeInteger(projectID) || projectID <= 0 ||
    (orgID !== undefined && (!Number.isSafeInteger(orgID) || orgID <= 0)) ||
    !Number.isSafeInteger(page) || page <= 0 ||
    !Number.isSafeInteger(pageSize) || ![20, 50, 100].includes(pageSize)) {
    return null;
  }
  return {
    org_id: orgID,
    project_id: projectID,
    project_tab: normalizeProjectTab(source.project_tab),
    project_page: page,
    project_page_size: pageSize,
  };
}

function syncProjectURL(state: {
  projectID?: number;
  tab: ProjectTab;
  page: number;
  pageSize: number;
}) {
  replaceControlledQuery(projectQueryKeys, {
    org_id: undefined,
    project_id: state.projectID,
    project_tab: state.tab === 'overview' ? undefined : state.tab,
    project_page: state.page === 1 ? undefined : state.page,
    project_page_size: state.pageSize === 20 ? undefined : state.pageSize,
  });
}

function yuanFromCents(cents: number) {
  return Number((Number(cents || 0) / 100).toFixed(2));
}

function centsFromYuan(yuan: unknown) {
  const value = Number(yuan || 0);
  return Number.isFinite(value) && value >= 0 && value <= maxProjectBudgetYuan ? Math.round(value * 100) : -1;
}

export default function ProjectManagementPage({ embedded = false }: { embedded?: boolean }) {
  const { locale } = useI18n();
  const currentUser = useAuthStore(state => state.user);
  const role = currentUser?.role || '';
  const organizationRole = canonicalOrganizationRole(currentUser?.organization_role, role);
  const platform = isPlatformAdminRole(organizationRole);
  const customerContext = resolveCustomerContext(currentUser);
  const customerOrgID = effectiveCustomerOrgID(currentUser);
  const initialState = useRef(readInitialProjectState()).current;
  const canCreateProject = platform || organizationRole === 'org_admin';
  const licenseModelGovernance = currentUser?.license_access?.features?.model_governance === true;

  const selectedOrgID = customerOrgID;
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectTotal, setProjectTotal] = useState(0);
  const [projectPage, setProjectPage] = useState(initialState.page);
  const [projectPageSize, setProjectPageSize] = useState(initialState.pageSize);
  const [projectLoading, setProjectLoading] = useState(false);
  const [projectLoadError, setProjectLoadError] = useState('');
  const [selectedProjectID, setSelectedProjectID] = useState<number | undefined>(initialState.projectID);
  const selectedProjectIDRef = useRef<number | undefined>(initialState.projectID);
  const [activeTab, setActiveTab] = useState<ProjectTab>(initialState.tab);
  const activeTabRef = useRef<ProjectTab>(initialState.tab);
  const [reloadVersion, setReloadVersion] = useState(0);
  const [createSheet, setCreateSheet] = useState(false);
  const [createFormApi, setCreateFormApi] = useState<any>(null);
  const [overviewFormApi, setOverviewFormApi] = useState<any>(null);

  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [memberTotal, setMemberTotal] = useState(0);
  const [memberPage, setMemberPage] = useState(1);
  const [memberPageSize, setMemberPageSize] = useState(20);
  const [eligibleUsers, setEligibleUsers] = useState<EligibleUser[]>([]);
  const [selectedCandidateID, setSelectedCandidateID] = useState<number>();
  const [selectedMembershipRole, setSelectedMembershipRole] = useState('project_user');
  const [candidateQuery, setCandidateQuery] = useState('');
  const [membersLoading, setMembersLoading] = useState(false);

  const [modelSets, setModelSets] = useState<ModelSet[]>([]);
  const [assignedModelSetIDs, setAssignedModelSetIDs] = useState<number[]>([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelError, setModelError] = useState('');
  const projectRequestVersion = useRef(0);
  const memberRequestVersion = useRef(0);
  const modelRequestVersion = useRef(0);
  const [savedViews, setSavedViews] = useState<SavedView<ProjectSavedState>[]>(() => (
    loadSavedViews(projectSavedViewStorageKey, normalizeProjectSavedState)
  ));
  const [savedViewName, setSavedViewName] = useState('');
  const [selectedSavedView, setSelectedSavedView] = useState<string>();

  const selectedProject = useMemo(
    () => projects.find(project => project.id === selectedProjectID) || null,
    [projects, selectedProjectID],
  );
  const selectedProjectMembershipRole = canonicalProjectMembershipRole(selectedProject?.current_user_membership_role);
  const organizationCanManageProjects = platform || organizationRole === 'org_admin';
  const canManageMembers = organizationCanManageProjects || selectedProjectMembershipRole === 'project_admin';
  const canEditProject = canManageMembers;
  const canGrantProjectAdmin = organizationCanManageProjects;
  const membershipOptions = useMemo(() => localizeOptions(canGrantProjectAdmin
    ? projectMembershipOptions
    : projectMembershipOptions.filter(option => option.value !== 'project_admin')), [canGrantProjectAdmin, locale]);

  const loadProjects = useCallback((page: number, pageSize: number) => {
    if (!customerOrgID || selectedOrgID !== customerOrgID) {
      setProjects([]);
      setProjectTotal(0);
      selectedProjectIDRef.current = undefined;
      setSelectedProjectID(undefined);
      return Promise.resolve();
    }
    const version = ++projectRequestVersion.current;
    const desiredProjectID = selectedProjectIDRef.current;
    memberRequestVersion.current += 1;
    modelRequestVersion.current += 1;
    setProjectLoading(true);
    setProjectLoadError('');
    setProjects([]);
    setProjectTotal(0);
    setSelectedProjectID(undefined);
    setMembers([]);
    setMemberTotal(0);
    setEligibleUsers([]);
    setMembersLoading(false);
    setModelSets([]);
    setAssignedModelSetIDs([]);
    setModelsLoading(false);
    setModelError('');
    return api.getProjects(page, pageSize, { org_id: platform ? selectedOrgID : undefined })
      .then((response: any) => {
        if (version !== projectRequestVersion.current) return;
        const nextProjects = response.data || [];
        const preferred = nextProjects.find((project: Project) => project.id === desiredProjectID) || nextProjects[0];
        setProjects(nextProjects);
        setProjectTotal(response.total || 0);
        selectedProjectIDRef.current = preferred?.id;
        setSelectedProjectID(preferred?.id);
        syncProjectURL({
          projectID: preferred?.id,
          tab: activeTabRef.current,
          page,
          pageSize,
        });
      })
      .catch(() => {
        if (version !== projectRequestVersion.current) return;
        setProjects([]);
        setProjectTotal(0);
        selectedProjectIDRef.current = undefined;
        setSelectedProjectID(undefined);
        setProjectLoadError('无法加载当前组织与分页范围内的项目。');
        syncProjectURL({
          tab: activeTabRef.current,
          page,
          pageSize,
        });
        Toast.error(t("项目列表加载失败"));
      })
      .finally(() => {
        if (version === projectRequestVersion.current) setProjectLoading(false);
      });
  }, [customerOrgID, platform, selectedOrgID]);

  useEffect(() => {
    void loadProjects(projectPage, projectPageSize);
    return () => { projectRequestVersion.current += 1; };
  }, [loadProjects, projectPage, projectPageSize, reloadVersion]);

  const loadMembers = useCallback((projectID: number, page = 1, pageSize = 20, query = '') => {
    const version = ++memberRequestVersion.current;
    setMembersLoading(true);
    return api.getProjectMembers(projectID, page, pageSize, query)
      .then((response: any) => {
        if (version !== memberRequestVersion.current) return;
        setMembers(response.data || []);
        setMemberTotal(response.total || 0);
        setEligibleUsers(response.eligible_users || []);
      })
      .catch(() => {
        if (version !== memberRequestVersion.current) return;
        setMembers([]);
        setMemberTotal(0);
        setEligibleUsers([]);
        Toast.error(t("项目成员加载失败"));
      })
      .finally(() => {
        if (version === memberRequestVersion.current) setMembersLoading(false);
      });
  }, []);

  const loadModels = useCallback(async (project: Project) => {
    const version = ++modelRequestVersion.current;
    setModelsLoading(true);
    setModelError('');
    try {
      const [sets, assigned] = await Promise.all([
        loadAllPages<ModelSet>((page, size) => api.getModelSets(page, size, { org_id: project.org_id })),
        api.getProjectModelSets(project.id),
      ]);
      if (version !== modelRequestVersion.current) return;
      setModelSets(sets.filter(item => item.status === 'active'));
      setAssignedModelSetIDs((assigned.data || assigned.model_set_ids || []).map((item: any) => Number(item.model_set_id ?? item.id ?? item)));
    } catch (error) {
      if (version !== modelRequestVersion.current) return;
      setModelSets([]);
      setAssignedModelSetIDs([]);
      setModelError(error instanceof ApiError && error.status === 403
        ? '当前角色或商业授权不允许维护项目模型范围。'
        : '模型范围加载失败，请稍后重试。');
    } finally {
      if (version === modelRequestVersion.current) setModelsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!selectedProject) return;
    const budgetVisible = selectedProject.budget_cents !== undefined && selectedProject.budget_period !== undefined;
    overviewFormApi?.setValues({
      name: selectedProject.name,
      slug: selectedProject.slug,
      description: selectedProject.description || '',
      status: selectedProject.status,
      budget_yuan: budgetVisible ? yuanFromCents(selectedProject.budget_cents as number) : undefined,
      budget_period: budgetVisible ? selectedProject.budget_period : undefined,
    });
  }, [overviewFormApi, selectedProject]);

  useEffect(() => {
    if (!selectedProject) return;
    if (activeTab === 'members' && canManageMembers) void loadMembers(selectedProject.id, 1, memberPageSize);
    if (activeTab === 'models' && canManageMembers && licenseModelGovernance) void loadModels(selectedProject);
  }, [activeTab, canManageMembers, licenseModelGovernance, loadMembers, loadModels, memberPageSize, selectedProject]);

  useEffect(() => {
    if (canManageMembers || activeTab === 'overview') return;
    activeTabRef.current = 'overview';
    setActiveTab('overview');
  }, [activeTab, canManageMembers]);

  const selectProject = useCallback((project: Project) => {
    memberRequestVersion.current += 1;
    modelRequestVersion.current += 1;
    selectedProjectIDRef.current = project.id;
    setSelectedProjectID(project.id);
    activeTabRef.current = 'overview';
    setActiveTab('overview');
    setMemberPage(1);
    setMembers([]);
    setMemberTotal(0);
    setEligibleUsers([]);
    setModelSets([]);
    setAssignedModelSetIDs([]);
    setModelError('');
    setSelectedCandidateID(undefined);
    setSelectedMembershipRole('project_user');
    setCandidateQuery('');
    syncProjectURL({
      projectID: project.id,
      tab: 'overview',
      page: projectPage,
      pageSize: projectPageSize,
    });
  }, [projectPage, projectPageSize]);

  const changeActiveTab = (value: string) => {
    const nextTab = canManageMembers ? normalizeProjectTab(value) : 'overview';
    activeTabRef.current = nextTab;
    setActiveTab(nextTab);
    syncProjectURL({
      projectID: selectedProjectIDRef.current,
      tab: nextTab,
      page: projectPage,
      pageSize: projectPageSize,
    });
  };

  const changeProjectPage = (nextPage: number) => {
    selectedProjectIDRef.current = undefined;
    setSelectedProjectID(undefined);
    setProjectPage(nextPage);
    syncProjectURL({
      tab: activeTabRef.current,
      page: nextPage,
      pageSize: projectPageSize,
    });
  };

  const changeProjectPageSize = (nextPageSize: number) => {
    selectedProjectIDRef.current = undefined;
    setSelectedProjectID(undefined);
    setProjectPageSize(nextPageSize);
    setProjectPage(1);
    syncProjectURL({
      tab: activeTabRef.current,
      page: 1,
      pageSize: nextPageSize,
    });
  };

  const saveCurrentProjectView = () => {
    if (!selectedProjectIDRef.current) {
      Toast.warning(t("当前没有可保存的项目工作区"));
      return;
    }
    const next = saveLocalView(projectSavedViewStorageKey, savedViews, savedViewName, {
      project_id: selectedProjectIDRef.current,
      project_tab: activeTabRef.current,
      project_page: projectPage,
      project_page_size: projectPageSize,
    });
    if (!next.saved) {
      Toast.error(t("视图名称需为 1 至 40 个字符，并且浏览器存储必须可用。"));
      return;
    }
    const name = savedViewName.trim();
    setSavedViews(next.views);
    setSelectedSavedView(name);
    setSavedViewName(name);
    Toast.success(t("项目工作区已保存在此浏览器"));
  };

  const applyProjectView = (name: string) => {
    const view = savedViews.find(item => item.name === name);
    if (!view) return;
    const nextTab = canManageMembers ? view.state.project_tab : 'overview';
    memberRequestVersion.current += 1;
    modelRequestVersion.current += 1;
    setSelectedSavedView(name);
    setSavedViewName(name);
    selectedProjectIDRef.current = view.state.project_id;
    setSelectedProjectID(view.state.project_id);
    activeTabRef.current = nextTab;
    setActiveTab(nextTab);
    setProjectPage(view.state.project_page);
    setProjectPageSize(view.state.project_page_size);
    syncProjectURL({
      projectID: view.state.project_id,
      tab: nextTab,
      page: view.state.project_page,
      pageSize: view.state.project_page_size,
    });
    setReloadVersion(version => version + 1);
  };

  const deleteProjectView = () => {
    if (!selectedSavedView) return;
    const next = deleteLocalView(projectSavedViewStorageKey, savedViews, selectedSavedView);
    if (!next.deleted) {
      Toast.error(t("无法删除此浏览器中的项目工作区"));
      return;
    }
    setSavedViews(next.views);
    setSelectedSavedView(undefined);
    setSavedViewName('');
    Toast.success(t("已删除此浏览器中的项目工作区"));
  };

  const submitCreate = async (values: any) => {
    const budgetCents = centsFromYuan(values.create_budget_yuan);
    if (budgetCents < 0) {
      Toast.error(t('预算金额需在 0 至 {max} 元之间', { max: maxProjectBudgetYuan.toFixed(2) }));
      return;
    }
    if (!selectedOrgID) {
      Toast.error(t("无法确认客户组织，未创建项目"));
      return;
    }
    try {
      await api.createProject({
        org_id: selectedOrgID,
        name: values.create_name,
        slug: values.create_slug,
        description: values.create_description || '',
        budget_cents: budgetCents,
        budget_period: values.create_budget_period || 'monthly',
      });
      Toast.success(t("项目已创建"));
      setCreateSheet(false);
      selectedProjectIDRef.current = undefined;
      setProjectPage(1);
      setReloadVersion(version => version + 1);
    } catch {
      Toast.error(t("项目创建失败"));
    }
  };

  const submitOverview = async (values: any) => {
    if (!selectedProject) return;
    const budgetCents = centsFromYuan(values.budget_yuan);
    if (budgetCents < 0) {
      Toast.error(t('预算金额需在 0 至 {max} 元之间', { max: maxProjectBudgetYuan.toFixed(2) }));
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
      Toast.success(t("项目已更新"));
      setReloadVersion(version => version + 1);
    } catch {
      Toast.error(t("项目更新失败"));
    }
  };

  const addMember = async () => {
    if (!selectedProject || !selectedCandidateID) return;
    const candidate = eligibleUsers.find(item => item.user_id === selectedCandidateID);
    if (!candidate) return;
    try {
      await api.upsertProjectMember(selectedProject.id, candidate.user_id, selectedMembershipRole);
      Toast.success(t("成员已加入项目"));
      setSelectedCandidateID(undefined);
      await loadMembers(selectedProject.id, memberPage, memberPageSize, candidateQuery);
    } catch {
      Toast.error(t("添加成员失败"));
    }
  };

  const updateMemberRole = useCallback(async (member: ProjectMember, nextRole: string) => {
    if (!selectedProject) return;
    try {
      await api.upsertProjectMember(selectedProject.id, member.user_id, nextRole);
      Toast.success(t("项目角色已更新"));
      await loadMembers(selectedProject.id, memberPage, memberPageSize, candidateQuery);
    } catch (error) {
      Toast.error(error instanceof ApiError ? error.message : t("项目角色更新失败"));
    }
  }, [candidateQuery, loadMembers, memberPage, memberPageSize, selectedProject]);

  const removeMember = useCallback((member: ProjectMember) => {
    if (!selectedProject) return;
    const project = selectedProject;
    Modal.confirm({
      title: t("移除项目成员"),
      content: t('确认从 {project} 移除 {name}？这不会改变其组织角色或其他项目成员关系。', { project: projectDisplayName(project), name: member.username }),
      onOk: async () => {
        try {
          await api.deleteProjectMember(project.id, member.user_id);
          Toast.success(t("成员已移除"));
          await loadMembers(project.id, memberPage, memberPageSize, candidateQuery);
        } catch (error) {
          Toast.error(error instanceof ApiError ? error.message : t("移除成员失败"));
        }
      },
    });
  }, [candidateQuery, loadMembers, memberPage, memberPageSize, selectedProject]);

  const saveModels = async () => {
    if (!selectedProject) return;
    try {
      await api.replaceProjectModelSets(selectedProject.id, assignedModelSetIDs);
      Toast.success(t("项目模型范围已更新"));
    } catch {
      Toast.error(t("项目模型范围更新失败"));
    }
  };

  // Semi Table compares column identity in componentDidUpdate. Keeping these
  // pure definitions stable prevents an application-StrictMode render from
  // turning the persistent table + inspector form into a nested update loop.
  const columns = useMemo(() => [
    { title: t('项目'), key: 'project', width: 220, render: (_: unknown, project: Project) => <div className="ur-identity-cell">
      <strong>{projectDisplayName(project)}</strong>
      <small>{project.slug} · {customerContext.orgName || t('当前客户')}</small>
      <StatusTag status={project.status}>{project.status === 'active' ? t('启用') : project.status === 'disabled' ? t('停用') : t('归档')}</StatusTag>
    </div> },
    { title: t('预算'), key: 'budget', width: 160, render: (_: unknown, project: Project) => <div className="ur-identity-cell">
      <span>{!project.budget_period ? t('受限字段') : project.budget_period === 'none' || !project.budget_cents ? t('不限制') : `¥${yuanFromCents(project.budget_cents).toFixed(2)} / ${project.budget_period === 'daily' ? t('日') : t('月')}`}</span>
      <small>{t('凭证数')}: <span>{typeof project.key_count === 'number' ? project.key_count : t('受限字段')}</span></small>
    </div> },
    { title: t('我的项目角色'), dataIndex: 'current_user_membership_role', width: 170, render: (value: string) => value ? (Object.prototype.hasOwnProperty.call(roleNames, canonicalProjectMembershipRole(value)) ? translateLabel(roleNames[canonicalProjectMembershipRole(value)]) : value) : t('组织级权限') },
  ], [customerContext.orgName, locale]);

  const memberColumns = useMemo(() => [
    { title: t("用户"), dataIndex: 'username', render: (username: string, member: ProjectMember) => <span>{username}{member.email ? <Text type="tertiary"> · {member.email}</Text> : null}</span> },
    { title: t("组织角色"), dataIndex: 'organization_role', render: (value: string) => <Tag>{organizationRoleLabel(value)}</Tag> },
    {
      title: t("项目角色"),
      dataIndex: 'membership_role',
      render: (value: string, member: ProjectMember) => {
        const normalized = canonicalProjectMembershipRole(value);
        const membershipScopedAdminCannotChange = !canGrantProjectAdmin && normalized === 'project_admin';
        return (
          <Select
            aria-label={t('{name}的项目角色', { name: member.username })}
            value={normalized}
            optionList={membershipOptions}
            disabled={membershipScopedAdminCannotChange}
            onChange={next => void updateMemberRole(member, String(next))}
            style={{ width: 190 }}
          />
        );
      },
    },
    { title: t("账号来源"), dataIndex: 'auth_provider', render: (value: string) => value === 'oidc' ? t("企业 OIDC") : t("本地账号") },
    { title: t("状态"), dataIndex: 'status', render: (value: string) => <StatusTag status={value}>{value === 'active' ? t("启用") : t("停用")}</StatusTag> },
    {
      title: t("操作"),
      render: (_: unknown, member: ProjectMember) => (
        <Button
          type="danger"
          theme="borderless"
          icon={<IconDelete />}
          disabled={!canGrantProjectAdmin && canonicalProjectMembershipRole(member.membership_role) === 'project_admin'}
          onClick={() => removeMember(member)}
        >{t("移除")}</Button>
      ),
    },
  ], [canGrantProjectAdmin, membershipOptions, removeMember, updateMemberRole, locale]);

  const projectRowProps = useCallback((record?: Project) => {
    if (!record) return {};
    return {
      onClick: () => selectProject(record),
      onKeyDown: (event: KeyboardEvent) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          selectProject(record);
        }
      },
      tabIndex: 0,
      'aria-selected': selectedProjectID === record.id,
      style: {
        cursor: 'pointer',
        background: selectedProjectID === record.id ? 'var(--ur-bg-subtle)' : undefined,
      },
    };
  }, [selectProject, selectedProjectID]);

  const candidateOptions = useMemo(() => eligibleUsers.map(candidate => ({
    value: candidate.user_id,
    label: `${candidate.username} · ${organizationRoleLabel(candidate.organization_role || candidate.user_role)}${candidate.status === 'active' ? '' : t("（已停用）")}`,
  })), [eligibleUsers, locale]);

  if (customerContext.state !== 'ready') {
    const blocker = customerContextBlockerCopy(customerContext.state);
    return (
      <div className={embedded ? 'ur-embedded-page ur-people-workspace' : 'ur-page ur-page-wide ur-people-workspace'}>
        {!embedded ? (
          <PageHeader eyebrow={t("访问与安全")} title={t("项目与成员")} description={t("本页只管理当前部署唯一客户组织中的项目。")} />
        ) : null}
        <Surface>
          <EmptyState title={blocker.title} description={blocker.description} minHeight={260} />
        </Surface>
      </div>
    );
  }

  return (
    <div className={embedded ? 'ur-embedded-page ur-people-workspace' : 'ur-page ur-page-wide ur-people-workspace'}>
      {embedded ? (
        <div className="ur-embedded-heading">
          <div>
            <Typography.Title heading={4} style={{ margin: 0 }}>{t("项目与成员")}</Typography.Title>
            <Text type="tertiary">{t("项目是 UniRouter 本地权限和成本归属范围；人员可同时属于多个项目。")}</Text>
          </div>
          <Space>
            <Button icon={<IconRefresh />} loading={projectLoading} onClick={() => setReloadVersion(version => version + 1)}>{t("刷新")}</Button>
            {canCreateProject && <Button aria-label={t("新建项目")} type="primary" icon={<IconPlus />} onClick={() => setCreateSheet(true)}>{t("新建项目")}</Button>}
          </Space>
        </div>
      ) : <PageHeader
        eyebrow={t("访问与安全")}
        title={t("项目与成员")}
        description={t("项目列表与工作区保持并列；首个项目自动进入检查器，无需再次打开详情。")}
        actions={<Space>
          <Button icon={<IconRefresh />} loading={projectLoading} onClick={() => setReloadVersion(version => version + 1)}>{t("刷新")}</Button>
          {canCreateProject && <Button aria-label={t("新建项目")} type="primary" icon={<IconPlus />} onClick={() => setCreateSheet(true)}>{t("新建项目")}</Button>}
        </Space>}
      />}

      <Surface title={projectTotal > 1 || savedViews.length > 0 ? t("工作区视图") : t("当前项目范围")} description={`${t('当前客户：')}${customerContext.orgName || t('组织 #{id}', { id: selectedOrgID || '—' })} · ${projectTotal > 1 || savedViews.length > 0 ? t("项目、分页和当前 Tab 会写入 URL；命名视图只保存在当前浏览器。") : t("只有一个项目时会自动进入，无需保存工作区视图。")}`}>
        {(projectTotal > 1 || savedViews.length > 0) && <div className="ur-project-view-bar">
          <div className="ur-saved-view-bar">
            <div role="group" aria-label={t("已保存的项目工作区")}>
              <Select
                aria-label={t("已保存工作区")}
                placeholder={t("已保存工作区（此浏览器）")}
                value={selectedSavedView}
                optionList={savedViews.map(view => ({ label: view.name, value: view.name }))}
                onChange={value => applyProjectView(String(value))}
                showClear
                onClear={() => setSelectedSavedView(undefined)}
                className="ur-saved-view-select"
              />
            </div>
            <Input
              aria-label={t("项目工作区名称")}
              placeholder={t("工作区名称")}
              value={savedViewName}
              maxLength={40}
              onChange={setSavedViewName}
              onEnterPress={saveCurrentProjectView}
            />
            <Button onClick={saveCurrentProjectView}>{t("保存 / 覆盖")}</Button>
            <Button disabled={!selectedSavedView} onClick={deleteProjectView}>{t("删除")}</Button>
          </div>
        </div>}
      </Surface>

      <div className="ur-context-explorer ur-project-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("项目列表")}</Typography.Title>
              <Text className="ur-panel-hint">{t("默认选择首条记录 · 键盘可切换项目")}</Text>
            </div>
            <Tag size="small">{projectTotal}  {t("个项目")}</Tag>
          </div>
          <div className="ur-table-shell">
          <Table
            rowKey="id"
            columns={columns}
            dataSource={projects}
            loading={projectLoading}
            scroll={{ x: 550 }}
            onRow={projectRowProps}
            empty={projectLoadError ? (
              <EmptyState
                title={t("项目列表加载失败")}
                description={translateLabel(projectLoadError)}
                actions={<Button type="primary" onClick={() => setReloadVersion(version => version + 1)}>{t("重新加载项目")}</Button>}
                minHeight={240}
              />
            ) : <EmptyState title={t("当前范围暂无项目")} minHeight={240} />}
            pagination={{
              currentPage: projectPage,
              pageSize: projectPageSize,
              total: projectTotal,
              showSizeChanger: true,
              pageSizeOpts: [20, 50, 100],
              onPageChange: changeProjectPage,
              onPageSizeChange: changeProjectPageSize,
            }}
          />
          </div>
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail ur-project-workspace" aria-label={t("项目工作区检查器")}>
          <div className="ur-detail-header">
            <div>
              <Text className="ur-panel-hint">{t("项目详情")}</Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("项目管理")}</Typography.Title>
            </div>
            <Tag size="small" color={selectedProject ? 'blue' : undefined}>{selectedProject ? projectDisplayName(selectedProject) : t("等待选择")}</Tag>
          </div>

          {selectedProject ? (
            <div className="ur-project-detail" aria-label={t('项目详情：{name}', { name: selectedProject.name })}>
              <div className="ur-detail-hero">
                <div>
                  <Text type="tertiary" size="small">{customerContext.orgName || t("当前客户")}</Text>
                  <div><Typography.Title heading={5} style={{ margin: 0 }}>{projectDisplayName(selectedProject)}</Typography.Title></div>
                  <Text type="tertiary">{selectedProject.description || t("暂无项目说明")}</Text>
                </div>
                <StatusTag status={selectedProject.status}>
                  {selectedProject.status === 'active' ? t("启用") : selectedProject.status === 'disabled' ? t("停用") : t("归档")}
                </StatusTag>
              </div>
              <Tabs activeKey={activeTab} onChange={changeActiveTab}>
            <TabPane tab={t("项目概况")} itemKey="overview">
              {(selectedProject.budget_cents === undefined || selectedProject.budget_period === undefined) && (
                <Banner
                  type="info"
                  closeIcon={null}
                  description={t("当前组织只读视图不包含预算和凭证统计；缺失值不会按 0 或“不限制”解释。")}
                  style={{ marginBottom: 12 }}
                />
              )}
              <Form getFormApi={setOverviewFormApi} onSubmit={submitOverview} style={{ maxWidth: 720 }}>
                <Form.Input field="name" label={<span id="project-overview-name-label">{t("项目名称")}</span>} aria-labelledby="project-overview-name-label" disabled={!canEditProject} rules={[{ required: true, message: t("必填") }]} />
                <Form.Input field="slug" label={<span id="project-overview-slug-label">{t("项目标识")}</span>} aria-labelledby="project-overview-slug-label" disabled={!canEditProject} rules={[{ required: true, message: t("必填") }]} />
                <Form.TextArea field="description" label={<span id="project-overview-description-label">{t("项目说明")}</span>} aria-labelledby="project-overview-description-label" disabled={!canEditProject} />
                <Form.Select field="status" label={<span id="project-overview-status-label">{t("项目状态")}</span>} aria-labelledby="project-overview-status-label" disabled={!canEditProject} optionList={[
                  { label: t("启用"), value: 'active' }, { label: t("停用"), value: 'disabled' }, { label: t("归档"), value: 'archived' },
                ]} />
                <Form.InputNumber field="budget_yuan" label={<span id="project-overview-budget-label">{t("预算金额（元）")}</span>} aria-labelledby="project-overview-budget-label" disabled={!canEditProject} min={0} max={maxProjectBudgetYuan} precision={2} extraText={t("0 表示不限制；结算和告警以所选预算周期为准。")} style={{ width: '100%' }} />
                <Form.Select field="budget_period" label={<span id="project-overview-budget-period-label">{t("预算周期")}</span>} aria-labelledby="project-overview-budget-period-label" disabled={!canEditProject} optionList={localizeOptions(periodOptions)} style={{ width: '100%' }} />
                {canEditProject && <Button type="primary" htmlType="submit">{t("保存项目")}</Button>}
              </Form>
            </TabPane>

            {canManageMembers && (
              <TabPane tab={t("成员与角色")} itemKey="members">
                <div className="ur-project-member-controls">
                  <Input
                    aria-label={t("搜索项目成员候选人")}
                    value={candidateQuery}
                    placeholder={t("按用户名或邮箱搜索候选人")}
                    onChange={setCandidateQuery}
                    onEnterPress={() => loadMembers(selectedProject.id, 1, memberPageSize, candidateQuery)}
                    className="ur-project-member-search"
                  />
                  <Button onClick={() => loadMembers(selectedProject.id, 1, memberPageSize, candidateQuery)}>{t("搜索")}</Button>
                  <Select
                    aria-labelledby="project-member-candidate-label"
                    insetLabel={t("账号")}
                    insetLabelId="project-member-candidate-label"
                    value={selectedCandidateID}
                    placeholder={t("选择同组织的活跃账号")}
                    optionList={candidateOptions}
                    onChange={value => setSelectedCandidateID(Number(value))}
                    filter
                    className="ur-project-member-select"
                  />
                  <Select
                    aria-labelledby="project-member-role-label"
                    insetLabel={t("项目角色")}
                    insetLabelId="project-member-role-label"
                    value={selectedMembershipRole}
                    optionList={membershipOptions}
                    onChange={value => setSelectedMembershipRole(String(value))}
                    style={{ width: 210 }}
                  />
                  <Button type="primary" disabled={!selectedCandidateID} onClick={addMember}>{t("加入项目")}</Button>
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
              <TabPane tab={t("模型范围")} itemKey="models">
                {!licenseModelGovernance ? (
                  <EmptyState title={t("模型治理暂不可用")} description={t("模型治理随有效商业授权自动包含；请刷新授权状态，若仍不可用请检查许可证有效性。")} />
                ) : modelError ? (
                  <Banner type="danger" description={translateLabel(modelError)} closeIcon={null} />
                ) : (
                  <Space vertical align="start" spacing="medium" style={{ width: '100%' }}>
                    <Text>{t("选择该项目允许使用的模型集合。模型集合本身在“模型治理”中维护。")}</Text>
                    <Select
                      aria-labelledby="project-model-set-scope-label"
                      insetLabel={t("允许的模型集")}
                      insetLabelId="project-model-set-scope-label"
                      multiple
                      filter
                      loading={modelsLoading}
                      value={assignedModelSetIDs}
                      optionList={modelSets.map(set => ({ label: `${set.name} (${set.slug})`, value: set.id }))}
                      onChange={value => setAssignedModelSetIDs((value as number[]) || [])}
                      style={{ width: '100%' }}
                    />
                    <Button type="primary" onClick={saveModels}>{t("保存模型范围")}</Button>
                  </Space>
                )}
              </TabPane>
            )}
              </Tabs>
            </div>
          ) : (
            <div className="ur-receipt-empty" role="status">
              <Typography.Title heading={5}>
                {projectLoading ? t("正在加载项目工作区") : projectLoadError ? t("等待可验证项目") : t("暂无可管理项目")}
              </Typography.Title>
              <Text type="tertiary">
                {projectLoadError
                  ? t("加载失败时不会保留上一组织或分页范围的项目详情。")
                  : t("有项目时会自动选择当前页第一项，无需再次点击打开。")}
              </Text>
            </div>
          )}
        </aside>
      </div>

      <SideSheet title={t("新建项目")} visible={createSheet} onCancel={() => setCreateSheet(false)} width="min(520px, 100vw)">
        <Form getFormApi={setCreateFormApi} onSubmit={submitCreate}>
          <Form.Input field="create_name" label={t("项目名称")} rules={[{ required: true, message: t("必填") }]} />
          <Form.Input field="create_slug" label={t("项目标识（可留空自动生成）")} />
          <Form.TextArea field="create_description" label={t("项目说明")} />
          <Form.InputNumber field="create_budget_yuan" label={t("预算金额（元）")} min={0} max={maxProjectBudgetYuan} precision={2} initValue={0} style={{ width: '100%' }} />
          <Form.Select field="create_budget_period" aria-label={t("新项目预算周期")} label={t("预算周期")} optionList={localizeOptions(periodOptions)} initValue="monthly" style={{ width: '100%' }} />
          <Space style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
            <Button onClick={() => setCreateSheet(false)}>{t("取消")}</Button>
            <Button type="primary" onClick={() => createFormApi?.submitForm()}>{t("创建")}</Button>
          </Space>
        </Form>
      </SideSheet>
    </div>
  );
}
