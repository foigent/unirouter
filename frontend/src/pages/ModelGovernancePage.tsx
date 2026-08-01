import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Form, Input, Select, SideSheet, Space, Table, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconEdit, IconPlus, IconRefresh, IconServer, IconSetting } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { isPlatformAdminRole } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { loadAllPages } from '../utils/loadAllPages';
import RuntimeChangeControlWorkspace from './RuntimeChangeControlWorkspace';

const { Text } = Typography;

const lifecycleOptions = [
  { label: 'Staging', value: 'staging' },
  { label: 'GA', value: 'ga' },
  { label: 'Deprecated', value: 'deprecated' },
  { label: 'Retired', value: 'retired' },
];

const statusOptions = [
  { label: 'Active', value: 'active' },
  { label: 'Disabled', value: 'disabled' },
];

const approvedOptions = [
  { label: '加入开发者目录', value: 'true' },
  { label: '仅内部可见', value: 'false' },
];

type TabKey = 'registry' | 'aliases' | 'sets' | 'assignments' | 'releases';

interface ModelRegistryItem {
  id: number;
  model_name: string;
  display_name?: string;
  provider_family?: string;
  lifecycle: string;
  context_length?: number;
  output_limit?: number;
  capability_tags?: string[] | string;
  data_residency?: string;
  pricing_model_name?: string;
  approved_catalog?: boolean;
  migration_recommendation?: string;
}

interface ModelAlias {
  id: number;
  alias: string;
  target_model: string;
  display_name?: string;
  description?: string;
  status: string;
}

interface ModelSet {
  id: number;
  name: string;
  slug: string;
  description?: string;
  status: string;
  item_count?: number;
}

interface Project {
  id: number;
  name: string;
  slug: string;
  status: string;
}

interface ModelSetItem {
  item_type: 'model' | 'alias';
  item_name: string;
}

interface Organization {
  id: number;
  name: string;
}

function compactNumber(value?: number) {
  const n = Number(value || 0);
  if (!n) return '-';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function toList(value?: string[] | string) {
  if (Array.isArray(value)) return value.filter(Boolean);
  return (value || '').split(',').map(item => item.trim()).filter(Boolean);
}

function toNumber(value: unknown) {
  const n = Number(value || 0);
  return Number.isFinite(n) && n >= 0 ? n : 0;
}

function isApproved(value: unknown) {
  return value === true || value === 'true' || value === '1';
}

function slugify(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
}

export function parseModelSetItems(text: string): ModelSetItem[] {
  const seen = new Set<string>();
  const items: ModelSetItem[] = [];
  text.split(/\r?\n/).forEach(raw => {
    const line = raw.trim().replace('：', ':');
    if (!line) return;
    const separator = line.indexOf(':');
    if (separator <= 0) {
      throw new Error('invalid item');
    }
    const itemType = line.slice(0, separator).trim();
    const itemName = line.slice(separator + 1).trim();
    if ((itemType !== 'model' && itemType !== 'alias') || !itemName) {
      throw new Error('invalid item');
    }
    const key = `${itemType}:${itemName}`;
    if (!seen.has(key)) {
      seen.add(key);
      items.push({ item_type: itemType, item_name: itemName });
    }
  });
  return items;
}

export function formatModelSetItems(items: ModelSetItem[]) {
  return items.map(item => `${item.item_type}:${item.item_name}`).join('\n');
}

export default function ModelGovernancePage() {
  const user = useAuthStore((state) => state.user);
  const platform = isPlatformAdminRole(user?.role);
  const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(user?.org_id);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [activeTab, setActiveTab] = useState<TabKey>('registry');

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
    ...(platform ? [{ key: 'releases' as const, label: '运行时发布' }] : []),
  ];
  const targetOrgID = platform ? selectedOrgID : undefined;

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

  useEffect(() => {
    if (!platform) return;
    if (user?.org_id) setSelectedOrgID(current => current || user.org_id);
    loadAllPages<Organization>((page, size) => api.getOrganizations(page, size))
      .then(setOrganizations)
      .catch((err: any) => Toast.error(`组织列表加载失败${err?.message ? `：${err.message}` : ''}`));
  }, [platform, user?.org_id]);

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
    if (activeTab === 'registry') loadRegistry();
    if (activeTab === 'aliases') loadAliases();
    if (activeTab === 'sets') loadModelSets();
    if (activeTab === 'assignments') loadAssignmentBase();
  }, [activeTab, loadAliases, loadAssignmentBase, loadModelSets, loadRegistry]);

  useEffect(() => {
    if (activeTab === 'assignments' && selectedProjectId) {
      loadProjectAssignment(selectedProjectId);
    }
  }, [activeTab, loadProjectAssignment, selectedProjectId]);

  const changeOrganization = (value: unknown) => {
    const orgID = Number(value);
    if (!Number.isSafeInteger(orgID) || orgID <= 0) return;
    setSelectedOrgID(orgID);
    registryRequestVersion.current += 1;
    aliasRequestVersion.current += 1;
    modelSetRequestVersion.current += 1;
    assignmentBaseRequestVersion.current += 1;
    setRegistryPage(1);
    setAliasPage(1);
    setModelSetPage(1);
    setRegistry([]);
    setRegistryTotal(0);
    setAliases([]);
    setAliasTotal(0);
    setModelSets([]);
    setModelSetTotal(0);
    setProjects([]);
    setAssignmentModelSets([]);
    setSelectedProjectId(undefined);
    setAssignedIds([]);
    setLoadedAssignmentProjectId(undefined);
    assignmentRequestVersion.current += 1;
    setRegistrySheet(null);
    setAliasSheet(null);
    setModelSetSheet(false);
    setItemsSheet(null);
    setRegistryLoadError('');
    setAliasLoadError('');
    setModelSetLoadError('');
    setAssignmentBaseError('');
    setAssignmentLoadError('');
  };

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

  const handleRegistrySubmit = async (values: Record<string, any>) => {
    const payload = {
      model_name: String(values.model_name || '').trim(),
      display_name: String(values.display_name || values.model_name || '').trim(),
      provider_family: String(values.provider_family || '').trim(),
      lifecycle: String(values.lifecycle || 'staging'),
      context_length: toNumber(values.context_length),
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

  const registryColumns = [
    {
      title: '模型',
      dataIndex: 'model_name',
      key: 'model_name',
      render: (value: string, record: ModelRegistryItem) => (
        <div className="ur-modelgov-name">
          <Text strong>{record.display_name || value}</Text>
          <Text code>{value}</Text>
          {record.migration_recommendation && <Text type="tertiary" size="small">{record.migration_recommendation}</Text>}
        </div>
      ),
    },
    { title: '供应商族', dataIndex: 'provider_family', key: 'provider_family', render: (value: string) => value || '-' },
    { title: '生命周期', dataIndex: 'lifecycle', key: 'lifecycle', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: '目录可见性', dataIndex: 'approved_catalog', key: 'approved_catalog', render: (value: boolean) => <StatusTag status={Boolean(value)}>{value ? '已批准' : '内部'}</StatusTag> },
    { title: '上下文 / 输出', key: 'limits', render: (_: unknown, record: ModelRegistryItem) => `${compactNumber(record.context_length)} / ${compactNumber(record.output_limit)}` },
    { title: '能力', dataIndex: 'capability_tags', key: 'capability_tags', render: (value: string[] | string) => toList(value).slice(0, 4).join(', ') || '-' },
    { title: '数据驻留', dataIndex: 'data_residency', key: 'data_residency', render: (value: string) => value || '-' },
    {
      title: '操作',
      key: 'actions',
      width: 86,
      render: (_: unknown, record: ModelRegistryItem) => (
        <Button icon={<IconEdit />} size="small" onClick={() => openEditRegistry(record)} aria-label="编辑模型" />
      ),
    },
  ];

  const aliasColumns = [
    { title: '别名', dataIndex: 'alias', key: 'alias', render: (value: string) => <Text code>{value}</Text> },
    { title: '目标模型', dataIndex: 'target_model', key: 'target_model' },
    { title: '名称', dataIndex: 'display_name', key: 'display_name', render: (value: string) => value || '-' },
    { title: '描述', dataIndex: 'description', key: 'description', render: (value: string) => value || '-' },
    { title: '状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    {
      title: '操作',
      key: 'actions',
      width: 86,
      render: (_: unknown, record: ModelAlias) => (
        <Button icon={<IconEdit />} size="small" onClick={() => openEditAlias(record)} aria-label="编辑别名" />
      ),
    },
  ];

  const modelSetColumns = [
    {
      title: '模型集',
      dataIndex: 'name',
      key: 'name',
      render: (value: string, record: ModelSet) => (
        <div className="ur-modelgov-name">
          <Text strong>{value}</Text>
          <Text code>{record.slug}</Text>
          {record.description && <Text type="tertiary" size="small">{record.description}</Text>}
        </div>
      ),
    },
    { title: '状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: '条目数', dataIndex: 'item_count', key: 'item_count', render: (value: number) => value || 0 },
    {
      title: '操作',
      key: 'actions',
      width: 112,
      render: (_: unknown, record: ModelSet) => (
        <Button icon={<IconSetting />} size="small" onClick={() => openItemsSheet(record)}>条目</Button>
      ),
    },
  ];

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="模型接入"
        title="模型治理"
        description="维护企业模型目录、别名、模型集与项目范围；平台管理员还可在同一工作区完成通道变更的验证、影子、灰度、提升和回滚。"
        actions={(
          <Space>
            {platform && (
              <Space spacing={6}>
                <Text type="tertiary">目标组织</Text>
                <Select
                  aria-label="目标组织"
                  placeholder="选择目标组织"
                  value={selectedOrgID}
                  optionList={organizations.map(org => ({ label: `${org.name}（ID ${org.id}）`, value: org.id }))}
                  onChange={changeOrganization}
                  disabled={registryLoading || aliasLoading || modelSetLoading || assignmentLoading}
                  style={{ width: 220 }}
                />
              </Space>
            )}
            {activeTab !== 'releases' && <Button icon={<IconRefresh />} onClick={() => activeTab === 'assignments' ? loadAssignmentBase() : activeTab === 'sets' ? loadModelSets() : activeTab === 'aliases' ? loadAliases() : loadRegistry()}>刷新</Button>}
          </Space>
        )}
      />

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
        <>
          <div className="ur-modelgov-summary">
            <div><Text type="tertiary">生产可用</Text><strong>{registryStats.ga}</strong></div>
            <div><Text type="tertiary">已弃用</Text><strong>{registryStats.deprecated}</strong></div>
            <div><Text type="tertiary">已退役</Text><strong>{registryStats.retired}</strong></div>
          </div>
          <Surface
            title="模型目录"
            description="这里是企业内部目录，不是公开模型市场；只维护企业确实允许评估、发布或逐步退役的模型。"
            actions={<Button icon={<IconPlus />} type="primary" onClick={openCreateRegistry}>新增模型</Button>}
          >
            <div className="ur-modelgov-filters">
              <Input prefix={<IconServer />} value={registrySearch} onChange={(value) => {
                registryRequestVersion.current += 1;
                setRegistry([]);
                setRegistryTotal(0);
                setRegistrySearch(value);
              }} placeholder="搜索模型、显示名或供应商族" showClear />
              <Select value={registryLifecycle} onChange={(value) => {
                registryRequestVersion.current += 1;
                setRegistry([]);
                setRegistryTotal(0);
                setRegistryLifecycle(String(value || ''));
              }} optionList={lifecycleOptions} placeholder="生命周期" showClear />
              <Button type={registryApprovedOnly ? 'primary' : 'tertiary'} onClick={() => {
                registryRequestVersion.current += 1;
                setRegistry([]);
                setRegistryTotal(0);
                setRegistryApprovedOnly(current => !current);
              }}>仅看已批准</Button>
            </div>
            <div className="ur-table-shell">
              <Table
                columns={registryColumns}
                dataSource={registry}
                rowKey="id"
                loading={registryLoading}
                empty={registryLoadError ? (
                  <EmptyState title="模型目录加载失败" description={registryLoadError} actions={<Button type="primary" onClick={() => loadRegistry()}>重新加载模型目录</Button>} minHeight={220} />
                ) : <EmptyState title="暂无模型" description="新增模型后才能进入别名、模型集和项目分配流程。" minHeight={220} />}
                pagination={{
                  currentPage: registryPage,
                  pageSize: registryPageSize,
                  total: registryTotal,
                  onPageChange: (page: number) => setRegistryPage(page),
                  onPageSizeChange: (pageSize: number) => { setRegistryPageSize(pageSize); setRegistryPage(1); },
                }}
              />
            </div>
          </Surface>
        </>
      )}

      {activeTab === 'aliases' && (
        <Surface
          title="企业别名"
          description="把内部稳定名称映射到真实上游模型，方便后续替换模型时减少应用侧改动。"
          actions={<Button icon={<IconPlus />} type="primary" onClick={openCreateAlias}>新增别名</Button>}
        >
          <div className="ur-table-shell">
            <Table
              columns={aliasColumns}
              dataSource={aliases}
              rowKey="id"
              loading={aliasLoading}
              empty={aliasLoadError ? (
                <EmptyState title="企业别名加载失败" description={aliasLoadError} actions={<Button type="primary" onClick={() => loadAliases()}>重新加载企业别名</Button>} minHeight={220} />
              ) : <EmptyState title="暂无别名" description="常见形式如 company-chat -> gpt-4o。" minHeight={220} />}
              pagination={{
                currentPage: aliasPage,
                pageSize: aliasPageSize,
                total: aliasTotal,
                onPageChange: (page: number) => setAliasPage(page),
                onPageSizeChange: (pageSize: number) => { setAliasPageSize(pageSize); setAliasPage(1); },
              }}
            />
          </div>
        </Surface>
      )}

      {activeTab === 'sets' && (
        <Surface
          title="模型集"
          description="把模型或别名组合成可复用集合，再分配给项目。"
          actions={<Button icon={<IconPlus />} type="primary" onClick={openCreateModelSet}>新增模型集</Button>}
        >
          <div className="ur-table-shell">
            <Table
              columns={modelSetColumns}
              dataSource={modelSets}
              rowKey="id"
              loading={modelSetLoading}
              empty={modelSetLoadError ? (
                <EmptyState title="模型集加载失败" description={modelSetLoadError} actions={<Button type="primary" onClick={() => loadModelSets()}>重新加载模型集</Button>} minHeight={220} />
              ) : <EmptyState title="暂无模型集" description="模型集条目支持 model:gpt-4o 和 alias:company-chat 两种格式。" minHeight={220} />}
              pagination={{
                currentPage: modelSetPage,
                pageSize: modelSetPageSize,
                total: modelSetTotal,
                onPageChange: (page: number) => setModelSetPage(page),
                onPageSizeChange: (pageSize: number) => { setModelSetPageSize(pageSize); setModelSetPage(1); },
              }}
            />
          </div>
        </Surface>
      )}

      {activeTab === 'assignments' && (
        <Surface title="项目分配" description="把模型集绑定到项目；项目存在绑定时，网关只允许这些模型或别名。">
          {assignmentBaseError ? (
            <EmptyState title="项目分配加载失败" description={assignmentBaseError} actions={<Button type="primary" onClick={loadAssignmentBase}>重新加载项目分配</Button>} minHeight={260} />
          ) : projects.length === 0 ? (
            <EmptyState title="暂无可分配项目" description="先创建活跃项目后再绑定模型集。" minHeight={260} />
          ) : (
            <div className="ur-modelgov-assignment">
              <div>
								<Text id="model-governance-project-label" className="ur-filter-label">项目</Text>
                <Select
                  value={selectedProjectId}
                  onChange={(value) => {
                    const projectId = Number(value);
                    if (!Number.isSafeInteger(projectId) || projectId <= 0 || projectId === selectedProjectId) return;
                    assignmentRequestVersion.current += 1;
                    setSelectedProjectId(projectId);
                    setAssignedIds([]);
                    setLoadedAssignmentProjectId(undefined);
                    setAssignmentLoading(true);
                  }}
                  optionList={projects.map(project => ({ label: `${project.name} (${project.slug})`, value: project.id }))}
                  style={{ width: '100%' }}
									aria-labelledby="model-governance-project-label"
                  disabled={assignmentLoading}
                />
              </div>
              <div>
                <Text className="ur-filter-label">模型集</Text>
                <Select
                  multiple
                  value={assignedIds}
                  onChange={(value) => setAssignedIds(Array.isArray(value) ? value.map(Number) : [])}
                  optionList={assignmentModelSets.map(set => ({ label: `${set.name} (${set.slug})`, value: set.id }))}
                  style={{ width: '100%' }}
                  placeholder="选择一个或多个模型集"
                  aria-label="选择模型集"
                  disabled={assignmentLoading || loadedAssignmentProjectId !== selectedProjectId}
                />
              </div>
              <div className="ur-modelgov-assignment-actions">
                <Button icon={<IconRefresh />} onClick={() => selectedProjectId && loadProjectAssignment(selectedProjectId)} loading={assignmentLoading}>重新加载</Button>
                <Button
                  type="primary"
                  onClick={handleAssignmentSubmit}
                  loading={assignmentLoading}
                  disabled={!selectedProjectId || loadedAssignmentProjectId !== selectedProjectId}
                >保存分配</Button>
              </div>
              {assignmentLoadError && (
                <EmptyState
                  title="所选项目分配加载失败"
                  description={assignmentLoadError}
                  actions={<Button type="primary" onClick={() => selectedProjectId && loadProjectAssignment(selectedProjectId)}>重新加载所选项目</Button>}
                  minHeight={160}
                />
              )}
              <div className="ur-chip-row">
                {!assignmentLoadError && assignedIds.length === 0 ? (
                  <Text type="tertiary">当前项目未绑定模型集，将使用基础模型目录。</Text>
                ) : !assignmentLoadError && assignedIds.map(id => {
                  const set = assignmentModelSets.find(item => item.id === id);
                  return <StatusTag key={id} status="active">{set?.name || `#${id}`}</StatusTag>;
                })}
              </div>
            </div>
          )}
        </Surface>
      )}

      {activeTab === 'releases' && platform && <RuntimeChangeControlWorkspace />}

      <SideSheet title={registrySheet?.mode === 'edit' ? '编辑模型' : '新增模型'} visible={!!registrySheet} onCancel={closeRegistrySheet} width={560}>
        <Form getFormApi={(apiRef: any) => setRegistryFormApi(apiRef)} onSubmit={handleRegistrySubmit}>
          <Form.Input field="model_name" label="模型名称" placeholder="gpt-4o" rules={[{ required: true, message: '请输入模型名称' }]} />
          <Form.Input field="display_name" label="显示名称" placeholder="Company Chat" />
          <Form.Input field="provider_family" label="供应商族" placeholder="openai / anthropic / gemini" />
          <Form.Select field="lifecycle" label="生命周期" optionList={lifecycleOptions} rules={[{ required: true, message: '请选择生命周期' }]} />
          <div className="ur-modelgov-form-grid">
            <Form.InputNumber field="context_length" label="上下文长度" min={0} style={{ width: '100%' }} />
            <Form.InputNumber field="output_limit" label="输出上限" min={0} style={{ width: '100%' }} />
          </div>
          <Form.Input field="capability_tags" label="能力标签" placeholder="chat,vision,json" />
          <Form.Input field="data_residency" label="数据驻留" placeholder="us / eu / cn" />
          <Form.Input field="pricing_model_name" label="计价模型名" placeholder="默认使用模型名称" />
          <Form.Select field="approved_catalog" label="开发者目录可见" optionList={approvedOptions} />
          <Form.TextArea field="migration_recommendation" label="迁移建议" placeholder="例如：请迁移至 gpt-4.1-mini" rows={3} />
          <div className="ur-form-actions">
            <Button onClick={closeRegistrySheet}>取消</Button>
            <Button type="primary" onClick={() => registryFormApi?.submitForm()}>保存</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title={aliasSheet?.mode === 'edit' ? '编辑别名' : '新增别名'} visible={!!aliasSheet} onCancel={closeAliasSheet} width={520}>
        <Form getFormApi={(apiRef: any) => setAliasFormApi(apiRef)} onSubmit={handleAliasSubmit}>
          <Form.Input field="alias" label="别名" placeholder="company-chat" rules={[{ required: true, message: '请输入别名' }]} />
          <Form.Input field="target_model" label="目标模型" placeholder="gpt-4o" rules={[{ required: true, message: '请输入目标模型' }]} />
          <Form.Input field="display_name" label="显示名称" placeholder="Company Chat" />
          <Form.TextArea field="description" label="描述" placeholder="内部稳定聊天模型" rows={3} />
          <Form.Select field="status" label="状态" optionList={statusOptions} />
          <div className="ur-form-actions">
            <Button onClick={closeAliasSheet}>取消</Button>
            <Button type="primary" onClick={() => aliasFormApi?.submitForm()}>保存</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title="新增模型集" visible={modelSetSheet} onCancel={closeModelSetSheet} width={540}>
        <Form getFormApi={(apiRef: any) => setModelSetFormApi(apiRef)} onSubmit={handleModelSetSubmit}>
          <Form.Input field="name" label="名称" placeholder="Core Chat Models" rules={[{ required: true, message: '请输入名称' }]} />
          <Form.Input field="slug" label="标识 Slug" placeholder="core-chat-models" />
          <Form.TextArea field="description" label="描述" placeholder="核心业务应用可用模型集合" rows={2} />
          <Form.Select field="status" label="状态" optionList={statusOptions} />
          <Form.TextArea field="items" label="条目" placeholder={'alias:company-chat\nmodel:gpt-4o-mini'} rows={7} />
          <div className="ur-form-actions">
            <Button onClick={closeModelSetSheet}>取消</Button>
            <Button type="primary" onClick={() => modelSetFormApi?.submitForm()}>创建</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title={`模型集条目：${itemsSheet?.name || ''}`} visible={!!itemsSheet} onCancel={closeItemsSheet} width={520}>
        <Space vertical align="start" style={{ width: '100%' }}>
          <Text type="tertiary">每行一条，格式为 model:gpt-4o 或 alias:company-chat。</Text>
          <TextArea value={itemsText} onChange={setItemsText} rows={12} style={{ width: '100%' }} disabled={itemsLoading} />
          <div className="ur-form-actions">
            <Button onClick={closeItemsSheet}>取消</Button>
            <Button type="primary" loading={itemsLoading} onClick={handleItemsSubmit}>保存条目</Button>
          </div>
        </Space>
      </SideSheet>
    </div>
  );
}
