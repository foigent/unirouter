import '../i18n/integrations';
import { translate as t, useI18n } from '../i18n';
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
  onAction?: () => void;
}

function IntegrationCard({ title, state, description, boundary, action, onAction }: IntegrationCardProps) {
  useI18n();
  const statePresentation = state === 'available'
    ? { color: 'blue', label: t("可配置") }
    : state === 'configured'
      ? { color: 'green', label: t("已配置") }
      : { color: 'orange', label: t("规划中") };
  return (
    <Surface className="ur-integration-card">
      <div className="ur-detail-header">
        <Title heading={5} style={{ margin: 0 }}>{title}</Title>
        <Tag color={statePresentation.color as any}>{statePresentation.label}</Tag>
      </div>
      <Paragraph>{description}</Paragraph>
      <dl className="ur-financial-truth-detail">
        <dt>{t("权限边界")}</dt><dd>{boundary}</dd>
        <dt>{t("运行方式")}</dt><dd>{state === 'planned' ? t('规划中的连接方式；尚未验证运行。') : t('异步连接，不参与模型请求的同步执行路径')}</dd>
        <dt>{t("当前动作")}</dt><dd>{action || t("等待具备真实企业环境后验证")}</dd>
      </dl>
      {onAction && <Button type="primary" onClick={onAction}>{t('查看接入与通讯状态')}</Button>}
    </Surface>
  );
}

export default function IntegrationsPage() {
  useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const requested = searchParams.get('tab');
  const [activeTab, setActiveTab] = useState(['xinsight', 'feishu', 'enterprise'].includes(requested || '') ? requested! : 'catalog');

  const changeTab = (key: string) => {
    setActiveTab(key);
    setSearchParams(key === 'catalog' ? {} : { tab: key }, { replace: true });
  };

  return (
    <div className="ur-page ur-page-wide ur-integration-workspace">
      <PageHeader
        eyebrow={t("系统与集成")}
        title={t("集成管理")}
        description={t("统一查看可用或规划中的外部系统连接，以及每项连接能读取和写入什么；外部系统仍独立管理账号、数据和最终操作。")}
      />
      <Banner
        type="info"
        description={t("集成须遵守最小权限，不应进入模型请求的同步执行路径。连接 X-Insight、飞书或其他企业系统，不会自动获得模型、管理员或财务权限。")}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      <Tabs activeKey={activeTab} onChange={changeTab} type="line">
        <TabPane tab={t("集成目录")} itemKey="catalog">
          <div className="ur-integration-catalog">
            <IntegrationCard
              title={t("X-Insight 执行记录订阅")}
              state="available"
              description={t("在 Foigent 中，UniRouter 管理执行、用量与费用事实；X-Insight 异步接收已授权事实，用于证据分析与调查。完成请求摘要仅含元数据；来源类别和正文副本另行明确授权。")}
              boundary={t("UniRouter 提供来源事实，X-Insight 独立管理证据。接入需要已部署的受信 X-Insight、当前项目权限和两端确认；可配置不代表公开托管服务或已经连接。")}
              action={t("在“X-Insight”页签确认接入并查看同步状态")}
              onAction={() => changeTab('xinsight')}
            />
            <details className="ur-technical-details">
              <summary>{t('规划中的集成（3）')}</summary>
              <div className="ur-integration-grid">
            <IntegrationCard
              title={t("飞书企业机器人")}
              state="planned"
              description={t("面向企业群聊与机器人事件的隔离适配器；需要 3 秒内确认、至少一次处理与事件幂等。")}
              boundary={t("机器人凭据与事件处理留在 Adapter；UniRouter 只接收经过身份绑定的模型调用。")}
            />
            <IntegrationCard
              title={t("飞书 Aily")}
              state="planned"
              description={t("需要真实 Aily 租户和应用环境才能冻结精确适配合同，本轮只保留架构位置。")}
              boundary={t("Aily 平台配置和凭据不进入 UniRouter 核心路由、计费或 IAM 扩展入口。")}
            />
            <IntegrationCard
              title={t("企业目录 / HCM / IAM")}
              state="planned"
              description={t("未来对接飞书、钉钉、AD 等权威组织源，支持增量同步、离职停权和有效期证据。")}
              boundary={t("UniRouter 保存映射、有效权限与审计；权威人员和组织事实仍由企业目录拥有。")}
            />
              </div>
            </details>
          </div>
        </TabPane>
        <TabPane tab="X-Insight" itemKey="xinsight">
          {activeTab === 'xinsight' && <DataExchangePage />}
        </TabPane>
        <TabPane tab={t("飞书")} itemKey="feishu">
          <Surface title={t("飞书适配准备")} description={t("暂无真实租户，因此不创建凭据、不接入流量、不声明已验证。")}>
            <div className="ur-readiness-axis-list">
              <div className="ur-readiness-axis"><div><strong>{t("企业机器人事件合同")}</strong><Tag color="orange">{t("待环境验证")}</Tag></div><Text>{t("预留事件去重、快速 ACK、异步模型调用、重试与审计边界。")}</Text></div>
              <div className="ur-readiness-axis"><div><strong>{t("Aily 适配合同")}</strong><Tag color="orange">{t("待环境验证")}</Tag></div><Text>{t("仅记录 IntegrationInstance 位置；不根据公开说明猜测客户环境配置。")}</Text></div>
              <div className="ur-readiness-axis"><div><strong>{t("密钥与隔离")}</strong><Tag color="green">{t("边界已冻结")}</Tag></div><Text>{t("飞书 App Secret 不进入 UniRouter 通用前端或网关进程，适配器采用独立最小权限凭据。")}</Text></div>
            </div>
            <Button disabled>{t("配置飞书实例（需要真实环境）")}</Button>
          </Surface>
        </TabPane>
        <TabPane tab={t("企业系统")} itemKey="enterprise">
          <Surface title={t("企业集成目录")} description={t("HCM、AD、飞书/钉钉组织架构与 ERP 只在客户 POC 环境具备后接入。")}>
            <Text>{t("当前可以创建本地账号、组织、项目与应用；外部组织同步不会被伪装成已实现能力。")}</Text>
          </Surface>
        </TabPane>
      </Tabs>
    </div>
  );
}
