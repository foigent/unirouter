import '../i18n/access';
import { translate as t, translateLabel } from '../i18n';
import { roleLabel } from './displayLabels';

const scopeModeLabels: Record<string, string> = {
  project_default: '跟随项目可用模型与接口',
  explicit: '仅限指定模型与接口',
};

const reasonLabels: Record<string, string> = {
  auto_approved_current_project_default: '当前项目的默认使用权限可自动批准',
  auto_approved_workload_project_default: '当前项目的应用默认权限可自动批准',
  direct_grant_authorized_nonproduction: '管理员直接授予非生产权限',
  bootstrap_authorized: '初始化流程授权',
  recovery_authorized: '受控恢复流程授权',
  project_default_scope: '使用项目默认范围',
  first_cross_project: '首次加入此项目，需要管理员确认',
  role_elevation: '申请更高的项目权限',
  special_model: '包含需要额外审核的模型',
  special_endpoint: '包含需要额外审核的接口',
  scope_expansion: '扩大现有使用范围',
  extra_rate_or_concurrency: '提高速率或并发上限',
  validity_extension: '延长授权有效期',
  production_workload: '生产应用权限需要审核',
  ambiguous_migration: '历史权限归属需要人工确认',
  workload_requires_review: '应用权限需要管理员确认',
  read_only_subject_cannot_invoke: '只读账号不能调用模型',
  manual_approved: '管理员已批准',
  manual_rejected: '管理员已拒绝',
  requester_cancelled: '申请人已取消',
  subject_offboarded: '人员已离职或停用',
  workload_archived: '应用已归档',
  organization_admin_actionable: '组织管理员可处理',
  project_admin_actionable: '项目管理员可处理',
  requester_actionable: '申请人可处理',
  organization_admin_required: '需要组织管理员处理',
  not_authorized: '当前账号无权处理',
};

export function accessScopeLabel(projectRole?: string, scopeMode?: string) {
  return `${roleLabel(projectRole)} · ${Object.prototype.hasOwnProperty.call(scopeModeLabels, scopeMode || '') ? translateLabel(scopeModeLabels[scopeMode || '']) : scopeMode || t('未说明范围')}`;
}

export function accessReasonLabel(reason?: string) {
  return Object.prototype.hasOwnProperty.call(reasonLabels, reason || '') ? translateLabel(reasonLabels[reason || '']) : reason || t('未说明原因');
}
