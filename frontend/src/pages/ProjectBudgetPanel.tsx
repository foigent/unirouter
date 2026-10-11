import '../i18n/people';
import { translate as t, localizeOptions, useI18n } from '../i18n';
import { useCallback, useEffect, useState } from 'react';
import { Button, Form, SideSheet, Table, Toast } from '@douyinfe/semi-ui';
import { IconEdit, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, StatusTag } from '../ui';

interface ProjectBudget {
  id: number;
  name: string;
  slug: string;
  status: string;
  budget_cents: number;
  budget_period: string;
}

const maxProjectBudgetYuan = 21474836.47;

const periodOptions = [
  { label: '不限制', value: 'none' },
  { label: '每日', value: 'daily' },
  { label: '每月', value: 'monthly' },
];

export default function ProjectBudgetPanel() {
  useI18n();
  const [projects, setProjects] = useState<ProjectBudget[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState<ProjectBudget | null>(null);
  const [formApi, setFormApi] = useState<any>(null);

  const load = useCallback((nextPage = page, nextPageSize = pageSize) => {
    setLoading(true);
    return api.getProjects(nextPage, nextPageSize)
      .then((response: any) => {
        setProjects(response.data || []);
        setTotal(response.total || 0);
      })
      .catch(() => {
        setProjects([]);
        setTotal(0);
        Toast.error(t("项目预算加载失败"));
      })
      .finally(() => setLoading(false));
  }, [page, pageSize]);

  useEffect(() => { void load(1, pageSize); }, []);
  useEffect(() => {
    if (!editing || !formApi) return;
    formApi.setValues({
      budget_yuan: Number((Number(editing.budget_cents || 0) / 100).toFixed(2)),
      budget_period: editing.budget_period,
    });
  }, [editing, formApi]);

  const save = async (values: any) => {
    if (!editing) return;
    const yuan = Number(values.budget_yuan || 0);
    if (!Number.isFinite(yuan) || yuan < 0 || yuan > maxProjectBudgetYuan) {
      Toast.error(t('预算金额需在 0 至 {max} 元之间', { max: maxProjectBudgetYuan.toFixed(2) }));
      return;
    }
    try {
      await api.updateProjectBudget(editing.id, {
        budget_cents: Math.round(yuan * 100),
        budget_period: values.budget_period,
      });
      Toast.success(t("项目预算已更新"));
      setEditing(null);
      await load(page, pageSize);
    } catch {
      Toast.error(t("项目预算更新失败"));
    }
  };

  const columns = [
    { title: t("项目"), dataIndex: 'name' },
    { title: t("标识"), dataIndex: 'slug' },
    { title: t("状态"), dataIndex: 'status', render: (status: string) => <StatusTag status={status}>{status === 'active' ? t("启用") : status === 'disabled' ? t("停用") : t("归档")}</StatusTag> },
    {
      title: t("预算"),
      render: (_: unknown, project: ProjectBudget) => project.budget_period === 'none' || !project.budget_cents
        ? t("不限制")
        : `¥${(project.budget_cents / 100).toFixed(2)} / ${project.budget_period === 'daily' ? t("日") : t("月")}`,
    },
    { title: t("操作"), render: (_: unknown, project: ProjectBudget) => <Button icon={<IconEdit />} onClick={() => setEditing(project)}>{t("调整预算")}</Button> },
  ];

  if (!loading && projects.length === 0) {
    return <EmptyState title={t("暂无项目预算")} description={t("项目创建后会在这里显示；账务管理员只能修改预算金额与周期。")} />;
  }

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
        <Button icon={<IconRefresh />} onClick={() => load(page, pageSize)}>{t("刷新")}</Button>
      </div>
      <div className="ur-table-shell">
        <Table
          rowKey="id"
          columns={columns}
          dataSource={projects}
          loading={loading}
          pagination={{
            currentPage: page,
            pageSize,
            total,
            showSizeChanger: true,
            pageSizeOpts: [20, 50, 100],
            onPageChange: nextPage => { setPage(nextPage); void load(nextPage, pageSize); },
            onPageSizeChange: size => { setPage(1); setPageSize(size); void load(1, size); },
          }}
        />
      </div>
      <SideSheet title={editing ? t('调整预算 · {name}', { name: editing.name }) : t("调整预算")} visible={!!editing} onCancel={() => setEditing(null)} width="min(440px, 100vw)">
        <Form getFormApi={setFormApi} onSubmit={save}>
          <Form.InputNumber
            field="budget_yuan"
            label={t("预算金额（元）")}
            min={0}
            max={maxProjectBudgetYuan}
            precision={2}
            extraText={t("0 表示不限制；最大值由当前数据库金额字段上限决定。")}
            style={{ width: '100%' }}
          />
          <Form.Select field="budget_period" aria-label={t("项目预算周期")} label={t("预算周期")} optionList={localizeOptions(periodOptions)} style={{ width: '100%' }} />
          <Button type="primary" htmlType="submit">{t("保存预算")}</Button>
        </Form>
      </SideSheet>
    </>
  );
}
