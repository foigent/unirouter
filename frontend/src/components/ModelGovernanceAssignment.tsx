import { translate as t, translateLabel, useI18n } from '../i18n';
import '../i18n/models';
import { Button, Select, Typography } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { EmptyState, StatusTag, Surface } from '../ui';
import { projectOption } from '../utils/projectDisplay';
import type { ModelSet, Project } from '../utils/modelGovernance';

const { Text } = Typography;

interface ModelGovernanceAssignmentProps {
  projects: Project[];
  modelSets: ModelSet[];
  selectedProjectId?: number;
  assignedIds: number[];
  loading: boolean;
  baseError: string;
  loadError: string;
  loadedProjectId?: number;
  onReloadBase: () => void | Promise<void>;
  onSelectProject: (projectId: number) => void;
  onChangeAssigned: (ids: number[]) => void;
  onReloadProject: (projectId: number) => void | Promise<void>;
  onSave: () => void | Promise<void>;
}

export default function ModelGovernanceAssignment(props: ModelGovernanceAssignmentProps) {
  useI18n();
  return (
    <Surface title={t("项目分配")} description={t("把模型集绑定到项目；项目存在绑定时，网关只允许这些模型或别名。")}>
      {props.baseError ? (
        <EmptyState title={t("项目分配加载失败")} description={translateLabel(props.baseError)} actions={<Button type="primary" onClick={props.onReloadBase}>{t("重新加载项目分配")}</Button>} minHeight={260} />
      ) : props.projects.length === 0 ? (
        <EmptyState title={t("暂无可分配项目")} description={t("先创建活跃项目后再绑定模型集。")} minHeight={260} />
      ) : (
        <div className="ur-modelgov-assignment">
          <div>
            <Text id="model-governance-project-label" className="ur-filter-label">{t("项目")}</Text>
            <Select
              value={props.selectedProjectId}
              onChange={(value) => {
                const projectId = Number(value);
                if (Number.isSafeInteger(projectId) && projectId > 0 && projectId !== props.selectedProjectId) {
                  props.onSelectProject(projectId);
                }
              }}
              optionList={props.projects.map(projectOption)}
              style={{ width: '100%' }}
              aria-labelledby="model-governance-project-label"
              disabled={props.loading}
            />
          </div>
          <div>
            <Text id="model-governance-model-set-label" className="ur-filter-label">{t("模型集")}</Text>
            <Select
              multiple
              value={props.assignedIds}
              onChange={(value) => props.onChangeAssigned(Array.isArray(value) ? value.map(Number) : [])}
              optionList={props.modelSets.map(set => ({ label: `${set.name} (${set.slug})`, value: set.id }))}
              style={{ width: '100%' }}
              placeholder={t("选择一个或多个模型集")}
              aria-labelledby="model-governance-model-set-label"
              disabled={props.loading || props.loadedProjectId !== props.selectedProjectId}
            />
          </div>
          <div className="ur-modelgov-assignment-actions">
            <Button icon={<IconRefresh />} onClick={() => props.selectedProjectId && props.onReloadProject(props.selectedProjectId)} loading={props.loading}>{t("重新加载")}</Button>
            <Button
              type="primary"
              onClick={props.onSave}
              loading={props.loading}
              disabled={!props.selectedProjectId || props.loadedProjectId !== props.selectedProjectId}
            >{t("保存分配")}</Button>
          </div>
          {props.loadError && (
            <EmptyState
              title={t("所选项目分配加载失败")}
              description={translateLabel(props.loadError)}
              actions={<Button type="primary" onClick={() => props.selectedProjectId && props.onReloadProject(props.selectedProjectId)}>{t("重新加载所选项目")}</Button>}
              minHeight={160}
            />
          )}
          <div className="ur-chip-row">
            {!props.loadError && props.assignedIds.length === 0 ? (
              <Text type="tertiary">{t("当前项目未绑定模型集，将使用基础模型目录。")}</Text>
            ) : !props.loadError && props.assignedIds.map(id => {
              const set = props.modelSets.find(item => item.id === id);
              return <StatusTag key={id} status="active">{set?.name || t("已选模型集")}</StatusTag>;
            })}
          </div>
        </div>
      )}
    </Surface>
  );
}
