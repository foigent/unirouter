import { useState } from 'react';
import { Banner, Button, Tabs, Tag, Typography } from '@douyinfe/semi-ui';
import { useSearchParams } from 'react-router-dom';
import DataExchangePage from './DataExchangePage';
import { PageHeader, Surface } from '../ui';

const { TabPane } = Tabs;
const { Text, Title, Paragraph } = Typography;

interface IntegrationCardProps {
  title: string;
  state: string;
  description: string;
  boundary: string;
  action?: string;
}

function IntegrationCard({ title, state, description, boundary, action }: IntegrationCardProps) {
  const statePresentation = state === 'available'
    ? { color: 'blue', label: '可配置' }
    : state === 'configured'
      ? { color: 'green', label: '已配置' }
      : { color: 'orange', label: '规划中' };
  return (
    <Surface className="ur-integration-card">
      <div className="ur-detail-header">
        <Title heading={5} style={{ margin: 0 }}>{title}</Title>
        <Tag color={statePresentation.color as any}>{statePresentation.label}</Tag>
      </div>
      <Paragraph>{description}</Paragraph>
      <dl className="ur-financial-truth-detail">
        <dt>权限边界</dt><dd>{boundary}</dd>
        <dt>运行方式</dt><dd>独立连接服务，不在 UniRouter 模型网关进程内运行</dd>
        <dt>当前动作</dt><dd>{action || '等待具备真实企业环境后验证'}</dd>
      </dl>
    </Surface>
  );
}

export default function IntegrationsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const requested = searchParams.get('tab');
  const [activeTab, setActiveTab] = useState(['xinsight', 'feishu', 'enterprise'].includes(requested || '') ? requested! : 'catalog');

  const changeTab = (key: string) => {
    setActiveTab(key);
    setSearchParams(key === 'catalog' ? {} : { tab: key }, { replace: true });
  };

  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="系统与集成"
        title="集成管理"
        description="统一查看可用或规划中的外部系统连接，以及每项连接能读取和写入什么；外部系统仍独立管理账号、数据和最终操作。"
      />
      <Banner
        type="info"
        description="所有集成都按最小权限隔离运行，不进入模型请求关键路径。连接 X-Insight、飞书或其他企业系统，不会自动获得模型、管理员或财务权限。"
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      <Tabs activeKey={activeTab} onChange={changeTab} type="line">
        <TabPane tab="集成目录" itemKey="catalog">
          <div className="ur-integration-grid">
            <IntegrationCard
              title="X-Insight 执行记录订阅"
              state="available"
              description="向 X-Insight 提供已完成请求的元数据订阅；不包含原始 Prompt、响应、供应商错误或管理员凭证。"
              boundary="UniRouter 只提供来源事实；X-Insight 独立负责存储、分析与调查。"
              action="在“X-Insight”页签查看连接状态和维护凭证"
            />
            <IntegrationCard
              title="飞书企业机器人"
              state="planned"
              description="面向企业群聊与机器人事件的隔离适配器；需要 3 秒内确认、至少一次处理与事件幂等。"
              boundary="机器人凭据与事件处理留在 Adapter；UniRouter 只接收经过身份绑定的模型调用。"
            />
            <IntegrationCard
              title="飞书 Aily"
              state="planned"
              description="需要真实 Aily 租户和应用环境才能冻结精确适配合同，本轮只保留架构位置。"
              boundary="Aily 平台配置和凭据不进入 UniRouter 核心路由、计费或 IAM 扩展入口。"
            />
            <IntegrationCard
              title="企业目录 / HCM / IAM"
              state="planned"
              description="未来对接飞书、钉钉、AD 等权威组织源，支持增量同步、离职停权和有效期证据。"
              boundary="UniRouter 保存映射、有效权限与审计；权威人员和组织事实仍由企业目录拥有。"
            />
          </div>
        </TabPane>
        <TabPane tab="X-Insight" itemKey="xinsight">
          {activeTab === 'xinsight' && <DataExchangePage />}
        </TabPane>
        <TabPane tab="飞书" itemKey="feishu">
          <Surface title="飞书适配准备" description="暂无真实租户，因此不创建凭据、不接入流量、不声明已验证。">
            <div className="ur-readiness-axis-list">
              <div className="ur-readiness-axis"><div><strong>企业机器人事件合同</strong><Tag color="orange">待环境验证</Tag></div><Text>预留事件去重、快速 ACK、异步模型调用、重试与审计边界。</Text></div>
              <div className="ur-readiness-axis"><div><strong>Aily 适配合同</strong><Tag color="orange">待环境验证</Tag></div><Text>仅记录 IntegrationInstance 位置；不根据公开说明猜测客户环境配置。</Text></div>
              <div className="ur-readiness-axis"><div><strong>密钥与隔离</strong><Tag color="green">边界已冻结</Tag></div><Text>飞书 App Secret 不进入 UniRouter 通用前端或网关进程，适配器采用独立最小权限凭据。</Text></div>
            </div>
            <Button disabled>配置飞书实例（需要真实环境）</Button>
          </Surface>
        </TabPane>
        <TabPane tab="企业系统" itemKey="enterprise">
          <Surface title="企业集成目录" description="HCM、AD、飞书/钉钉组织架构与 ERP 只在客户 POC 环境具备后接入。">
            <Text>当前可以创建本地账号、组织、项目与应用；外部组织同步不会被伪装成已实现能力。</Text>
          </Surface>
        </TabPane>
      </Tabs>
    </div>
  );
}
