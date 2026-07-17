import { useState } from 'react';
import { Tabs } from '@douyinfe/semi-ui';
import { PageHeader } from '../ui';
import ModelPricesPage from './ModelPricesPage';
import OrgModelPricesPage from './OrgModelPricesPage';

const { TabPane } = Tabs;

export default function ModelBillingPage() {
  const initialTab = new URLSearchParams(window.location.search).get('tab');
  const [activeTab, setActiveTab] = useState(initialTab === 'org' ? 'org' : 'base');

  const changeTab = (key: string) => {
    setActiveTab(key);
    const params = new URLSearchParams(window.location.search);
    if (key === 'base') params.delete('tab'); else params.set('tab', key);
    const query = params.toString();
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
  };

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="计费与资金"
        title="模型计费"
        description="基础价格定义平台统一计费单位，组织倍率只覆盖特定组织；两者职责独立，并在同一模型计费上下文中维护。"
      />
      <Tabs activeKey={activeTab} onChange={changeTab}>
        <TabPane tab="基础价格" itemKey="base">
          {activeTab === 'base' ? <ModelPricesPage embedded /> : null}
        </TabPane>
        <TabPane tab="组织倍率" itemKey="org">
          {activeTab === 'org' ? <OrgModelPricesPage embedded /> : null}
        </TabPane>
      </Tabs>
    </div>
  );
}
