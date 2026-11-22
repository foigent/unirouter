import '../i18n/people';
import { translate as t, localizeOptions, useI18n } from '../i18n';
import { useEffect, useState } from 'react';
import { Button, Form, SideSheet, Toast } from '@douyinfe/semi-ui';
import { IconEdit, IconRefresh } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PagedTable, StatusTag } from '../ui';
import { useServerPage } from '../utils/pagination';
import { MAX_PROJECT_BUDGET_YUAN } from '../utils/projectDisplay';

interface ProjectBudget {
  id: number;
  name: string;
  slug: string;
  status: string;
  budget_cents: number;
  budget_period: string;
}

const periodOptions = [
  { label: '不限制', value: 'none' },
  { label: '每日', value: 'daily' },
  { label: '每月', value: 'monthly' },
];

export default function ProjectBudgetPanel() {
  useI18n();
  const projectPage = useServerPage<{ data?: ProjectBudget[]; total?: number }>(api.getProjects, 'project-budgets');
  const projects = projectPage.data?.data ?? [];
  const [editing, setEditing] = useState<ProjectBudget | null>(null);
  const [formApi, setFormApi] = useState<any>(null);

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
    if (!Number.isFinite(yuan) || yuan < 0 || yuan > MAX_PROJECT_BUDGET_YUAN) {
      Toast.error(t('预算金额需在 0 至 {max} 元之间', { max: MAX_PROJECT_BUDGET_YUAN.toFixed(2) }));
      return;
    }
    try {
      await api.updateProjectBudget(editing.id, {
        budget_cents: Math.round(yuan * 100),
        budget_period: values.budget_period,
      });
      Toast.success(t("项目预算已更新"));
      setEditing(null);
      projectPage.reload();
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

  return (
    <>
      <div className="ur-surface-actions">
        <Button icon={<IconRefresh />} onClick={projectPage.reload}>{t("刷新")}</Button>
      </div>
      <PagedTable
        rowKey="id"
        columns={columns}
        dataSource={projects}
        loading={projectPage.loading}
        total={projectPage.failed ? 0 : projectPage.latestData?.total ?? 0}
        page={projectPage.page}
        pageSize={projectPage.pageSize}
        showSizeChanger
        onPageChange={projectPage.onPageChange}
        onPageSizeChange={projectPage.onPageSizeChange}
        loadError={projectPage.failed ? { title: t("项目预算加载失败"), retry: projectPage.reload } : undefined}
        empty={<EmptyState title={t("暂无项目预算")} description={t("项目创建后会在这里显示；账务管理员只能修改预算金额与周期。")} />}
      />
      <SideSheet title={editing ? t('调整预算 · {name}', { name: editing.name }) : t("调整预算")} visible={!!editing} onCancel={() => setEditing(null)} width="min(440px, 100vw)">
        <Form getFormApi={setFormApi} onSubmit={save}>
          <Form.InputNumber
            field="budget_yuan"
            label={t("预算金额（元）")}
            min={0}
            max={MAX_PROJECT_BUDGET_YUAN}
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
