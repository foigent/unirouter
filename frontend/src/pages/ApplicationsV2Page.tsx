import '../i18n/access';
import { translate as t, translateLabel, localizeOptions, useI18n } from '../i18n';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Form, Modal, Select, SideSheet, Space, Table, Tabs, TabPane, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { IconKeyStroked, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { useSearchParams } from 'react-router-dom';
import { accessApi, effectiveGrantStatus, isGrantCurrentlyActive, type AccessCredentialFamily, type AccessGrant, type AccessRequest, type IssuedCredential, type LegacyAccessClassification, type WorkloadApplication, type WorkloadContract } from '../api/access';
import { ApiError, api } from '../api/client';
import { canonicalProjectMembershipRole, isOrgAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { IntentRequestIDs } from '../utils/intentRequestId';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { loadAllPages } from '../utils/loadAllPages';
import { parseSortedModelList } from '../utils/modelList';
import { projectOption } from '../utils/projectDisplay';

const { Text } = Typography;

type Project = { id: number; org_id: number; name: string; slug?: string; status: string; current_user_membership_role?: string };
type ApplicationScope = { projectID: number; applicationID?: number; applicationRef?: string };
type ProjectMember = { user_id: number; username: string; status: string; membership_role: string };

const applicationKindOptions = [
  { value: 'service', label: '服务应用' },
  { value: 'agent', label: 'Agent' },
  { value: 'workflow', label: '工作流' },
  { value: 'integration_adapter', label: '集成适配器' },
];

function applicationKindLabel(value: string) {
  const label = applicationKindOptions.find((option) => option.value === value)?.label;
  return label ? translateLabel(label) : value;
}

function environmentLabel(value: string) {
  return ({ development: t("开发"), staging: t("测试"), production: t("生产") } as Record<string, string>)[value] || value;
}

function accessRequestStatusLabel(value: AccessRequest['status']) {
  return ({ approved: t("已批准"), needs_review: t("待审核"), rejected: t("已拒绝"), cancelled: t("已取消") } as const)[value] || value;
}

function accessGrantStatusLabel(grant: AccessGrant) {
  const status = effectiveGrantStatus(grant);
  return ({ active: t("已生效"), expired: t("已过期"), suspended: t("已暂停"), revoked: t("已撤销") } as const)[status] || status;
}

function credentialStatusLabel(value: string) {
  return ({ active: t("可用"), disabled: t("停用"), revoked: t("已撤销"), grant_expired: t("授权已过期"), grant_suspended: t("授权已暂停"), grant_revoked: t("授权已撤销") } as Record<string, string>)[value] || value;
}

function grantBelongsToApplication(grant: AccessGrant, application: WorkloadApplication) {
  return grant.org_id === application.org_id && grant.project_id === application.project_id &&
    grant.subject.type === 'workload_application' && grant.subject.workload_application_id === application.id;
}

export default function ApplicationsV2Page() {
  const { locale } = useI18n();
  const user = useAuthStore((state) => state.user);
  const organizationRole = organizationRoleForUser(user);
  const customerContext = resolveCustomerContext(user);
  const customerOrgID = effectiveCustomerOrgID(user);
  const customerContextReady = customerContext.state === 'ready' && Boolean(customerOrgID);
  const [search, setSearch] = useSearchParams();
  const intents = useRef(new IntentRequestIDs()).current;
  const selectedProjectID = Number(search.get('project_id') || 0) || undefined;
  const selectedApplicationRef = search.get('application_ref') || '';
  const activeTab = search.get('tab') || 'applications';

  const [contract, setContract] = useState<WorkloadContract | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectsLoading, setProjectsLoading] = useState(true);
  const [projectsError, setProjectsError] = useState('');
  const [applications, setApplications] = useState<WorkloadApplication[]>([]);
  const [applicationsLoading, setApplicationsLoading] = useState(false);
  const [applicationsError, setApplicationsError] = useState('');
  const [accessRequests, setAccessRequests] = useState<AccessRequest[]>([]);
  const [grants, setGrants] = useState<AccessGrant[]>([]);
  const [credentials, setCredentials] = useState<AccessCredentialFamily[]>([]);
  const [requestError, setRequestError] = useState('');
  const [grantError, setGrantError] = useState('');
  const [credentialError, setCredentialError] = useState('');
	const [credentialHasMore, setCredentialHasMore] = useState(false);
	const [accessStateScope, setAccessStateScope] = useState<ApplicationScope | null>(null);
	const [accessLoadingScope, setAccessLoadingScope] = useState<ApplicationScope | null>(null);
  const [legacy, setLegacy] = useState<LegacyAccessClassification[]>([]);
  const [legacyLoading, setLegacyLoading] = useState(false);
  const [legacyError, setLegacyError] = useState('');
  const [drawer, setDrawer] = useState<'create' | 'edit' | 'request' | 'credential' | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [mutating, setMutating] = useState(false);
  const [secret, setSecret] = useState<IssuedCredential | null>(null);
	const [sponsorOptions, setSponsorOptions] = useState<Array<{ value: number; label: string }>>([]);
	const [sponsorLoading, setSponsorLoading] = useState(false);
	const [sponsorError, setSponsorError] = useState('');
	const [drawerScope, setDrawerScope] = useState<ApplicationScope | null>(null);
	const currentProjectID = useRef<number | undefined>(selectedProjectID);
	const currentApplicationRef = useRef(selectedApplicationRef);
	const applicationsGeneration = useRef(0);
	const accessGeneration = useRef(0);
	const sponsorGeneration = useRef(0);
	currentProjectID.current = selectedProjectID;
	currentApplicationRef.current = selectedApplicationRef;

  const selectedApplication = applications.find((item) => item.application_ref === selectedApplicationRef && item.project_id === selectedProjectID);
  const selectedProject = projects.find((item) => item.id === selectedProjectID);
	const accessStateIsCurrent = !!selectedApplication && accessStateScope?.projectID === selectedApplication.project_id &&
		accessStateScope.applicationID === selectedApplication.id && accessStateScope.applicationRef === selectedApplication.application_ref;
	const visibleAccessRequests = accessStateIsCurrent ? accessRequests : [];
	const visibleGrants = accessStateIsCurrent ? grants : [];
	const visibleCredentials = accessStateIsCurrent ? credentials : [];
	const visibleRequestError = accessStateIsCurrent ? requestError : '';
	const visibleGrantError = accessStateIsCurrent ? grantError : '';
	const visibleCredentialError = accessStateIsCurrent ? credentialError : '';
	const visibleCredentialHasMore = accessStateIsCurrent && credentialHasMore;
	const visibleAccessLoading = !!selectedApplication && accessLoadingScope?.projectID === selectedApplication.project_id &&
		accessLoadingScope.applicationID === selectedApplication.id && accessLoadingScope.applicationRef === selectedApplication.application_ref;
  const canManageSelectedProject = isOrgAdminRole(organizationRole) ||
    canonicalProjectMembershipRole(selectedProject?.current_user_membership_role) === 'project_admin';
	const activeGrant = selectedApplication && visibleGrants.find((grant) => grantBelongsToApplication(grant, selectedApplication) && isGrantCurrentlyActive(grant));
  const maySeeLegacy = isOrgAdminRole(organizationRole);
	const isCurrentScope = useCallback((scope: ApplicationScope) => currentProjectID.current === scope.projectID &&
		(scope.applicationRef === undefined || (currentApplicationRef.current === scope.applicationRef &&
			applications.some((item) => item.project_id === scope.projectID && item.id === scope.applicationID && item.application_ref === scope.applicationRef))), [applications]);
	const applicationScope = useCallback((application: WorkloadApplication): ApplicationScope => ({
		projectID: application.project_id, applicationID: application.id, applicationRef: application.application_ref,
	}), []);

  const updateSearch = useCallback((values: Record<string, string | number | undefined>) => {
    const next = new URLSearchParams(search);
    Object.entries(values).forEach(([key, value]) => {
      if (value === undefined || value === '') next.delete(key); else next.set(key, String(value));
    });
    setSearch(next, { replace: true });
  }, [search, setSearch]);

  const loadProjectsAndContract = useCallback(async () => {
    setProjectsLoading(true);
    setProjectsError('');
    if (!customerContextReady) {
      setContract(null);
      setProjects([]);
      setProjectsLoading(false);
      return;
    }
    try {
      const [profileContract, allProjects] = await Promise.all([
        api.getWorkloadApplicationContract() as Promise<WorkloadContract>,
        loadAllPages<Project>((page, size) => api.getProjects(page, size, { status: 'active' })),
      ]);
      setContract(profileContract);
      // ListProjects is already actor-scoped. Retain every project the backend
      // permits this actor to discover so a mixed-role user can inspect a
      // project_user project without inheriting project_admin write controls.
      setProjects(allProjects);
      if (!selectedProjectID && allProjects[0]) updateSearch({ project_id: allProjects[0].id });
    } catch {
      setProjects([]);
      setProjectsError('无法读取应用合同或当前账号可管理的项目。');
    } finally {
      setProjectsLoading(false);
    }
  }, [customerContextReady, organizationRole, selectedProjectID, updateSearch]);

  const loadApplications = useCallback(async () => {
	const projectID = selectedProjectID;
	const generation = ++applicationsGeneration.current;
	accessGeneration.current++;
	setApplications([]);
	setAccessStateScope(null);
	setAccessLoadingScope(null);
	setAccessRequests([]); setGrants([]); setCredentials([]); setCredentialHasMore(false);
	if (!customerContextReady || !projectID) { setApplicationsLoading(false); return; }
    setApplicationsLoading(true);
    setApplicationsError('');
    try {
	  const result = await api.getWorkloadApplications({ project_id: projectID }) as { data?: WorkloadApplication[] };
	  if (currentProjectID.current !== projectID || applicationsGeneration.current !== generation) return;
	  const items = (result.data || []).filter((item) => item.project_id === projectID);
      setApplications(items);
      if (selectedApplicationRef && !items.some((item) => item.application_ref === selectedApplicationRef)) {
        updateSearch({ application_ref: undefined });
      }
    } catch {
	  if (currentProjectID.current !== projectID || applicationsGeneration.current !== generation) return;
      setApplications([]);
      setApplicationsError('无法读取当前项目的应用。');
    } finally {
	  if (currentProjectID.current === projectID && applicationsGeneration.current === generation) setApplicationsLoading(false);
    }
  }, [customerContextReady, selectedApplicationRef, selectedProjectID, updateSearch]);

  const loadApplicationAccess = useCallback(async () => {
	const application = selectedApplication;
	const generation = ++accessGeneration.current;
	if (!customerContextReady || !application) {
	  setAccessStateScope(null);
	  setAccessLoadingScope(null);
      setAccessRequests([]); setGrants([]); setCredentials([]); setRequestError(''); setGrantError(''); setCredentialError('');
	  setCredentialHasMore(false);
      return;
    }
	const scope = applicationScope(application);
	setAccessStateScope(scope);
	setAccessLoadingScope(scope);
	setAccessRequests([]); setGrants([]); setCredentials([]); setCredentialHasMore(false);
    setRequestError('');
    setGrantError('');
    const [requestResult, grantResult] = await Promise.allSettled([
	  accessApi.listProjectRequests(application.project_id, { workload_application_id: application.id, limit: 50 }),
	  accessApi.listProjectGrants(application.project_id, { workload_application_id: application.id, limit: 50 }),
    ]);
	if (!isCurrentScope(scope) || accessGeneration.current !== generation) return;
    if (requestResult.status === 'fulfilled') setAccessRequests(requestResult.value.data.filter((request) => request.project_id === application.project_id &&
	  request.subject.type === 'workload_application' && request.subject.workload_application_id === application.id));
    else { setAccessRequests([]); setRequestError('访问申请暂时不可用；不会清空授权或 Key 的已知事实。'); }
    if (grantResult.status === 'fulfilled') setGrants(grantResult.value.data.filter((grant) => grantBelongsToApplication(grant, application)));
    else { setGrants([]); setGrantError('授权状态暂时不可用；不会把读取错误解释成“未授权”。'); }
    setCredentialError('');
	if (!application.can_read_credentials) {
      setCredentials([]);
	  if (isCurrentScope(scope) && accessGeneration.current === generation) setAccessLoadingScope(null);
      return;
    }
    try {
	  const result = await api.getWorkloadApplicationCredentials(application.application_ref,
		organizationRole === 'platform_admin' ? application.org_id : undefined, 1, 100) as { data?: AccessCredentialFamily[]; has_more?: boolean };
	  if (!isCurrentScope(scope) || accessGeneration.current !== generation) return;
      setCredentials(result.data || []);
	  setCredentialHasMore(!!result.has_more);
    } catch {
	  if (!isCurrentScope(scope) || accessGeneration.current !== generation) return;
      setCredentials([]);
      setCredentialError('凭据元数据暂时不可用；不会把错误显示成“未配置”。');
	} finally {
	  if (isCurrentScope(scope) && accessGeneration.current === generation) setAccessLoadingScope(null);
    }
	}, [applicationScope, customerContextReady, isCurrentScope, organizationRole, selectedApplication]);

  const loadLegacy = useCallback(async () => {
    if (!maySeeLegacy || !customerContextReady) return;
    setLegacyLoading(true);
    setLegacyError('');
    try {
      const result = await accessApi.previewLegacy(organizationRole === 'platform_admin' ? customerOrgID : undefined);
      setLegacy(result.data);
    } catch {
      setLegacy([]);
      setLegacyError('兼容分类暂时不可用；不会回退为前端猜测。');
    } finally {
      setLegacyLoading(false);
    }
  }, [customerContextReady, customerOrgID, maySeeLegacy, organizationRole]);

  useEffect(() => { loadProjectsAndContract(); }, [loadProjectsAndContract]);
  useEffect(() => { loadApplications(); }, [loadApplications]);
  useEffect(() => { loadApplicationAccess(); }, [loadApplicationAccess]);
  useEffect(() => { if (activeTab === 'legacy') loadLegacy(); }, [activeTab, loadLegacy]);

  useEffect(() => {
    if (!drawer || !formApi) return;
    formApi.reset();
    if (drawer === 'edit' && selectedApplication) {
      formApi.setValues({
        display_name: selectedApplication.display_name,
        environment: selectedApplication.environment,
        production_use: selectedApplication.production_use,
        client_profiles: selectedApplication.client_profiles,
        primary_client_profile: selectedApplication.client_profile,
        owner_team: selectedApplication.owner_team,
        description: selectedApplication.description,
        status: selectedApplication.status,
      });
    }
  }, [drawer, formApi, selectedApplication]);

	const openCreateDrawer = () => {
		if (!selectedProjectID) return;
		setDrawerScope({ projectID: selectedProjectID });
		setDrawer('create');
	};
	const openApplicationDrawer = (kind: 'edit' | 'request' | 'credential', application: WorkloadApplication) => {
		const scope = applicationScope(application);
		if (!isCurrentScope(scope)) {
		Toast.warning(t("应用范围已切换，请重新选择。"));
			return;
		}
		setDrawerScope(scope);
		setDrawer(kind);
	};
	const openRequestDrawer = async (application: WorkloadApplication) => {
		const scope = applicationScope(application);
		if (!isCurrentScope(scope)) {
			Toast.warning(t("应用范围已切换，请重新选择。"));
			return;
		}
		setDrawerScope(scope);
		setDrawer('request');
		setSponsorOptions([]);
		setSponsorError('');
		setSponsorLoading(true);
		const generation = ++sponsorGeneration.current;
		try {
			const members = await loadAllPages<ProjectMember>((page, size) => api.getProjectMembers(application.project_id, page, size));
			if (!isCurrentScope(scope) || sponsorGeneration.current !== generation) return;
			setSponsorOptions(members
				.filter((member) => member.user_id > 0 && member.status === 'active')
				.map((member) => ({ value: member.user_id, label: `${member.username} · ${member.membership_role}` })));
		} catch {
			if (isCurrentScope(scope) && sponsorGeneration.current === generation) setSponsorError('无法读取当前项目的有效成员；未提交应用访问申请。');
		} finally {
			if (isCurrentScope(scope) && sponsorGeneration.current === generation) setSponsorLoading(false);
		}
	};
	const closeDrawer = () => {
		sponsorGeneration.current++;
		setDrawer(null); setDrawerScope(null); setSponsorOptions([]); setSponsorError(''); setSponsorLoading(false);
	};
	useEffect(() => {
		if (!drawer || !drawerScope || isCurrentScope(drawerScope)) return;
		sponsorGeneration.current++;
		setDrawer(null); setDrawerScope(null); setSponsorOptions([]); setSponsorError(''); setSponsorLoading(false);
	}, [drawer, drawerScope, isCurrentScope, selectedApplicationRef, selectedProjectID]);

  const submitApplication = async (values: Record<string, any>) => {
    const project = projects.find((item) => item.id === selectedProjectID);
	const application = selectedApplication;
	const scope = drawerScope;
	if (!project || !scope || !isCurrentScope(scope) || (drawer === 'edit' && (!application || application.id !== scope.applicationID))) {
		Toast.warning(t("应用范围已切换；未提交旧范围修改。"));
		return;
	}
    setMutating(true);
    try {
	  if (drawer === 'edit' && application) {
		await api.updateWorkloadApplication(application.application_ref, {
		  org_id: application.org_id,
		  expected_revision: application.revision,
		  display_name: values.display_name,
		  environment: application.environment,
          client_profiles: values.client_profiles || [],
          primary_client_profile: values.primary_client_profile || '',
          owner_team: values.owner_team || '', description: values.description || '', status: values.status,
        });
		if (isCurrentScope(scope)) Toast.success(t("应用信息已更新"));
      } else {
        const created = await api.createWorkloadApplication({
          org_id: project.org_id, project_id: project.id,
          display_name: values.display_name, kind: values.kind,
		  environment: values.environment, production_use: values.environment === 'production',
          identity_mode: 'service_credential', client_profiles: values.client_profiles || [],
          primary_client_profile: values.primary_client_profile || '',
          owner_team: values.owner_team || '', description: values.description || '', status: 'active',
        }) as WorkloadApplication;
		if (isCurrentScope(scope)) {
			updateSearch({ application_ref: created.application_ref });
			Toast.success(t("应用已创建"));
		}
      }
	  if (isCurrentScope(scope)) {
		closeDrawer();
		await loadApplications();
	  }
    } catch (error) {
	  if (isCurrentScope(scope)) {
		Toast.error(error instanceof ApiError && error.status === 409 ? t("应用已变化，请刷新后重试。") : t("应用保存失败"));
		if (error instanceof ApiError && error.status === 409) await loadApplications();
	  }
    } finally {
      setMutating(false);
    }
  };

  const requestAccess = async (sponsorUserID: number, expectedScope: ApplicationScope | null) => {
	const application = selectedApplication;
	if (!application || !user || sponsorUserID <= 0 || !expectedScope ||
		application.id !== expectedScope.applicationID || application.application_ref !== expectedScope.applicationRef ||
		application.project_id !== expectedScope.projectID || !isCurrentScope(expectedScope)) {
		Toast.warning(t("应用范围已切换；未提交旧范围请求。"));
		return;
	}
	const scope = expectedScope;
	const intent = `workload-request:${application.id}:${application.revision}:${sponsorUserID}`;
    setMutating(true);
    try {
      const result = await accessApi.createRequest({
		org_id: application.org_id, project_id: application.project_id,
		subject: { type: 'workload_application', workload_application_id: application.id },
        request_kind: 'workload_service',
        requested_scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
		environment: application.environment, production_use: application.production_use,
		submission_mode: 'delegated', sponsor_user_id: sponsorUserID,
      }, intents.get(intent));
      intents.complete(intent);
	  if (isCurrentScope(scope)) {
		Toast.success(result.request.status === 'approved' ? t("应用访问已批准") : t("应用访问已进入审核"));
		await loadApplicationAccess();
	  }
    } catch (error) {
      intents.settleError(intent, error);
	  if (isCurrentScope(scope)) {
		Toast.error(error instanceof ApiError && error.status === 409 ? t("应用访问状态已变化，已刷新。") : t("无法提交应用访问申请"));
		if (error instanceof ApiError && error.status === 409) await loadApplicationAccess();
	  }
	} finally { setMutating(false); if (isCurrentScope(scope)) closeDrawer(); }
  };

  const issueCredential = async (values: { name: string; allowed_ips?: string; allowed_models?: string }) => {
	const application = selectedApplication;
	const grant = activeGrant;
	const scope = drawerScope;
	if (!application || !grant || !scope || !isCurrentScope(scope) || application.id !== scope.applicationID) {
		Toast.warning(t("应用或有效授权已切换；未签发 Key。"));
		return;
	}
	const allowedModels = parseSortedModelList(values.allowed_models);
	const intent = `workload-issue:${grant.grant_ref}:${values.name.trim()}:${(values.allowed_ips || '').trim()}:${allowedModels.join(',')}`;
    setMutating(true);
    try {
	  const issued = await accessApi.issueCredential(grant.grant_ref, {
        name: values.name.trim(), allowed_ips: (values.allowed_ips || '').trim(), allowed_models: allowedModels, allowed_endpoints: [],
      }, intents.get(intent));
      intents.complete(intent);
	  if (isCurrentScope(scope)) {
		setSecret(issued); closeDrawer();
		await loadApplications();
	  }
    } catch (error) {
      intents.settleError(intent, error);
	  if (isCurrentScope(scope)) {
		Toast.error(error instanceof ApiError && error.status === 409 ? t("Key 状态已变化，请刷新。") : t("应用 Key 签发失败"));
		if (error instanceof ApiError && error.status === 409) await loadApplications();
	  }
    } finally { setMutating(false); }
  };

  const rotateCredential = async (family: AccessCredentialFamily) => {
	const application = selectedApplication;
	const scope = application && applicationScope(application);
	const familyGrant = application && visibleGrants.find((grant) => grant.grant_ref === family.grant_ref && grantBelongsToApplication(grant, application));
	if (!application || !scope || !isCurrentScope(scope) || !familyGrant || !isGrantCurrentlyActive(familyGrant)) {
		Toast.warning(t("此 Key 的授权当前不可轮换；请刷新后重试。"));
		return;
	}
    const predecessor = family.versions.find((version) => version.status === 'active');
    if (!predecessor) {
      Toast.error(t("当前应用 Key 没有可轮换的有效版本"));
      return;
    }
	const intent = `workload-rotate:${familyGrant.grant_ref}:${family.credential_ref}:${predecessor.id}`;
    setMutating(true);
    try {
	  const result = await accessApi.rotateCredential(familyGrant.grant_ref, predecessor.id, intents.get(intent));
      intents.complete(intent);
	  if (isCurrentScope(scope)) {
		setSecret(result.credential);
		await loadApplications();
	  }
    } catch (error) {
      intents.settleError(intent, error);
	  if (isCurrentScope(scope)) {
		Toast.error(error instanceof ApiError && error.status === 409 ? t("Key 版本已变化，已刷新。") : t("应用 Key 轮换失败"));
		if (error instanceof ApiError && error.status === 409) await loadApplications();
	  }
    } finally { setMutating(false); }
  };

  const revokeCredential = (family: AccessCredentialFamily) => {
	const application = selectedApplication;
	const scope = application && applicationScope(application);
	const familyGrant = application && visibleGrants.find((grant) => grant.grant_ref === family.grant_ref && grantBelongsToApplication(grant, application));
	if (!application || !scope || !isCurrentScope(scope) || !family.grant_ref || !familyGrant) {
		Toast.warning(t("此 Key 的授权身份缺失；未执行撤销。"));
		return;
	}
    const locator = family.versions.find((version) => version.status === 'active') || family.versions[0];
	if (!locator) {
		Toast.warning(t("此 API Key 缺少可管理的有效版本；未执行撤销。"));
		return;
	}
    Modal.confirm({
      title: t("撤销应用 Key？"),
      content: t('{name} 的全部历史版本将立即失效，且不能恢复。', { name: family.name }),
      okType: 'danger',
      onOk: async () => {
		if (!isCurrentScope(scope)) {
			Toast.warning(t("应用范围已切换；未撤销旧范围 Key。"));
			return;
		}
		const intent = `workload-revoke:${family.grant_ref}:${family.credential_ref}`;
        setMutating(true);
        try {
		  await accessApi.revokeCredential(family.grant_ref, locator.id, intents.get(intent));
          intents.complete(intent);
		  if (isCurrentScope(scope)) {
			Toast.success(t("逻辑凭据已撤销"));
			await loadApplications();
		  }
        } catch (error) {
          intents.settleError(intent, error);
		  if (isCurrentScope(scope)) {
			Toast.error(error instanceof ApiError && error.status === 409 ? t("Key 状态已变化，已刷新。") : t("撤销应用 Key 失败"));
			if (error instanceof ApiError && error.status === 409) await loadApplications();
		  }
        } finally { setMutating(false); }
      },
    });
  };

  const archiveApplication = () => {
	const application = selectedApplication;
	if (!application?.can_manage) return;
	const scope = applicationScope(application);
	if (!isCurrentScope(scope)) {
		Toast.warning(t("应用范围已切换；未执行归档。"));
		return;
	}
    Modal.confirm({
      title: t("归档应用？"),
      content: t("归档是终态操作：现有授权与应用 Key 会失效，历史申请、审计与执行证据仍会保留。"),
      okType: 'danger',
      onOk: async () => {
		if (!isCurrentScope(scope)) {
			Toast.warning(t("应用范围已切换；未执行归档。"));
			return;
		}
        setMutating(true);
        try {
		  await api.archiveWorkloadApplication(application.application_ref, {
			org_id: organizationRole === 'platform_admin' ? application.org_id : undefined,
			expected_revision: application.revision,
          });
		  if (isCurrentScope(scope)) {
			Toast.success(t("应用已归档"));
			updateSearch({ application_ref: undefined });
			await loadApplications();
		  }
        } catch (error) {
		  if (isCurrentScope(scope)) {
			Toast.error(error instanceof ApiError && error.status === 409 ? t("应用已变化，请刷新后重试。") : t("应用归档失败"));
			if (error instanceof ApiError && error.status === 409) await loadApplications();
		  }
        } finally { setMutating(false); }
      },
    });
  };

  const applicationColumns = useMemo(() => [
    { title: t("应用"), render: (_: unknown, row: WorkloadApplication) => <Text strong>{row.display_name}</Text> },
    { title: t("类型与环境"), render: (_: unknown, row: WorkloadApplication) => <>{applicationKindLabel(row.kind)} · {environmentLabel(row.environment)}</> },
    { title: t("客户端兼容"), render: (_: unknown, row: WorkloadApplication) => row.client_profiles.length ? <Space wrap>{row.client_profiles.map((profile) => <Tag key={profile}>{profile}</Tag>)}</Space> : <Text type="tertiary">{t("可选，未配置")}</Text> },
    { title: t("可用 Key"), render: (_: unknown, row: WorkloadApplication) => `${row.active_credential_count}/${row.credential_count}` },
    { title: t("状态"), dataIndex: 'status', render: (value: string) => <StatusTag status={value}>{value === 'active' ? t("启用") : value === 'disabled' ? t("停用") : t("已归档")}</StatusTag> },
    { title: t("操作"), render: (_: unknown, row: WorkloadApplication) => <Button size="small" onClick={() => updateSearch({ application_ref: row.application_ref })}>{row.application_ref === selectedApplicationRef ? t("已选择") : t("查看")}</Button> },
  ], [selectedApplicationRef, updateSearch, locale]);

  const legacyColumns = useMemo(() => [
    { title: t("对象"), render: (_: unknown, row: LegacyAccessClassification) => <><Tag>{row.object_type}</Tag> <Text code>{row.object_ref}</Text></> },
    { title: t("分类"), render: (_: unknown, row: LegacyAccessClassification) => <><Text>{row.classification}</Text><br /><Text type="tertiary">{row.reason_code}</Text></> },
    { title: t("迁移状态"), dataIndex: 'migration_status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: t("客户端兼容"), render: (_: unknown, row: LegacyAccessClassification) => (row.client_profiles || []).join(', ') || '—' },
    { title: t("关联凭据"), render: (_: unknown, row: LegacyAccessClassification) => <Space wrap>{(row.linked_credentials || []).map((key) => <Tag key={key.key_id}>{key.key_prefix}</Tag>)}{!row.linked_credentials?.length && <Text type="tertiary">0</Text>}</Space> },
  ], [locale]);

	const profileOptions = (contract?.client_profiles || []).map((profile) => ({ value: profile.profile_id, label: `${profile.display_name} · ${profile.runtime_state}` }));
	const exactGrantForCredential = (family: AccessCredentialFamily) => selectedApplication && visibleGrants.find((grant) =>
		grant.grant_ref === family.grant_ref && grantBelongsToApplication(grant, selectedApplication));
	const canRotateCredential = (family: AccessCredentialFamily) => {
		const exactGrant = exactGrantForCredential(family);
		return family.effective_status === 'active' && !!exactGrant && isGrantCurrentlyActive(exactGrant);
	};
	const credentialLocator = (family: AccessCredentialFamily) => family.versions.find((version) => version.status === 'active') || family.versions[0];

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state === 'ready' ? 'unavailable' : customerContext.state);
    return (
      <div className="ur-page ur-access-workspace">
        <PageHeader eyebrow={t("访问与安全")} title={t("应用与 Agent")} description={t("应用授权和应用 Key 必须绑定这套部署唯一的客户组织。")} />
        <Surface><EmptyState title={blocker.title} description={blocker.description} minHeight={300} /></Surface>
      </div>
    );
  }

  return (
    <div className="ur-page ur-access-workspace">
      <PageHeader eyebrow={t("访问与安全")} title={t("应用与 Agent")} description={t("为服务、Agent 或工作流创建独立 API Key。人员账号请在“人员与权限”加入项目后，由本人在项目工作台创建个人 Key。")} actions={<Button icon={<IconRefresh />} onClick={() => { loadApplications(); if (selectedApplication) loadApplicationAccess(); }}>{t("刷新")}</Button>} />
      <Tabs activeKey={activeTab} onChange={(tab) => updateSearch({ tab })}>
        <TabPane tab={t("应用与 Agent")} itemKey="applications">
          <Surface title={t("项目范围")} description={t("可用操作按当前项目和你的实时项目角色决定。")}>
            {projectsLoading ? <>
              <Space>
                <Select aria-labelledby="application-project-loading-label" insetLabel={t("项目")} insetLabelId="application-project-loading-label" disabled style={{ width: 300 }} optionList={[]} placeholder={t("正在读取项目")} />
                <Button type="primary" icon={<IconPlus />} disabled>{t("新建应用")}</Button>
              </Space>
              <EmptyState title={t("正在读取项目范围")} description={t("正在读取当前账号可访问的项目和应用合同；权限确认前不会开放创建操作。")} />
            </>
              : projectsError ? <EmptyState title={t("项目范围暂时不可用")} description={translateLabel(projectsError)} actions={<Button onClick={loadProjectsAndContract}>{t("重试")}</Button>} />
                : projects.length === 0 ? <EmptyState title={t("暂无可用项目")} description={t("请先在“人员与权限”中创建或加入一个启用项目。")} />
                  : <Space wrap><Select aria-labelledby="application-project-label" insetLabel={t("项目")} insetLabelId="application-project-label" value={selectedProjectID} style={{ width: 300 }} optionList={projects.map(projectOption)} placeholder={t("选择项目")} onChange={(value) => updateSearch({ project_id: Number(value), application_ref: undefined })} /><Button type="primary" icon={<IconPlus />} disabled={!selectedProjectID || !canManageSelectedProject} onClick={openCreateDrawer}>{t("新建应用")}</Button></Space>}
            {selectedProjectID && !canManageSelectedProject && <Text type="tertiary">{t("当前所选项目只有读取或使用权限；创建和维护应用需要项目管理员权限。")}</Text>}
          </Surface>
          <div className="ur-application-layout">
          <Surface title={t("应用")} description={t("每个应用绑定一个项目和环境；内部身份标识由服务端生成并保留在高级审计信息中。")}>
            {projectsLoading || (!selectedProjectID && projects.length > 0) ? <EmptyState title={t("正在准备应用范围")} description={t("正在选择并确认当前项目，请稍候。")} />
              : !selectedProjectID ? <EmptyState title={t("没有可读取的项目应用")} description={t("应用必须绑定一个启用项目。")} />
                : applicationsError ? <EmptyState title={t("应用列表暂时不可用")} description={translateLabel(applicationsError)} actions={<Button onClick={loadApplications}>{t("重试")}</Button>} />
                  : applicationsLoading && applications.length === 0 ? <EmptyState title={t("正在读取应用")} description={t("正在读取当前项目的应用，请稍候。")} />
                    : <Table columns={applicationColumns} dataSource={applications} rowKey="id" pagination={false} loading={applicationsLoading} empty={<EmptyState title={t("当前项目暂无应用")} />} />}
          </Surface>
          <aside className="ur-application-inspector">
          {selectedApplication ? <>
            <Surface title={selectedApplication.display_name} description={`${applicationKindLabel(selectedApplication.kind)} · ${environmentLabel(selectedApplication.environment)}`} actions={<Space><Button disabled={!selectedApplication.can_manage || selectedApplication.status === 'archived'} onClick={() => openApplicationDrawer('edit', selectedApplication)}>{t("编辑")}</Button><Button disabled={!selectedApplication.can_request_access || mutating} onClick={() => organizationRole === 'platform_admin' ? openRequestDrawer(selectedApplication) : requestAccess(Number(user?.id || 0), applicationScope(selectedApplication))}>{t("申请应用访问")}</Button><Button type="danger" disabled={!selectedApplication.can_manage || selectedApplication.status === 'archived' || mutating} onClick={archiveApplication}>{t("归档")}</Button></Space>}>
              <Space wrap><Tag>{applicationKindLabel(selectedApplication.kind)}</Tag><Tag>{environmentLabel(selectedApplication.environment)}</Tag>{selectedApplication.production_use && <Tag color="red">{t("生产用途")}</Tag>}<Tag color={selectedApplication.can_manage ? 'green' : 'grey'}>{selectedApplication.can_manage ? t("可管理") : t("只读")}</Tag></Space>
			  <details style={{ marginTop: 12 }}><summary>{t("高级审计标识")}</summary><div style={{ marginTop: 8 }}><Text code>Application: {selectedApplication.application_ref}</Text><br /><Text code>Logical identity: {selectedApplication.logical_ref}</Text><br /><Text type="tertiary">{selectedApplication.identity_contract_version}</Text></div></details>
            </Surface>
            <Surface title={t("访问授权")} description={t("展示此应用自己的访问申请和当前授权；读取失败不会被误报成未申请或未授权。")}>
              <Space vertical align="start" style={{ width: '100%' }}>
				{visibleAccessLoading ? <Text type="tertiary">{t("正在读取访问状态…")}</Text> : <>
				  {visibleRequestError ? <Text type="warning">{translateLabel(visibleRequestError)}</Text> : <Text>{t("访问申请：")}{visibleAccessRequests.length ? visibleAccessRequests.map((request) => accessRequestStatusLabel(request.status)).join('；') : t("未申请")}</Text>}
				  {visibleGrantError ? <Text type="warning">{translateLabel(visibleGrantError)}</Text> : <Text>{t("当前授权：")}{visibleGrants.length ? visibleGrants.map(accessGrantStatusLabel).join('；') : t("未授权")}</Text>}
				  {(visibleAccessRequests.length > 0 || visibleGrants.length > 0) && <details><summary>{t("高级审计标识")}</summary><div style={{ marginTop: 8 }}>{visibleAccessRequests.map((request) => <div key={request.request_ref}><Text code>{request.request_ref} ({request.status})</Text></div>)}{visibleGrants.map((grant) => <div key={grant.grant_ref}><Text code>{grant.grant_ref} ({effectiveGrantStatus(grant)})</Text></div>)}</div></details>}
				</>}
                {(visibleRequestError || visibleGrantError) && <Button size="small" onClick={loadApplicationAccess}>{t("重试访问事实")}</Button>}
              </Space>
            </Surface>
          </> : <Surface><EmptyState title={t('选择一个应用')} description={t('查看应用的环境、访问申请、授权和 API Key。')} /></Surface>}
          </aside>
          </div>
          {selectedApplication && <>
            <Surface title={t("应用 API Key")} description={t("每个 Key 只属于当前应用；完整 Key 只在创建或轮换成功后显示一次。")} actions={selectedApplication.can_manage && activeGrant ? <Button type="primary" icon={<IconKeyStroked />} onClick={() => openApplicationDrawer('credential', selectedApplication)}>{t("创建应用 Key")}</Button> : undefined}>
              {!selectedApplication.can_read_credentials ? <EmptyState title={t("当前账号无权读取应用 Key")} description={t("应用列表只显示安全的汇总数量。")} /> : visibleAccessLoading ? <EmptyState title={t("正在读取应用 Key")} description={t("正在核对当前应用的授权与 Key 状态。")} /> : visibleCredentialError ? <EmptyState title={t("应用 Key 暂时不可用")} description={translateLabel(visibleCredentialError)} actions={<Button onClick={loadApplicationAccess}>{t("重试")}</Button>} /> : <><Table key={selectedApplication.id} columns={[{ title: t("Key 名称"), render: (_: unknown, row: AccessCredentialFamily) => <><Text strong>{row.name}</Text><details><summary>{t("高级审计标识")}</summary><Text code>{row.credential_ref}</Text><br /><Text type="tertiary">{row.grant_ref || t("授权身份缺失")}</Text></details></> }, { title: t("状态"), dataIndex: 'effective_status', render: (value: string) => credentialStatusLabel(value) }, { title: t("版本"), render: (_: unknown, row: AccessCredentialFamily) => <>{row.versions.map((version) => `${version.key_prefix}（${credentialStatusLabel(version.status)}）`).join('；')}{row.versions_truncated && <Text type="tertiary">{t("；仅显示最近版本")}</Text>}</> }, { title: t("操作"), render: (_: unknown, row: AccessCredentialFamily) => selectedApplication.can_manage ? <Space><Button size="small" disabled={mutating || !canRotateCredential(row)} onClick={() => rotateCredential(row)}>{t("安全轮换")}</Button><Button size="small" type="danger" disabled={mutating || row.status !== 'active' || !row.grant_ref || !exactGrantForCredential(row) || !credentialLocator(row)} onClick={() => revokeCredential(row)}>{t("撤销")}</Button></Space> : <Text type="tertiary">{t("只读")}</Text> }]} dataSource={visibleCredentials} rowKey="id" pagination={false} empty={<EmptyState title={t("尚未创建应用 Key")} />} />{visibleCredentialHasMore && <Text type="warning">{t("当前只展示前 100 个应用 Key；请收窄管理范围后继续查看。")}</Text>}</>}
            </Surface>
          </>}
        </TabPane>
        <TabPane tab={t("旧版兼容（只读）")} itemKey="legacy">
          <Surface title={t("旧版对象分类")} description={t("只展示服务端已确认的兼容信息；不自动迁移、不推断、不提供写操作。")}>
            {!maySeeLegacy ? <EmptyState title={t("仅组织或平台管理员可查看兼容清单")} /> : legacyError ? <EmptyState title={t("兼容分类暂时不可用")} description={translateLabel(legacyError)} actions={<Button onClick={loadLegacy}>{t("重试")}</Button>} /> : <Table columns={legacyColumns} dataSource={legacy} rowKey={(row?: LegacyAccessClassification) => row ? `${row.object_type}:${row.object_id}` : ''} pagination={false} loading={legacyLoading} empty={<EmptyState title={t("没有需要展示的旧版对象")} />} />}
          </Surface>
        </TabPane>
      </Tabs>

      <SideSheet title={drawer === 'edit' ? t("编辑应用") : t("新建应用")} visible={drawer === 'create' || drawer === 'edit'} onCancel={closeDrawer} width="min(540px, 100vw)">
        <Form getFormApi={setFormApi} onSubmit={submitApplication}>
          <Form.Input field="display_name" label={t("显示名称")} rules={[{ required: true, message: t("必填") }]} />
          {drawer === 'create' && <Form.Select field="kind" label={<span id="application-kind-label">{t("应用类型")}</span>} aria-labelledby="application-kind-label" initValue="service" optionList={localizeOptions(applicationKindOptions)} rules={[{ required: true, message: t("必填") }]} style={{ width: '100%' }} />}
          {drawer === 'create' ? <Form.Select field="environment" label={<span id="application-environment-label">{t("环境（创建后不可变）")}</span>} aria-labelledby="application-environment-label" initValue="development" optionList={[{ value: 'development', label: t("开发") }, { value: 'staging', label: t("测试") }, { value: 'production', label: t("生产") }]} rules={[{ required: true, message: t("必填") }]} style={{ width: '100%' }} /> : <Form.Input field="environment" label={t("环境（创建后不可变）")} disabled />}
          <details><summary>{t("高级配置（可选）")}</summary><div style={{ marginTop: 12 }}>
			<Form.Select field="client_profiles" label={<span id="application-client-profiles-label">{t("客户端兼容配置")}</span>} aria-labelledby="application-client-profiles-label" multiple optionList={profileOptions} style={{ width: '100%' }} />
			<Form.Select field="primary_client_profile" label={<span id="application-primary-client-profile-label">{t("主要客户端配置")}</span>} aria-labelledby="application-primary-client-profile-label" optionList={profileOptions} style={{ width: '100%' }} />
			<Form.Input field="owner_team" label={t("负责团队")} />
			<Form.TextArea field="description" label={t("说明")} />
		  </div></details>
          {drawer === 'edit' && <Form.Select field="status" label={<span id="application-status-label">{t("状态")}</span>} aria-labelledby="application-status-label" optionList={[{ value: 'active', label: t("启用") }, { value: 'disabled', label: t("停用") }]} rules={[{ required: true, message: t("必填") }]} style={{ width: '100%' }} />}
          <div style={{ textAlign: 'right', marginTop: 20 }}><Button onClick={closeDrawer}>{t("取消")}</Button><Button type="primary" htmlType="submit" loading={mutating} style={{ marginLeft: 8 }}>{t("保存")}</Button></div>
        </Form>
      </SideSheet>

      <Modal title={t("创建应用 API Key")} visible={drawer === 'credential'} onCancel={closeDrawer} footer={null}>
        <Form getFormApi={setFormApi} onSubmit={issueCredential}>
          <Form.Input field="name" label={t("Key 名称")} rules={[{ required: true, message: t("必填") }]} />
          <Form.Input field="allowed_models" label={t("允许模型（可选）")} placeholder={t("留空继承项目模型范围；多个模型用逗号分隔")} />
          <Text type="tertiary" size="small">{t("填写精确模型或别名只会收窄此 Key 的范围，不会扩大应用 Grant 或项目权限。")}</Text>
          <Form.Input field="allowed_ips" label={t("允许的 IP / CIDR（可选）")} />
          <div style={{ textAlign: 'right' }}><Button onClick={closeDrawer}>{t("取消")}</Button><Button htmlType="submit" type="primary" loading={mutating} style={{ marginLeft: 8 }}>{t("创建")}</Button></div>
        </Form>
      </Modal>

	  <Modal title={t("选择应用访问 Sponsor")} visible={drawer === 'request'} onCancel={closeDrawer} footer={null}>
		<Form getFormApi={setFormApi} onSubmit={(values) => requestAccess(Number(values.sponsor_user_id || 0), drawerScope)}>
		  <Text type="tertiary">{t("Sponsor 必须是当前客户、当前项目中的有效业务成员；平台管理员身份不能替代业务主体。")}</Text>
		  {sponsorError ? <Text type="danger">{translateLabel(sponsorError)}</Text> : <Form.Select field="sponsor_user_id" label={t("业务 Sponsor")} aria-label={t("业务 Sponsor")} optionList={sponsorOptions} loading={sponsorLoading} disabled={sponsorLoading || sponsorOptions.length === 0} rules={[{ required: true, message: t("请选择业务 Sponsor") }]} style={{ width: '100%', marginTop: 12 }} />}
		  <div style={{ textAlign: 'right', marginTop: 20 }}><Button onClick={closeDrawer}>{t("取消")}</Button><Button htmlType="submit" type="primary" disabled={sponsorLoading || !!sponsorError || sponsorOptions.length === 0} loading={mutating} style={{ marginLeft: 8 }}>{t("提交申请")}</Button></div>
		</Form>
	  </Modal>

      <Modal title={t("一次性应用 API Key")} visible={!!secret} onOk={() => setSecret(null)} onCancel={() => setSecret(null)} okText={t("我已安全保存")} cancelButtonProps={{ style: { display: 'none' } }} maskClosable={false} closeOnEsc={false}>
        {secret?.secret_available && secret.key ? <><Text type="warning">{t("关闭后系统不会保留或再次显示完整 Key。")}</Text><div style={{ marginTop: 12, wordBreak: 'break-all' }}><Text code copyable>{secret.key}</Text></div></> : <Text type="warning">{t("这次请求是之前成功操作的安全重放，原 Key 无法再次显示。请安全轮换当前有效版本以获得新 Key。")}</Text>}
      </Modal>
    </div>
  );
}
