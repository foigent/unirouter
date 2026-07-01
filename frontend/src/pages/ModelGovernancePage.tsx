import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, Form, Input, Select, SideSheet, Space, Table, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { IconEdit, IconPlus, IconRefresh, IconServer, IconSetting } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';

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
  { label: '加入 Approved Catalog', value: 'true' },
  { label: '不加入 Approved Catalog', value: 'false' },
];

type TabKey = 'registry' | 'aliases' | 'sets' | 'assignments';

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
  const [activeTab, setActiveTab] = useState<TabKey>('registry');

  const [registry, setRegistry] = useState<ModelRegistryItem[]>([]);
  const [registryTotal, setRegistryTotal] = useState(0);
  const [registryPage, setRegistryPage] = useState(1);
  const [registryPageSize, setRegistryPageSize] = useState(20);
  const [registryLoading, setRegistryLoading] = useState(false);
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
  const [aliasSheet, setAliasSheet] = useState<{ mode: 'create' | 'edit'; record?: ModelAlias } | null>(null);
  const [aliasFormApi, setAliasFormApi] = useState<any>(null);

  const [modelSets, setModelSets] = useState<ModelSet[]>([]);
  const [modelSetTotal, setModelSetTotal] = useState(0);
  const [modelSetPage, setModelSetPage] = useState(1);
  const [modelSetPageSize, setModelSetPageSize] = useState(20);
  const [modelSetLoading, setModelSetLoading] = useState(false);
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

  const tabs = [
    { key: 'registry' as const, label: 'Model Registry' },
    { key: 'aliases' as const, label: 'Aliases' },
    { key: 'sets' as const, label: 'Model Sets' },
    { key: 'assignments' as const, label: 'Project Assignment' },
  ];

  const loadRegistry = useCallback(async (page = registryPage, pageSize = registryPageSize) => {
    setRegistryLoading(true);
    try {
      const data = await api.getModelRegistry(page, pageSize, {
        lifecycle: registryLifecycle,
        approved_only: registryApprovedOnly,
        q: registrySearch,
      });
      setRegistry((data?.data || []) as ModelRegistryItem[]);
      setRegistryTotal(Number(data?.total || 0));
    } catch (err: any) {
      Toast.error(`模型目录加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setRegistryLoading(false);
    }
  }, [registryApprovedOnly, registryLifecycle, registryPage, registryPageSize, registrySearch]);

  const loadAliases = useCallback(async (page = aliasPage, pageSize = aliasPageSize) => {
    setAliasLoading(true);
    try {
      const data = await api.getModelAliases(page, pageSize);
      setAliases((data?.data || []) as ModelAlias[]);
      setAliasTotal(Number(data?.total || 0));
    } catch (err: any) {
      Toast.error(`Alias 加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setAliasLoading(false);
    }
  }, [aliasPage, aliasPageSize]);

  const loadModelSets = useCallback(async (page = modelSetPage, pageSize = modelSetPageSize) => {
    setModelSetLoading(true);
    try {
      const data = await api.getModelSets(page, pageSize);
      setModelSets((data?.data || []) as ModelSet[]);
      setModelSetTotal(Number(data?.total || 0));
    } catch (err: any) {
      Toast.error(`模型集加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setModelSetLoading(false);
    }
  }, [modelSetPage, modelSetPageSize]);

  const loadAssignmentBase = useCallback(async () => {
    setAssignmentLoading(true);
    try {
      const [projectData, setData] = await Promise.all([
        api.getProjects(1, 100, { status: 'active' }),
        api.getModelSets(1, 100),
      ]);
      const projectItems = (projectData?.data || []) as Project[];
      const setItems = (setData?.data || []) as ModelSet[];
      setProjects(projectItems);
      setAssignmentModelSets(setItems);
      setSelectedProjectId(current => {
        if (current && projectItems.some(project => project.id === current)) return current;
        return projectItems[0]?.id;
      });
    } catch (err: any) {
      Toast.error(`项目模型集加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setAssignmentLoading(false);
    }
  }, []);

  const loadProjectAssignment = useCallback(async (projectId: number) => {
    setAssignmentLoading(true);
    try {
      const data = await api.getProjectModelSets(projectId);
      const items = (data?.data || []) as ModelSet[];
      setAssignedIds(items.map(item => item.id));
    } catch (err: any) {
      Toast.error(`项目分配加载失败${err?.message ? `：${err.message}` : ''}`);
    } finally {
      setAssignmentLoading(false);
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
    };
    try {
      if (registrySheet?.mode === 'edit' && registrySheet.record) {
        await api.updateModelRegistry(registrySheet.record.id, payload);
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
    };
    try {
      if (aliasSheet?.mode === 'edit' && aliasSheet.record) {
        await api.updateModelAlias(aliasSheet.record.id, payload);
        Toast.success('Alias 已更新');
      } else {
        await api.createModelAlias(payload);
        Toast.success('Alias 已创建');
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
      const data = await api.getModelSetItems(record.id);
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
      await api.replaceModelSetItems(itemsSheet.id, { items });
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
    if (!selectedProjectId) return;
    setAssignmentLoading(true);
    try {
      await api.replaceProjectModelSets(selectedProjectId, assignedIds);
      Toast.success('项目模型集已更新');
      loadProjectAssignment(selectedProjectId);
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
    { title: 'Provider', dataIndex: 'provider_family', key: 'provider_family', render: (value: string) => value || '-' },
    { title: '生命周期', dataIndex: 'lifecycle', key: 'lifecycle', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: 'Catalog', dataIndex: 'approved_catalog', key: 'approved_catalog', render: (value: boolean) => <StatusTag status={Boolean(value)}>{value ? 'approved' : 'internal'}</StatusTag> },
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
    { title: 'Alias', dataIndex: 'alias', key: 'alias', render: (value: string) => <Text code>{value}</Text> },
    { title: '目标模型', dataIndex: 'target_model', key: 'target_model' },
    { title: '名称', dataIndex: 'display_name', key: 'display_name', render: (value: string) => value || '-' },
    { title: '描述', dataIndex: 'description', key: 'description', render: (value: string) => value || '-' },
    { title: '状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    {
      title: '操作',
      key: 'actions',
      width: 86,
      render: (_: unknown, record: ModelAlias) => (
        <Button icon={<IconEdit />} size="small" onClick={() => openEditAlias(record)} aria-label="编辑 Alias" />
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
        eyebrow="PRD2"
        title="模型治理"
        description="维护企业内部模型目录、生命周期、企业 Alias、模型集和项目可访问模型范围。"
        actions={<Button icon={<IconRefresh />} onClick={() => activeTab === 'assignments' ? loadAssignmentBase() : activeTab === 'sets' ? loadModelSets() : activeTab === 'aliases' ? loadAliases() : loadRegistry()}>刷新</Button>}
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
            <div><Text type="tertiary">GA</Text><strong>{registryStats.ga}</strong></div>
            <div><Text type="tertiary">Deprecated</Text><strong>{registryStats.deprecated}</strong></div>
            <div><Text type="tertiary">Retired</Text><strong>{registryStats.retired}</strong></div>
          </div>
          <Surface
            title="Model Registry"
            description="这里是企业内部目录，不是公开模型市场；只维护企业确实允许评估、发布或逐步退役的模型。"
            actions={<Button icon={<IconPlus />} type="primary" onClick={openCreateRegistry}>新增模型</Button>}
          >
            <div className="ur-modelgov-filters">
              <Input prefix={<IconServer />} value={registrySearch} onChange={setRegistrySearch} placeholder="搜索模型、显示名或 Provider" showClear />
              <Select value={registryLifecycle} onChange={(value) => setRegistryLifecycle(String(value || ''))} optionList={lifecycleOptions} placeholder="生命周期" showClear />
              <Button type={registryApprovedOnly ? 'primary' : 'tertiary'} onClick={() => setRegistryApprovedOnly(current => !current)}>Approved only</Button>
            </div>
            <div className="ur-table-shell">
              <Table
                columns={registryColumns}
                dataSource={registry}
                rowKey="id"
                loading={registryLoading}
                empty={<EmptyState title="暂无模型" description="新增模型后才能进入 Alias、模型集和项目分配流程。" minHeight={220} />}
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
          title="Enterprise Aliases"
          description="把内部稳定名称映射到真实上游模型，方便后续替换模型时减少应用侧改动。"
          actions={<Button icon={<IconPlus />} type="primary" onClick={openCreateAlias}>新增 Alias</Button>}
        >
          <div className="ur-table-shell">
            <Table
              columns={aliasColumns}
              dataSource={aliases}
              rowKey="id"
              loading={aliasLoading}
              empty={<EmptyState title="暂无 Alias" description="常见形式如 company-chat -> gpt-4o。" minHeight={220} />}
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
          title="Model Sets"
          description="把模型或 Alias 组合成可复用集合，再分配给项目。"
          actions={<Button icon={<IconPlus />} type="primary" onClick={openCreateModelSet}>新增模型集</Button>}
        >
          <div className="ur-table-shell">
            <Table
              columns={modelSetColumns}
              dataSource={modelSets}
              rowKey="id"
              loading={modelSetLoading}
              empty={<EmptyState title="暂无模型集" description="模型集条目支持 model:gpt-4o 和 alias:company-chat 两种格式。" minHeight={220} />}
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
        <Surface title="Project Assignment" description="把模型集绑定到项目；项目存在绑定时，网关只允许这些模型或 Alias。">
          {projects.length === 0 ? (
            <EmptyState title="暂无可分配项目" description="先创建活跃项目后再绑定模型集。" minHeight={260} />
          ) : (
            <div className="ur-modelgov-assignment">
              <div>
                <Text className="ur-filter-label">项目</Text>
                <Select
                  value={selectedProjectId}
                  onChange={(value) => setSelectedProjectId(Number(value))}
                  optionList={projects.map(project => ({ label: `${project.name} (${project.slug})`, value: project.id }))}
                  style={{ width: '100%' }}
                  aria-label="选择项目"
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
                />
              </div>
              <div className="ur-modelgov-assignment-actions">
                <Button icon={<IconRefresh />} onClick={() => selectedProjectId && loadProjectAssignment(selectedProjectId)} loading={assignmentLoading}>重新加载</Button>
                <Button type="primary" onClick={handleAssignmentSubmit} loading={assignmentLoading}>保存分配</Button>
              </div>
              <div className="ur-chip-row">
                {assignedIds.length === 0 ? (
                  <Text type="tertiary">当前项目未绑定模型集，将保持 PRD1 兼容行为。</Text>
                ) : assignedIds.map(id => {
                  const set = assignmentModelSets.find(item => item.id === id);
                  return <StatusTag key={id} status="active">{set?.name || `#${id}`}</StatusTag>;
                })}
              </div>
            </div>
          )}
        </Surface>
      )}

      <SideSheet title={registrySheet?.mode === 'edit' ? '编辑模型' : '新增模型'} visible={!!registrySheet} onCancel={closeRegistrySheet} width={560}>
        <Form getFormApi={(apiRef: any) => setRegistryFormApi(apiRef)} onSubmit={handleRegistrySubmit}>
          <Form.Input field="model_name" label="模型名称" placeholder="gpt-4o" rules={[{ required: true, message: '请输入模型名称' }]} />
          <Form.Input field="display_name" label="显示名称" placeholder="Company Chat" />
          <Form.Input field="provider_family" label="Provider Family" placeholder="openai / anthropic / gemini" />
          <Form.Select field="lifecycle" label="生命周期" optionList={lifecycleOptions} rules={[{ required: true, message: '请选择生命周期' }]} />
          <div className="ur-modelgov-form-grid">
            <Form.InputNumber field="context_length" label="上下文长度" min={0} style={{ width: '100%' }} />
            <Form.InputNumber field="output_limit" label="输出上限" min={0} style={{ width: '100%' }} />
          </div>
          <Form.Input field="capability_tags" label="能力标签" placeholder="chat,vision,json" />
          <Form.Input field="data_residency" label="数据驻留" placeholder="us / eu / cn" />
          <Form.Input field="pricing_model_name" label="计价模型名" placeholder="默认使用模型名称" />
          <Form.Select field="approved_catalog" label="Approved Catalog" optionList={approvedOptions} />
          <Form.TextArea field="migration_recommendation" label="迁移建议" placeholder="例如：请迁移至 gpt-4.1-mini" rows={3} />
          <div className="ur-form-actions">
            <Button onClick={closeRegistrySheet}>取消</Button>
            <Button type="primary" onClick={() => registryFormApi?.submitForm()}>保存</Button>
          </div>
        </Form>
      </SideSheet>

      <SideSheet title={aliasSheet?.mode === 'edit' ? '编辑 Alias' : '新增 Alias'} visible={!!aliasSheet} onCancel={closeAliasSheet} width={520}>
        <Form getFormApi={(apiRef: any) => setAliasFormApi(apiRef)} onSubmit={handleAliasSubmit}>
          <Form.Input field="alias" label="Alias" placeholder="company-chat" rules={[{ required: true, message: '请输入 Alias' }]} />
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
          <Form.Input field="slug" label="Slug" placeholder="core-chat-models" />
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
