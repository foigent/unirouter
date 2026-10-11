import '../i18n/developer';
import { translate as t, useI18n } from '../i18n';
import { useMemo } from 'react';
import { Banner, Button, Space, Table, Typography } from '@douyinfe/semi-ui';
import { StatusTag, Surface } from '../ui';
import { parseChannelModels } from '../utils/channelReadiness';

const { Text } = Typography;

export interface ModelConnectionChannel {
  id: number;
  name: string;
  provider: string;
  models: string;
  status: string;
  effective_status?: string;
  published_models?: string;
  routing_epoch: number;
}

interface ConnectionModelPanelProps {
  channel: ModelConnectionChannel;
  modelsPublishedElsewhere: Set<string>;
  onOpenRouteManagement: (model: string, intent?: 'replace' | 'recover') => void;
  onDiagnose: (model: string) => void;
}

export function modelIsPublished(channel: Pick<ModelConnectionChannel, 'published_models'>, model: string) {
  return parseChannelModels(channel.published_models).includes(model);
}

function effectiveConnectionStatus(channel: ModelConnectionChannel) {
  return channel.effective_status || channel.status;
}

export default function ConnectionModelPanel({
  channel,
  modelsPublishedElsewhere,
  onOpenRouteManagement,
  onDiagnose,
}: ConnectionModelPanelProps) {
  useI18n();
  const models = useMemo(() => parseChannelModels(channel.models), [channel.models]);
  const connectionStatus = effectiveConnectionStatus(channel);

  const columns = [
    {
      title: t("模型"),
      dataIndex: 'model',
      key: 'model',
      width: 240,
      render: (model: string) => <Text code>{model}</Text>,
    },
    {
      title: t("旧投影记录"),
      key: 'legacy-state',
      width: 250,
      render: (_: unknown, row: { model: string }) => {
        const publishedHere = modelIsPublished(channel, row.model);
        if (publishedHere && connectionStatus !== 'active') {
          return <StatusTag status="failed">{t("发布记录保留，连接不可用")}</StatusTag>;
        }
        if (publishedHere) return <StatusTag status="warning">{t("存在历史发布记录")}</StatusTag>;
        if (modelsPublishedElsewhere.has(row.model)) return <StatusTag status="warning">{t("其他连接有历史发布记录")}</StatusTag>;
        return <StatusTag status="pending">{t("无历史发布记录")}</StatusTag>;
      },
    },
    {
      title: t("操作"),
      key: 'actions',
      render: (_: unknown, row: { model: string }) => {
        const publishedHere = modelIsPublished(channel, row.model);
        const intent = publishedHere && connectionStatus !== 'active'
          ? 'recover'
          : modelsPublishedElsewhere.has(row.model)
            ? 'replace'
            : undefined;
        return <Space wrap>
          <Button type="primary" size="small" onClick={() => onOpenRouteManagement(row.model, intent)}>

            {t("前往模型发布工作台")}
          </Button>
          <Button size="small" type="tertiary" onClick={() => onDiagnose(row.model)}>

            {t("查看只读诊断")}
          </Button>
        </Space>;
      },
    },
  ];

  return (
    <Surface
      title={t("{v0} · 历史连接模型", { v0: String(channel.name) })}
      description={t("此面板只保留迁移期只读信息；模型执行写入统一由 canonical 模型部署与发布工作台完成。")}
    >
      <Banner
        type="warning"
        description={t("旧连接检测、端点启用和凭证写入口已退役并固定返回 410。这里的旧投影仅供迁移审计，不代表 canonical 当前路由。")}
        closeIcon={null}
        style={{ marginBottom: 12 }}
      />
      <Table
        dataSource={models.map(model => ({ model }))}
        columns={columns}
        rowKey="model"
        pagination={false}
        empty={t("当前连接没有历史模型记录")}
      />
    </Surface>
  );
}
