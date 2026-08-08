import { useEffect, useState } from 'react';
import { Banner, Select, Space, Tabs, Toast, Typography } from '@douyinfe/semi-ui';
import { PageHeader } from '../ui';
import ModelPricesPage from './ModelPricesPage';
import OrgModelPricesPage from './OrgModelPricesPage';
import { EnterpriseRateCardWorkspace } from './EnterpriseFinanceWorkspaces';
import { useAuthStore } from '../store/auth';
import { isBillingRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { api } from '../api/client';
import { loadAllPages } from '../utils/loadAllPages';

const { TabPane } = Tabs;

export default function ModelBillingPage() {
	const user = useAuthStore(state => state.user);
	const organizationRole = organizationRoleForUser(user);
	const platform = isPlatformAdminRole(organizationRole);
	const canManageEnterpriseFinance = isBillingRole(organizationRole) || platform;
	const [organizations, setOrganizations] = useState<Array<{ id: number; name: string }>>([]);
	const [selectedOrgID, setSelectedOrgID] = useState<number | undefined>(user?.org_id);
  const initialTab = new URLSearchParams(window.location.search).get('tab');
	const [activeTab, setActiveTab] = useState(['org', 'enterprise'].includes(initialTab || '') ? initialTab! : 'base');

  const changeTab = (key: string) => {
    setActiveTab(key);
    const params = new URLSearchParams(window.location.search);
    if (key === 'base') params.delete('tab'); else params.set('tab', key);
    const query = params.toString();
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
  };

	useEffect(() => {
		if (!platform) return;
		loadAllPages<{ id: number; name: string }>((page, size) => api.getOrganizations(page, size))
			.then(setOrganizations)
			.catch((error: any) => Toast.error(`组织列表加载失败${error?.message ? `：${error.message}` : ''}`));
	}, [platform]);

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="成本与财务"
        title="费率与计价"
        description="管理当前请求结算使用的供应商基础费率与组织倍率，并明确区分目录价、合同价、内部计价和最终账单事实。"
		actions={platform && organizations.length > 0 ? (
		  <Space>
			<Typography.Text id="rate-card-organization-label" type="tertiary">企业 Rate Card 范围</Typography.Text>
			<Select
			  aria-labelledby="rate-card-organization-label"
			  value={selectedOrgID}
			  optionList={organizations.map(item => ({ value: item.id, label: `${item.name}（ID ${item.id}）` }))}
			  onChange={value => setSelectedOrgID(Number(value))}
			  filter
			  style={{ width: 240 }}
			/>
		  </Space>
		) : undefined}
      />
      <Banner
        type="info"
        description="基础价格和组织倍率继续服务现有请求结算。企业 Rate Card 已支持经济层分离、版本、审批、有效期冲突与不可变计价证据，但尚未接入请求路径，因此不会追溯改写历史结算。"
        style={{ marginBottom: 16 }}
      />
      <Tabs activeKey={activeTab} onChange={changeTab}>
        <TabPane tab="供应商基础费率" itemKey="base">
          {activeTab === 'base' ? <ModelPricesPage embedded /> : null}
        </TabPane>
        <TabPane tab="组织倍率（兼容）" itemKey="org">
          {activeTab === 'org' ? <OrgModelPricesPage embedded /> : null}
        </TabPane>
		<TabPane tab="企业 Rate Card" itemKey="enterprise">
		  {activeTab === 'enterprise' ? (
			<EnterpriseRateCardWorkspace orgId={selectedOrgID} canManage={canManageEnterpriseFinance} />
		  ) : null}
		</TabPane>
      </Tabs>
    </div>
  );
}
