import { isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import type { UserProfile } from '../store/auth';

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
    return contextOrgID
      ? { state: 'ready', orgID: contextOrgID, orgName: context.org_name?.trim() || undefined }
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

export function customerContextBlockerCopy(state: Exclude<CustomerContextState, 'ready'>) {
  switch (state) {
    case 'not_initialized':
      return {
        title: '尚未初始化客户组织',
        description: '当前部署还没有唯一客户组织。完成首客户初始化后，账号、项目和用量页面才会绑定客户范围。',
      };
    case 'ambiguous':
      return {
        title: '客户组织上下文不唯一',
        description: '检测到多个客户组织，系统不会猜测目标。请先由部署管理员修复组织基线，再继续客户域操作。',
      };
    case 'inactive':
      return {
        title: '客户组织当前未启用',
        description: '唯一客户组织已识别，但当前不是 active 状态。恢复客户组织后再继续账号、项目和用量操作。',
      };
    default:
      return {
        title: '无法确认客户组织上下文',
        description: '当前会话没有可验证的客户组织绑定。系统不会把部署技术组织当作客户组织，请刷新会话或检查部署版本。',
      };
  }
}
