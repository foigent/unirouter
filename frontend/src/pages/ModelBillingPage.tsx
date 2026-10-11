import '../i18n/finance';
import { translate as t, useI18n } from '../i18n';
import { useState, type ReactNode } from 'react';
import { Banner, Tabs, Typography } from '@douyinfe/semi-ui';
import { EmptyState, PageHeader, Surface } from '../ui';
import ModelPricesPage from './ModelPricesPage';
import OrgModelPricesPage from './OrgModelPricesPage';
import { EnterpriseRateCardWorkspace } from './EnterpriseFinanceWorkspaces';
import { useAuthStore } from '../store/auth';
import { isBillingRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';

const { TabPane } = Tabs;

export default function ModelBillingPage() {
  useI18n();
  const user = useAuthStore(state => state.user);
  const organizationRole = organizationRoleForUser(user);
  const platform = isPlatformAdminRole(organizationRole);
  const canManageEnterpriseFinance = isBillingRole(organizationRole) || platform;
  const customerContext = resolveCustomerContext(user);
  const customerContextReady = customerContext.state === 'ready';
  const customerOrgID = effectiveCustomerOrgID(user);
  const initialTab = new URLSearchParams(window.location.search).get('tab');
  const [activeTab, setActiveTab] = useState(['base', 'org', 'enterprise'].includes(initialTab || '') ? initialTab! : customerContextReady ? 'enterprise' : 'base');

  const changeTab = (key: string) => {
    setActiveTab(key);
    const params = new URLSearchParams(window.location.search);
    params.set('tab', key);
    const query = params.toString();
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
  };

  const customerScopeContent = (content: ReactNode) => {
    if (customerContextReady) return content;
    const blocker = customerContextBlockerCopy(customerContext.state as Exclude<typeof customerContext.state, 'ready'>);
    return (
      <Surface>
        <EmptyState title={blocker.title} description={t("{v0} 客户倍率和企业 Rate Card 不会读取或写入任何组织。", { v0: String(blocker.description) })} minHeight={260} />
      </Surface>
    );
  };

  return (
    <div className="ur-page ur-page-wide ur-finance-workspace">
      <PageHeader
        eyebrow={t("成本与财务")}
        title={t("费率与计价")}
        description={t("Rate Card 严格模式启用后，版本化企业 Rate Card 才是请求计价的唯一权威；当前实际权威以企业 Rate Card 页的状态卡为准。")}
        actions={platform && customerContextReady ? (
          <Typography.Text type="tertiary">{t("客户范围：")}{customerContext.orgName || t("组织 #{v0}", { v0: String(customerOrgID) })}</Typography.Text>
        ) : undefined}
      />
      <details className="ur-workspace-notes ur-workspace-guidance">
        <summary>{t('计价规则与兼容性')}</summary>
      <Banner
        type="info"
        description={t("严格模式启用后，只有完成独立复核并激活的 Rate Card 版本才参与请求计价；草稿和已批准但未激活版本均不生效。旧价格兼容模式仍使用原有价格权威；切换前必须通过预检。")}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      </details>
      <Tabs activeKey={activeTab} onChange={changeTab}>
        <TabPane tab={t("企业 Rate Card")} itemKey="enterprise">
          {activeTab === 'enterprise' ? customerScopeContent(
            <EnterpriseRateCardWorkspace orgId={customerOrgID} canManage={canManageEnterpriseFinance} />,
          ) : null}
        </TabPane>
        <TabPane tab={t("供应商基础费率")} itemKey="base">
          {activeTab === 'base' ? (
            <>
              <Banner type="warning" description={t("这里是全局基础费率兼容视图。当前请求是否仍使用它取决于企业 Rate Card 页显示的计价模式；严格模式启用后，请求只使用已激活的版本化 Rate Card。")} closeIcon={null} style={{ marginBottom: 16 }} />
              <ModelPricesPage embedded onOpenRateCard={() => changeTab('enterprise')} />
            </>
          ) : null}
        </TabPane>
        <TabPane tab={t("组织倍率（兼容视图）")} itemKey="org">
          {activeTab === 'org' ? customerScopeContent(<OrgModelPricesPage embedded />) : null}
        </TabPane>

      </Tabs>
    </div>
  );
}
