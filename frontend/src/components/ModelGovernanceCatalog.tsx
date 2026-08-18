import { Banner, Button, Input, Select, Space, Table, Typography } from '@douyinfe/semi-ui';
import { IconEdit, IconPlus, IconServer, IconSetting } from '@douyinfe/semi-icons';
import { EmptyState, StatusTag, Surface } from '../ui';
import {
  compactNumber,
  lifecycleOptions,
  toList,
  type ModelAlias,
  type ModelRegistryItem,
  type ModelSet,
} from '../utils/modelGovernance';

const { Text } = Typography;

type RegistryStats = { ga: number; deprecated: number; retired: number };

export function ModelRegistryPanel({
  stats,
  total,
  duplicates,
  search,
  lifecycle,
  approvedOnly,
  records,
  loading,
  error,
  page,
  pageSize,
  onSearchChange,
  onLifecycleChange,
  onApprovedOnlyChange,
  onPageChange,
  onPageSizeChange,
  onReload,
  onCreate,
  onEdit,
  onRetire,
}: {
  stats: RegistryStats;
  total: number;
  duplicates: ModelRegistryItem[][];
  search: string;
  lifecycle: string;
  approvedOnly: boolean;
  records: ModelRegistryItem[];
  loading: boolean;
  error: string;
  page: number;
  pageSize: number;
  onSearchChange: (value: string) => void;
  onLifecycleChange: (value: string) => void;
  onApprovedOnlyChange: () => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onReload: () => void;
  onCreate: () => void;
  onEdit: (record: ModelRegistryItem) => void;
  onRetire: (record: ModelRegistryItem) => void;
}) {
  const columns = [
    {
      title: '模型', dataIndex: 'model_name', key: 'model_name',
      render: (value: string, record: ModelRegistryItem) => <div className="ur-modelgov-name">
        <Text strong>{record.display_name || value}</Text>
        <Text code>{value}</Text>
        {record.migration_recommendation && <Text type="tertiary" size="small">{record.migration_recommendation}</Text>}
      </div>,
    },
    { title: '供应商族', dataIndex: 'provider_family', key: 'provider_family', render: (value: string) => value || '-' },
    { title: '生命周期', dataIndex: 'lifecycle', key: 'lifecycle', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    {
      title: '项目目录状态', dataIndex: 'approved_catalog', key: 'approved_catalog',
      render: (value: boolean, record: ModelRegistryItem) => {
        if (record.lifecycle === 'retired') return <StatusTag status="disabled">已退役·不可见</StatusTag>;
        if (value && ['ga', 'deprecated'].includes(record.lifecycle)) return <StatusTag status="success">项目可见</StatusTag>;
        if (value) return <StatusTag status="warning">已批准·待上线</StatusTag>;
        return <StatusTag status="disabled">仅内部</StatusTag>;
      },
    },
    { title: '上下文 / 输出', key: 'limits', render: (_: unknown, record: ModelRegistryItem) => `${compactNumber(record.context_length)} / ${compactNumber(record.output_limit)}` },
    { title: '能力', dataIndex: 'capability_tags', key: 'capability_tags', render: (value: string[] | string) => toList(value).slice(0, 4).join(', ') || '-' },
    { title: '数据驻留', dataIndex: 'data_residency', key: 'data_residency', render: (value: string) => value || '-' },
    {
      title: '操作', key: 'actions', width: 210,
      render: (_: unknown, record: ModelRegistryItem) => <Space>
        <Button icon={<IconEdit />} size="small" onClick={() => onEdit(record)} aria-label="编辑模型" />
        {record.lifecycle !== 'retired' && <Button size="small" type="danger" onClick={() => onRetire(record)}>退役并移出目录</Button>}
      </Space>,
    },
  ];

  return <>
    <div className="ur-modelgov-summary">
      <div><Text type="tertiary">当前页 GA 记录</Text><strong>{stats.ga}</strong></div>
      <div><Text type="tertiary">当前筛选记录总数</Text><strong>{total}</strong></div>
      <div><Text type="tertiary">当前页已弃用</Text><strong>{stats.deprecated}</strong></div>
      <div><Text type="tertiary">当前页已退役</Text><strong>{stats.retired}</strong></div>
    </div>
    <Text type="tertiary" size="small">GA、目录批准和当前可调用是不同条件；这里的数字只统计模型目录记录，不代表供应商路由或应用 API Key 已就绪。</Text>
    {duplicates.length > 0 && <Banner
      type="warning"
      description={`发现 ${duplicates.length} 组疑似重复模型标识：${duplicates.map(group => group.map(item => item.model_name).join(' / ')).join('；')}。请选择保留的精确模型 ID，并将旧记录“退役并移出目录”。`}
      closeIcon={null}
      style={{ marginTop: 12, marginBottom: 12 }}
    />}
    <Surface
      title="模型目录"
      description="这里是企业内部目录，不是公开模型市场；只维护企业确实允许评估、发布或逐步退役的模型。"
      actions={<Button icon={<IconPlus />} type="primary" onClick={onCreate}>新增模型</Button>}
    >
      <div className="ur-modelgov-filters">
        <Input aria-label="搜索模型目录" prefix={<IconServer />} value={search} onChange={onSearchChange} placeholder="搜索模型、显示名或供应商族" showClear />
        <Select aria-labelledby="model-catalog-lifecycle-label" insetLabel="生命周期" insetLabelId="model-catalog-lifecycle-label" value={lifecycle} onChange={(value) => onLifecycleChange(String(value || ''))} optionList={lifecycleOptions} placeholder="全部" showClear />
        <Button type={approvedOnly ? 'primary' : 'tertiary'} onClick={onApprovedOnlyChange}>仅看已批准</Button>
      </div>
      <div className="ur-table-shell">
        <Table
          columns={columns}
          dataSource={records}
          rowKey="id"
          loading={loading}
          empty={error
            ? <EmptyState title="模型目录加载失败" description={error} actions={<Button type="primary" onClick={onReload}>重新加载模型目录</Button>} minHeight={220} />
            : <EmptyState title="暂无模型" description="新增模型后才能进入别名、模型集和项目分配流程。" minHeight={220} />}
          pagination={{ currentPage: page, pageSize, total, onPageChange, onPageSizeChange }}
        />
      </div>
    </Surface>
  </>;
}

export function ModelAliasPanel({
  records, loading, error, page, pageSize, total, onReload, onCreate, onEdit, onPageChange, onPageSizeChange,
}: {
  records: ModelAlias[];
  loading: boolean;
  error: string;
  page: number;
  pageSize: number;
  total: number;
  onReload: () => void;
  onCreate: () => void;
  onEdit: (record: ModelAlias) => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
}) {
  const columns = [
    { title: '别名', dataIndex: 'alias', key: 'alias', render: (value: string) => <Text code>{value}</Text> },
    { title: '目标模型', dataIndex: 'target_model', key: 'target_model' },
    { title: '名称', dataIndex: 'display_name', key: 'display_name', render: (value: string) => value || '-' },
    { title: '描述', dataIndex: 'description', key: 'description', render: (value: string) => value || '-' },
    { title: '状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: '操作', key: 'actions', width: 86, render: (_: unknown, record: ModelAlias) => <Button icon={<IconEdit />} size="small" onClick={() => onEdit(record)} aria-label="编辑别名" /> },
  ];
  return <Surface
    title="企业别名"
    description="把内部稳定名称映射到真实上游模型，方便后续替换模型时减少应用侧改动。"
    actions={<Button icon={<IconPlus />} type="primary" onClick={onCreate}>新增别名</Button>}
  >
    <div className="ur-table-shell">
      <Table
        columns={columns}
        dataSource={records}
        rowKey="id"
        loading={loading}
        empty={error
          ? <EmptyState title="企业别名加载失败" description={error} actions={<Button type="primary" onClick={onReload}>重新加载企业别名</Button>} minHeight={220} />
          : <EmptyState title="暂无别名" description="常见形式如 company-chat -> gpt-4o。" minHeight={220} />}
        pagination={{ currentPage: page, pageSize, total, onPageChange, onPageSizeChange }}
      />
    </div>
  </Surface>;
}

export function ModelSetPanel({
  records, loading, error, page, pageSize, total, onReload, onCreate, onEditItems, onPageChange, onPageSizeChange,
}: {
  records: ModelSet[];
  loading: boolean;
  error: string;
  page: number;
  pageSize: number;
  total: number;
  onReload: () => void;
  onCreate: () => void;
  onEditItems: (record: ModelSet) => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
}) {
  const columns = [
    {
      title: '模型集', dataIndex: 'name', key: 'name',
      render: (value: string, record: ModelSet) => <div className="ur-modelgov-name">
        <Text strong>{value}</Text>
        <Text code>{record.slug}</Text>
        {record.description && <Text type="tertiary" size="small">{record.description}</Text>}
      </div>,
    },
    { title: '状态', dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: '条目数', dataIndex: 'item_count', key: 'item_count', render: (value: number) => value || 0 },
    { title: '操作', key: 'actions', width: 112, render: (_: unknown, record: ModelSet) => <Button icon={<IconSetting />} size="small" onClick={() => onEditItems(record)}>条目</Button> },
  ];
  return <Surface
    title="模型集"
    description="把模型或别名组合成可复用集合，再分配给项目。"
    actions={<Button icon={<IconPlus />} type="primary" onClick={onCreate}>新增模型集</Button>}
  >
    <div className="ur-table-shell">
      <Table
        columns={columns}
        dataSource={records}
        rowKey="id"
        loading={loading}
        empty={error
          ? <EmptyState title="模型集加载失败" description={error} actions={<Button type="primary" onClick={onReload}>重新加载模型集</Button>} minHeight={220} />
          : <EmptyState title="暂无模型集" description="模型集条目支持 model:gpt-4o 和 alias:company-chat 两种格式。" minHeight={220} />}
        pagination={{ currentPage: page, pageSize, total, onPageChange, onPageSizeChange }}
      />
    </div>
  </Surface>;
}
