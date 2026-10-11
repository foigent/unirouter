import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import type { UserProfile } from '../store/auth';
import { translate as t } from '../i18n';

export type CustomerContextState = 'ready' | 'not_initialized' | 'ambiguous' | 'inactive' | 'unavailable';

export type EffectiveCustomerContext = {
  state: CustomerContextState;
  orgID?: number;
  orgName?: string;
};

function positiveOrganizationID(value: unknown): number | undefined {
  const id = Number(value);
  return Number.isSafeInteger(id) && id > 0 ? id : undefined;
}

export function resolveCustomerContext(user: UserProfile | null | undefined): EffectiveCustomerContext {
  const context = user?.customer_context;
  const contextOrgID = positiveOrganizationID(context?.org_id);

  if (context?.state === 'ready') {
    const adoptedDeploymentRoot = isPlatformAdminRole(organizationRoleForUser(user)) &&
      positiveOrganizationID(user?.org_id) === contextOrgID &&
      user?.identity_scope === 'deployment_operator_and_customer';
    return contextOrgID
      ? { state: 'ready', orgID: contextOrgID, orgName: adoptedDeploymentRoot ? t('当前部署') : context.org_name?.trim() || undefined }
      : { state: 'unavailable' };
  }
  if (context?.state === 'not_initialized') return { state: 'not_initialized' };
  if (context?.state === 'ambiguous') return { state: 'ambiguous' };
  if (context?.state === 'inactive') {
    return { state: 'inactive', orgID: contextOrgID, orgName: context.org_name?.trim() || undefined };
  }
  if (context?.state === 'unavailable') return { state: 'unavailable' };

  // During a rolling upgrade, a customer-scoped identity can continue to use
  // its authenticated tenant. A platform identity cannot: its org_id may be
  // the deployment's technical organization rather than the customer.
  if (!isPlatformAdminRole(organizationRoleForUser(user))) {
    const userOrgID = positiveOrganizationID(user?.org_id);
    if (userOrgID) {
      return { state: 'ready', orgID: userOrgID, orgName: user?.org_name?.trim() || undefined };
    }
  }

  return { state: 'unavailable' };
}

export function effectiveCustomerOrgID(user: UserProfile | null | undefined): number | undefined {
  const context = resolveCustomerContext(user);
  return context.state === 'ready' ? context.orgID : undefined;
}

export function customerOrganizationDisplayName(
  user: UserProfile | null | undefined,
  organizationID?: unknown,
  organizationName?: unknown,
  fallback = t("当前客户"),
): string {
  const context = resolveCustomerContext(user);
  const requestedOrganizationID = positiveOrganizationID(organizationID);
  if (
    context.state === 'ready'
    && context.orgID
    && (!requestedOrganizationID || requestedOrganizationID === context.orgID)
  ) {
    return context.orgName || fallback;
  }

  const requestedOrganizationName = typeof organizationName === 'string'
    ? organizationName.trim()
    : '';
  if (requestedOrganizationName) return requestedOrganizationName;
  return requestedOrganizationID ? t('组织 #{id}', { id: requestedOrganizationID }) : fallback;
}

export function customerContextBlockerCopy(state: Exclude<CustomerContextState, 'ready'>) {
  switch (state) {
    case 'not_initialized':
      return {
		title: t("未找到部署组织"),
		description: t("正常安装会自动采用唯一根组织并补齐默认项目。当前数据库没有可用根组织，请由部署管理员检查安装或恢复状态。"),
      };
    case 'ambiguous':
      return {
        title: t("客户组织上下文不唯一"),
		description: t("检测到多个无法唯一判定的根组织，系统不会猜测目标。请先由部署管理员修复组织基线，再继续客户域操作。"),
      };
    case 'inactive':
      return {
        title: t("客户组织当前未启用"),
        description: t("唯一客户组织已识别，但当前不是 active 状态。恢复客户组织后再继续账号、项目和用量操作。"),
      };
    default:
      return {
        title: t("无法确认客户组织上下文"),
        description: t("当前会话没有可验证的客户组织绑定。系统不会把部署技术组织当作客户组织，请刷新会话或检查部署版本。"),
      };
  }
}
