import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Modal, Space, Toast, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, parseExactTokenCount, Surface } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import { loadAllPages } from '../utils/loadAllPages';
import RuntimeChangeControlWorkspace from './RuntimeChangeControlWorkspace';
import InvocationOptimizationWorkspace from './InvocationOptimizationWorkspace';
import ModelConnectionBackButton from '../components/ModelConnectionBackButton';
import ModelGovernanceAssignment from '../components/ModelGovernanceAssignment';
import ModelGovernanceSheets from '../components/ModelGovernanceSheets';
import { ModelAliasPanel, ModelRegistryPanel, ModelSetPanel } from '../components/ModelGovernanceCatalog';
import {
  comparableModelIdentity,
  formatModelSetItems,
  isApproved,
  parseModelSetItems,
  slugify,
  toList,
  toNumber,
  type ModelAlias,
  type ModelRegistryItem,
  type ModelSet,
  type ModelSetItem,
  type Project,
  type TabKey,
} from '../utils/modelGovernance';
export { comparableModelIdentity, formatModelSetItems, parseModelSetItems } from '../utils/modelGovernance';

const { Text, Title } = Typography;

export default function ModelGovernancePage({ embedded = false }: { embedded?: boolean } = {}) {
  const user = useAuthStore((state) => state.user);
  const platform = isPlatformAdminRole(organizationRoleForUser(user));
  const customerContext = resolveCustomerContext(user);
  const requestedTab = new URLSearchParams(window.location.search).get('tab');
  const [activeTab, setActiveTab] = useState<TabKey>(
    ['registry', 'aliases', 'sets', 'assignments', 'optimization', ...(!embedded && platform ? ['releases'] : [])].includes(requestedTab || '')
      ? requestedTab as TabKey
      : 'registry',
  );

  const [registry, setRegistry] = useState<ModelRegistryItem[]>([]);
  const [registryTotal, setRegistryTotal] = useState(0);
  const [registryPage, setRegistryPage] = useState(1);
  const [registryPageSize, setRegistryPageSize] = useState(20);
  const [registryLoading, setRegistryLoading] = useState(false);
  const [registryLoadError, setRegistryLoadError] = useState('');
  const [registryLifecycle, setRegistryLifecycle] = useState('');
  const [registrySearch, setRegistrySearch] = useState('');
  const [registryApprovedOnly, setRegistryApprovedOnly] = useState(false);
  const [registrySheet, setRegistrySheet] = useState<{ mode: 'create' | 'edit'; record?: ModelRegistryItem } | null>(null);
  const [registryFormApi, setRegistryFormApi] = useState<any>(null);

  const [aliases, setAliases] = useState<ModelAlias[]>([]);
  const [aliasTotal, setAliasTotal] = useState(0);
  const [aliasPage, setAliasPage] = useState(1);
  const [aliasPageSize, setAliasPageSize] = useState(20);
  const [aliasLoading, setAliasLoading] = useState(false);
  const [aliasLoadError, setAliasLoadError] = useState('');
  const [aliasSheet, setAliasSheet] = useState<{ mode: 'create' | 'edit'; record?: ModelAlias } | null>(null);
  const [aliasFormApi, setAliasFormApi] = useState<any>(null);

  const [modelSets, setModelSets] = useState<ModelSet[]>([]);
  const [modelSetTotal, setModelSetTotal] = useState(0);
  const [modelSetPage, setModelSetPage] = useState(1);
  const [modelSetPageSize, setModelSetPageSize] = useState(20);
  const [modelSetLoading, setModelSetLoading] = useState(false);
  const [modelSetLoadError, setModelSetLoadError] = useState('');
  const [modelSetSheet, setModelSetSheet] = useState(false);
  const [modelSetFormApi, setModelSetFormApi] = useState<any>(null);
  const [itemsSheet, setItemsSheet] = useState<ModelSet | null>(null);
  const [itemsText, setItemsText] = useState('');
  const [itemsLoading, setItemsLoading] = useState(false);

  const [projects, setProjects] = useState<Project[]>([]);
  const [assignmentModelSets, setAssignmentModelSets] = useState<ModelSet[]>([]);
  const [selectedProjectId, setSelectedProjectId] = useState<number>();
  const [assignedIds, setAssignedIds] = useState<number[]>([]);
  const [assignmentLoading, setAssignmentLoading] = useState(false);
  const [assignmentBaseError, setAssignmentBaseError] = useState('');
  const [assignmentLoadError, setAssignmentLoadError] = useState('');
  const [loadedAssignmentProjectId, setLoadedAssignmentProjectId] = useState<number>();
  const registryRequestVersion = useRef(0);
  const aliasRequestVersion = useRef(0);
  const modelSetRequestVersion = useRef(0);
  const assignmentBaseRequestVersion = useRef(0);
  const assignmentRequestVersion = useRef(0);

  const tabs = [
    { key: 'registry' as const, label: '模型目录' },
    { key: 'aliases' as const, label: '企业别名' },
    { key: 'sets' as const, label: '模型集' },
    { key: 'assignments' as const, label: '项目分配' },
    { key: 'optimization' as const, label: '调用优化' },
    ...(!embedded && platform ? [{ key: 'releases' as const, label: '运行时发布' }] : []),
  ];
  const targetOrgID = platform ? effectiveCustomerOrgID(user) : undefined;
  const customerContextReady = !platform || customerContext.state === 'ready';

  const loadRegistry = useCallback(async (page = registryPage, pageSize = registryPageSize) => {
    const version = ++registryRequestVersion.current;
    setRegistryLoading(true);
    setRegistryLoadError('');
    setRegistry([]);
    setRegistryTotal(0);
    try {
      const data = await api.getModelRegistry(page, pageSize, {
        lifecycle: registryLifecycle,
        approved_only: registryApprovedOnly,
        q: registrySearch,
        ...(targetOrgID ? { org_id: targetOrgID } : {}),
      });
      if (version !== registryRequestVersion.current) return;
      setRegistry((data?.data || []) as ModelRegistryItem[]);
      setRegistryTotal(Number(data?.total || 0));
    } catch {
      if (version !== registryRequestVersion.current) return;
      setRegistryLoadError('无法加载当前组织与筛选条件下的模型目录。');
      Toast.error('模型目录加载失败');
    } finally {
      if (version === registryRequestVersion.current) setRegistryLoading(false);
    }
  }, [registryApprovedOnly, registryLifecycle, registryPage, registryPageSize, registrySearch, targetOrgID]);

  const loadAliases = useCallback(async (page = aliasPage, pageSize = aliasPageSize) => {
    const version = ++aliasRequestVersion.current;
    setAliasLoading(true);
    setAliasLoadError('');
    setAliases([]);
    setAliasTotal(0);
    try {
      const data = targetOrgID
        ? await api.getModelAliases(page, pageSize, { org_id: targetOrgID })
        : await api.getModelAliases(page, pageSize);
      if (version !== aliasRequestVersion.current) return;
      setAliases((data?.data || []) as ModelAlias[]);
      setAliasTotal(Number(data?.total || 0));
    } catch {
      if (version !== aliasRequestVersion.current) return;
      setAliasLoadError('无法加载当前组织的企业别名。');
      Toast.error('别名加载失败');
    } finally {
      if (version === aliasRequestVersion.current) setAliasLoading(false);
    }
  }, [aliasPage, aliasPageSize, targetOrgID]);

  const loadModelSets = useCallback(async (page = modelSetPage, pageSize = modelSetPageSize) => {
    const version = ++modelSetRequestVersion.current;
    setModelSetLoading(true);
    setModelSetLoadError('');
    setModelSets([]);
    setModelSetTotal(0);
    try {
      const data = targetOrgID
        ? await api.getModelSets(page, pageSize, { org_id: targetOrgID })
        : await api.getModelSets(page, pageSize);
      if (version !== modelSetRequestVersion.current) return;
      setModelSets((data?.data || []) as ModelSet[]);
      setModelSetTotal(Number(data?.total || 0));
    } catch {
      if (version !== modelSetRequestVersion.current) return;
      setModelSetLoadError('无法加载当前组织的模型集。');
      Toast.error('模型集加载失败');
    } finally {
      if (version === modelSetRequestVersion.current) setModelSetLoading(false);
    }
  }, [modelSetPage, modelSetPageSize, targetOrgID]);

  const loadAssignmentBase = useCallback(async () => {
    const version = ++assignmentBaseRequestVersion.current;
    assignmentRequestVersion.current += 1;
    setAssignmentLoading(true);
    setAssignmentBaseError('');
    setAssignmentLoadError('');
    setProjects([]);
    setAssignmentModelSets([]);
    setSelectedProjectId(undefined);
    setAssignedIds([]);
    setLoadedAssignmentProjectId(undefined);
    try {
      const [projectData, setData] = await Promise.all([
        loadAllPages<Project>((page, size) => api.getProjects(page, size, {
          status: 'active',
          ...(targetOrgID ? { org_id: targetOrgID } : {}),
        })),
        loadAllPages<ModelSet>((page, size) => targetOrgID
          ? api.getModelSets(page, size, { org_id: targetOrgID })
          : api.getModelSets(page, size)),
      ]);
      if (version !== assignmentBaseRequestVersion.current) return;
      const projectItems = projectData as Project[];
      const setItems = setData as ModelSet[];
      setProjects(projectItems);
      setAssignmentModelSets(setItems);
      setSelectedProjectId(current => {
        if (current && projectItems.some(project => project.id === current)) return current;
        return projectItems[0]?.id;
      });
    } catch {
      if (version !== assignmentBaseRequestVersion.current) return;
      setAssignmentBaseError('无法加载项目与可用模型集。');
      Toast.error('项目模型集加载失败');
    } finally {
      if (version === assignmentBaseRequestVersion.current) setAssignmentLoading(false);
    }
  }, [targetOrgID]);

  const loadProjectAssignment = useCallback(async (projectId: number) => {
    const version = ++assignmentRequestVersion.current;
    setAssignmentLoading(true);
    setAssignmentLoadError('');
    setAssignedIds([]);
    setLoadedAssignmentProjectId(undefined);
    try {
      const data = await api.getProjectModelSets(projectId);
      if (version !== assignmentRequestVersion.current) return;
      const items = (data?.data || []) as ModelSet[];
      setAssignedIds(items.map(item => item.id));
      setLoadedAssignmentProjectId(projectId);
    } catch {
      if (version !== assignmentRequestVersion.current) return;
      setAssignmentLoadError('无法加载所选项目的模型集分配，保存已禁用。');
      Toast.error('项目分配加载失败');
    } finally {
      if (version === assignmentRequestVersion.current) setAssignmentLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!customerContextReady) return;
    if (activeTab === 'registry') loadRegistry();
    if (activeTab === 'aliases') loadAliases();
    if (activeTab === 'sets') loadModelSets();
    if (activeTab === 'assignments') loadAssignmentBase();
  }, [activeTab, customerContextReady, loadAliases, loadAssignmentBase, loadModelSets, loadRegistry]);

  useEffect(() => {
    if (activeTab === 'assignments' && selectedProjectId) {
      loadProjectAssignment(selectedProjectId);
    }
  }, [activeTab, loadProjectAssignment, selectedProjectId]);

  useEffect(() => {
    if (!registrySheet || !registryFormApi) return;
    const record = registrySheet.record;
    setTimeout(() => {
      registryFormApi.setValues(record ? {
        model_name: record.model_name,
        display_name: record.display_name || record.model_name,
        provider_family: record.provider_family || '',
        lifecycle: record.lifecycle || 'staging',
        context_length: record.context_length || 0,
        output_limit: record.output_limit || 0,
        capability_tags: toList(record.capability_tags).join(','),
        data_residency: record.data_residency || '',
        pricing_model_name: record.pricing_model_name || record.model_name,
        approved_catalog: String(Boolean(record.approved_catalog)),
        migration_recommendation: record.migration_recommendation || '',
      } : {
        lifecycle: 'staging',
        context_length: 0,
        output_limit: 0,
        approved_catalog: 'false',
      });
    }, 0);
  }, [registryFormApi, registrySheet]);

  useEffect(() => {
    if (!aliasSheet || !aliasFormApi) return;
    const record = aliasSheet.record;
    setTimeout(() => {
      aliasFormApi.setValues(record ? {
        alias: record.alias,
        target_model: record.target_model,
        display_name: record.display_name || '',
        description: record.description || '',
        status: record.status || 'active',
      } : {
        status: 'active',
      });
    }, 0);
  }, [aliasFormApi, aliasSheet]);

  const registryStats = useMemo(() => {
    const ga = registry.filter(item => item.lifecycle === 'ga').length;
    const deprecated = registry.filter(item => item.lifecycle === 'deprecated').length;
    const retired = registry.filter(item => item.lifecycle === 'retired').length;
    return { ga, deprecated, retired };
  }, [registry]);
	const duplicateRegistryGroups = useMemo(() => {
		const groups = new Map<string, ModelRegistryItem[]>();
		for (const item of registry) {
			if (item.lifecycle === 'retired') continue;
			const key = comparableModelIdentity(item.model_name);
			if (!key) continue;
			const entries = groups.get(key) || [];
			entries.push(item);
			groups.set(key, entries);
		}
		return [...groups.values()].filter(items => items.length > 1);
	}, [registry]);

  const openCreateRegistry = () => {
    setRegistrySheet({ mode: 'create' });
    setTimeout(() => registryFormApi?.reset(), 0);
  };

  const openEditRegistry = (record: ModelRegistryItem) => {
    setRegistrySheet({ mode: 'edit', record });
  };

  const closeRegistrySheet = () => {
    setRegistrySheet(null);
    registryFormApi?.reset();
  };

	const retireRegistry = (record: ModelRegistryItem) => {
		Modal.confirm({
			title: `退役 ${record.model_name}？`,
			content: '退役会使这个精确模型 ID 不再可调用，并把它移出项目目录；历史发布、审计证据和供应商连接仍会保留。',
			onOk: async () => {
				try {
					await api.updateModelRegistry(record.id, { lifecycle: 'retired', approved_catalog: false });
					Toast.success('模型记录已退役并移出目录');
					loadRegistry();
				} catch (error: any) {
					Toast.error(`退役失败${error?.message ? `：${error.message}` : ''}`);
				}
			},
		});
	};

  const handleRegistrySubmit = async (values: Record<string, any>) => {
    const parsedContextLength = parseExactTokenCount(values.context_length);
    if (!parsedContextLength.ok) {
      Toast.error('模型上下文窗口上限必须是 0..2147483647 的整数 Token、十进制整数 K/M，或留空');
      return;
    }
    const payload = {
      model_name: String(values.model_name || '').trim(),
      display_name: String(values.display_name || values.model_name || '').trim(),
      provider_family: String(values.provider_family || '').trim(),
      lifecycle: String(values.lifecycle || 'staging'),
      context_length: parsedContextLength.value,
      output_limit: toNumber(values.output_limit),
      capability_tags: String(values.capability_tags || '').trim(),
      data_residency: String(values.data_residency || '').trim(),
      pricing_model_name: String(values.pricing_model_name || values.model_name || '').trim(),
      approved_catalog: isApproved(values.approved_catalog),
      migration_recommendation: String(values.migration_recommendation || '').trim(),
      ...(targetOrgID ? { org_id: targetOrgID } : {}),
    };
    try {
      if (registrySheet?.mode === 'edit' && registrySheet.record) {
        if (targetOrgID) await api.updateModelRegistry(registrySheet.record.id, payload, targetOrgID);
        else await api.updateModelRegistry(registrySheet.record.id, payload);
        Toast.success('模型目录已更新');
      } else {
        await api.createModelRegistry(payload);
        Toast.success('模型已加入目录');
      }
      closeRegistrySheet();
      setRegistryPage(1);
      loadRegistry(1, registryPageSize);
    } catch (err: any) {
      Toast.error(`保存失败${err?.message ? `：${err.message}` : ''}`);
    }
  };

  const openCreateAlias = () => {
    setAliasSheet({ mode: 'create' });
    setTimeout(() => aliasFormApi?.reset(), 0);
  };

  const openEditAlias = (record: ModelAlias) => {
    setAliasSheet({ mode: 'edit', record });
  };

  const closeAliasSheet = () => {
    setAliasSheet(null);
    aliasFormApi?.reset();
  };

  const handleAliasSubmit = async (values: Record<string, any>) => {
    const payload = {
      alias: String(values.alias || '').trim(),
      target_model: String(values.target_model || '').trim(),
      display_name: String(values.display_name || '').trim(),
      description: String(values.description || '').trim(),
      status: String(values.status || 'active'),
      ...(targetOrgID ? { org_id: targetOrgID } : {}),
    };
    try {
      if (aliasSheet?.mode === 'edit' && aliasSheet.record) {
        if (targetOrgID) await api.updateModelAlias(aliasSheet.record.id, payload, targetOrgID);
        else await api.updateModelAlias(aliasSheet.record.id, payload);
        Toast.success('别名已更新');
      } else {
        await api.createModelAlias(payload);
        Toast.success('别名已创建');
      }
      closeAliasSheet();
      loadAliases(aliasPage, aliasPageSize);
    } catch (err: any) {
      Toast.error(`保存失败${err?.message ? `：${err.message}` : ''}`);
    }
  };

  const openCreateModelSet = () => {
    setModelSetSheet(true);
    setTimeout(() => {
      modelSetFormApi?.reset();
      modelSetFormApi?.setValues({ status: 'active' });
    }, 0);
  };

  const closeModelSetSheet = () => {
    setModelSetSheet(false);
    modelSetFormApi?.reset();
  };

  const handleModelSetSubmit = async (values: Record<string, any>) => {
    let items: ModelSetItem[];
    try {
      items = parseModelSetItems(String(values.items || ''));
    } catch {
      Toast.error('模型集条目格式应为 model:gpt-4o 或 alias:company-chat，每行一条');
      return;
    }
    const name = String(values.name || '').trim();
    const slug = String(values.slug || slugify(name)).trim();
    try {
      await api.createModelSet({
        name,
        slug,
        description: String(values.description || '').trim(),
        status: String(values.status || 'active'),
        items,
        ...(targetOrgID ? { org_id: targetOrgID } : {}),
      });
      Toast.success('模型集已创建');
      closeModelSetSheet();
      setModelSetPage(1);
      loadModelSets(1, modelSetPageSize);
    } catch (err: any) {
      Toast.error(`创建失败${err?.message ? `：${err.message}` : ''}`);
    }
  };

  const openItemsSheet = async (record: ModelSet) => {
    setItemsSheet(record);
    setItemsText('');
    setItemsLoading(true);
    try {
      const data = targetOrgID
        ? await api.getModelSetItems(record.id, { org_id: targetOrgID })
        : await api.getModelSetItems(record.id);
      setItemsText(formatModelSetItems((data?.data || []) as ModelSetItem[]));
    } catch (err: any) {
      Toast.error(`条目加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setItemsLoading(false);
    }
  };

  const closeItemsSheet = () => {
    setItemsSheet(null);
    setItemsText('');
  };

  const handleItemsSubmit = async () => {
    if (!itemsSheet) return;
    let items: ModelSetItem[];
    try {
      items = parseModelSetItems(itemsText);
    } catch {
      Toast.error('模型集条目格式应为 model:gpt-4o 或 alias:company-chat，每行一条');
      return;
    }
    setItemsLoading(true);
    try {
      if (targetOrgID) await api.replaceModelSetItems(itemsSheet.id, { items }, targetOrgID);
      else await api.replaceModelSetItems(itemsSheet.id, { items });
      Toast.success('模型集条目已更新');
      closeItemsSheet();
      loadModelSets(modelSetPage, modelSetPageSize);
    } catch (err: any) {
      Toast.error(`更新失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setItemsLoading(false);
    }
  };

  const handleAssignmentSubmit = async () => {
    if (!selectedProjectId || loadedAssignmentProjectId !== selectedProjectId) return;
    setAssignmentLoading(true);
    try {
      await api.replaceProjectModelSets(selectedProjectId, assignedIds);
      Toast.success('项目模型集已更新');
      await loadProjectAssignment(selectedProjectId);
    } catch (err: any) {
      Toast.error(`保存失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setAssignmentLoading(false);
    }
  };

  if (!customerContextReady) {
    const blocker = customerContextBlockerCopy(customerContext.state as Exclude<typeof customerContext.state, 'ready'>);
    return (
      <div className={embedded ? 'ur-embedded-page' : 'ur-page ur-page-wide'}>
        {!embedded && <PageHeader
          eyebrow="模型接入"
          title={blocker.title}
          description="模型目录、别名、模型集和项目分配必须共享同一个可验证客户范围；系统不会把部署技术组织当作客户组织。"
          actions={<ModelConnectionBackButton />}
        />}
        <Surface>
          <EmptyState title={blocker.title} description={blocker.description} minHeight={260} />
        </Surface>
      </div>
    );
  }

  return (
    <div className={embedded ? 'ur-embedded-page' : 'ur-page ur-page-wide'}>
      {!embedded && <PageHeader
        eyebrow="模型接入"
        title="模型治理"
        description="维护企业模型目录、别名、项目范围与调用优化策略；高影响变化统一经过验证、Shadow、证据门禁、Canary 和回滚。"
        actions={(
          <Space>
            <ModelConnectionBackButton />
            {platform && <Text type="tertiary">客户范围：{customerContext.orgName || `客户 #${targetOrgID}`}</Text>}
            {!['optimization', 'releases'].includes(activeTab) && <Button icon={<IconRefresh />} onClick={() => activeTab === 'assignments' ? loadAssignmentBase() : activeTab === 'sets' ? loadModelSets() : activeTab === 'aliases' ? loadAliases() : loadRegistry()}>刷新</Button>}
          </Space>
        )}
      />}

      {embedded && <div className="ur-embedded-heading">
        <div><Title heading={5} style={{ margin: 0 }}>模型目录与项目范围</Title><Text type="tertiary">运行路由在主页面管理；这里仅维护企业目录、别名和项目可见范围。</Text></div>
      </div>}

      <div className="ur-modelgov-tabs" role="tablist" aria-label="模型治理视图">
        {tabs.map(tab => (
          <Button
            key={tab.key}
            role="tab"
            aria-selected={activeTab === tab.key}
            type={activeTab === tab.key ? 'primary' : 'tertiary'}
            theme={activeTab === tab.key ? 'solid' : 'borderless'}
            onClick={() => setActiveTab(tab.key)}
          >
            {tab.label}
          </Button>
        ))}
      </div>

      {activeTab === 'registry' && (
        <ModelRegistryPanel
          stats={registryStats}
          total={registryTotal}
          duplicates={duplicateRegistryGroups}
          search={registrySearch}
          lifecycle={registryLifecycle}
          approvedOnly={registryApprovedOnly}
          records={registry}
          loading={registryLoading}
          error={registryLoadError}
          page={registryPage}
          pageSize={registryPageSize}
          onSearchChange={(value) => {
            registryRequestVersion.current += 1;
            setRegistry([]);
            setRegistryTotal(0);
            setRegistrySearch(value);
          }}
          onLifecycleChange={(value) => {
            registryRequestVersion.current += 1;
            setRegistry([]);
            setRegistryTotal(0);
            setRegistryLifecycle(value);
          }}
          onApprovedOnlyChange={() => {
            registryRequestVersion.current += 1;
            setRegistry([]);
            setRegistryTotal(0);
            setRegistryApprovedOnly(current => !current);
          }}
          onPageChange={setRegistryPage}
          onPageSizeChange={(pageSize) => { setRegistryPageSize(pageSize); setRegistryPage(1); }}
          onReload={() => loadRegistry()}
          onCreate={openCreateRegistry}
          onEdit={openEditRegistry}
          onRetire={retireRegistry}
        />
      )}

      {activeTab === 'aliases' && (
        <ModelAliasPanel
          records={aliases}
          loading={aliasLoading}
          error={aliasLoadError}
          page={aliasPage}
          pageSize={aliasPageSize}
          total={aliasTotal}
          onReload={() => loadAliases()}
          onCreate={openCreateAlias}
          onEdit={openEditAlias}
          onPageChange={setAliasPage}
          onPageSizeChange={(pageSize) => { setAliasPageSize(pageSize); setAliasPage(1); }}
        />
      )}

      {activeTab === 'sets' && (
        <ModelSetPanel
          records={modelSets}
          loading={modelSetLoading}
          error={modelSetLoadError}
          page={modelSetPage}
          pageSize={modelSetPageSize}
          total={modelSetTotal}
          onReload={() => loadModelSets()}
          onCreate={openCreateModelSet}
          onEditItems={openItemsSheet}
          onPageChange={setModelSetPage}
          onPageSizeChange={(pageSize) => { setModelSetPageSize(pageSize); setModelSetPage(1); }}
        />
      )}

      {activeTab === 'assignments' && (
        <ModelGovernanceAssignment
          projects={projects}
          modelSets={assignmentModelSets}
          selectedProjectId={selectedProjectId}
          assignedIds={assignedIds}
          loading={assignmentLoading}
          baseError={assignmentBaseError}
          loadError={assignmentLoadError}
          loadedProjectId={loadedAssignmentProjectId}
          onReloadBase={loadAssignmentBase}
          onSelectProject={(projectId) => {
            assignmentRequestVersion.current += 1;
            setSelectedProjectId(projectId);
            setAssignedIds([]);
            setLoadedAssignmentProjectId(undefined);
            setAssignmentLoading(true);
          }}
          onChangeAssigned={setAssignedIds}
          onReloadProject={loadProjectAssignment}
          onSave={handleAssignmentSubmit}
        />
      )}

      {activeTab === 'releases' && platform && <RuntimeChangeControlWorkspace />}

      {activeTab === 'optimization' && <InvocationOptimizationWorkspace orgId={targetOrgID} />}

      <ModelGovernanceSheets
        registrySheet={registrySheet}
        registryFormApi={registryFormApi}
        setRegistryFormApi={setRegistryFormApi}
        onRegistrySubmit={handleRegistrySubmit}
        closeRegistrySheet={closeRegistrySheet}
        aliasSheet={aliasSheet}
        aliasFormApi={aliasFormApi}
        setAliasFormApi={setAliasFormApi}
        onAliasSubmit={handleAliasSubmit}
        closeAliasSheet={closeAliasSheet}
        modelSetSheet={modelSetSheet}
        modelSetFormApi={modelSetFormApi}
        setModelSetFormApi={setModelSetFormApi}
        onModelSetSubmit={handleModelSetSubmit}
        closeModelSetSheet={closeModelSetSheet}
        itemsSheet={itemsSheet}
        itemsText={itemsText}
        setItemsText={setItemsText}
        itemsLoading={itemsLoading}
        onItemsSubmit={handleItemsSubmit}
        closeItemsSheet={closeItemsSheet}
      />
    </div>
  );
}
