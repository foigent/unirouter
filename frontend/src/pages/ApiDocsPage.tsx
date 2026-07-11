import { useEffect, useState } from 'react';
import { Typography, Table, Tag } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { PageHeader, Surface } from '../ui';

const { Text } = Typography;

const endpointGroups = [
  {
    area: 'Auth',
    endpoints: [
      ['POST', '/api/v1/auth/login', '登录并返回 JWT'],
      ['GET', '/api/v1/auth/oidc/config', 'OIDC 登录入口配置'],
      ['GET', '/api/v1/auth/oidc/start', 'OIDC 登录跳转地址'],
      ['GET', '/api/v1/auth/oidc/callback', 'OIDC 回调处理'],
      ['GET', '/api/v1/me', '当前登录用户信息'],
      ['PUT', '/api/v1/me/password', '修改当前用户密码'],
    ],
  },
  {
    area: 'Platform',
    endpoints: [
      ['GET', '/', '服务元信息'],
      ['GET', '/docs', '后端 API 合同索引'],
      ['GET', '/health/live', '存活探针'],
      ['GET', '/health/ready', '依赖就绪探针'],
      ['GET', '/metrics', 'Prometheus 指标（运行时可配置）'],
    ],
  },
  {
    area: 'Gateway',
    endpoints: [
      ['GET', '/v1/models', '兼容 OpenAI 协议的模型列表'],
      ['POST', '/v1/chat/completions', '兼容 OpenAI 协议的对话补全'],
      ['POST', '/v1/embeddings', '兼容 OpenAI 协议的向量接口'],
    ],
  },
  {
    area: '开发者工作台',
    endpoints: [
      ['GET', '/api/v1/projects', '当前账号可访问项目'],
      ['GET', '/api/v1/projects/{id}', '项目详情'],
      ['GET', '/api/v1/projects/{id}/model-sets', '项目模型集合绑定'],
      ['GET', '/api/v1/keys?project_id={id}', '项目访问凭证列表'],
      ['GET', '/api/v1/keys/{id}', '访问凭证详情'],
      ['POST', '/api/v1/keys', '创建项目访问凭证'],
      ['PUT', '/api/v1/keys/{id}', '更新访问凭证'],
      ['POST', '/api/v1/keys/{id}/disable', '禁用访问凭证'],
      ['GET', '/api/v1/model-catalog?project_id={id}', '项目可用模型目录'],
      ['GET', '/api/v1/logs?project_id={id}', '项目调用日志'],
      ['GET', '/api/v1/logs/{id}', '请求日志详情'],
      ['GET', '/api/v1/logs/{id}/trace', '请求路径事件'],
      ['GET', '/api/v1/logs?project_id={id}&request_id={rid}', '按 Request ID 查询项目请求'],
      ['POST', '/v1/chat/completions', '请求调试台直接使用项目访问凭证调用'],
    ],
  },
  {
    area: 'PRD2 Product',
    endpoints: [
      ['GET', '/api/v1/usage/analytics', '用量钻取聚合与 CSV 导出'],
      ['GET', '/api/v1/internal-status', '内部状态与事件历史'],
      ['GET', '/api/v1/runtime/status', '运行时、容量与外部监控状态'],
      ['GET', '/api/v1/error-docs', '错误码文档和 doc_url 注册表'],
    ],
  },
  {
    area: 'Admin',
    endpoints: [
      ['GET', '/api/v1/dashboard/stats', 'Dashboard 聚合数据'],
      ['GET', '/api/v1/channels', '上游通道列表'],
      ['POST', '/api/v1/channels', '创建上游通道'],
      ['GET', '/api/v1/channels/{id}', '上游通道详情'],
      ['PUT', '/api/v1/channels/{id}', '更新上游通道'],
      ['DELETE', '/api/v1/channels/{id}', '删除上游通道'],
      ['POST', '/api/v1/channels/{id}/test', '测试上游通道'],
      ['GET', '/api/v1/channel-keys', '上游凭证列表'],
      ['POST', '/api/v1/channel-keys', '创建上游凭证'],
      ['PUT', '/api/v1/channel-keys/{id}', '更新上游凭证'],
      ['DELETE', '/api/v1/channel-keys/{id}', '删除上游凭证'],
      ['GET', '/api/v1/keys', '访问凭证列表'],
      ['POST', '/api/v1/projects', '创建项目'],
      ['PUT', '/api/v1/projects/{id}', '更新项目'],
      ['PUT', '/api/v1/projects/{id}/model-sets', '更新项目模型集合绑定'],
      ['GET', '/api/v1/logs', '请求日志查询与筛选'],
      ['GET', '/api/v1/logs/{id}/trace', '请求路径事件'],
      ['GET', '/api/v1/wallet', '资金账户信息'],
      ['POST', '/api/v1/wallet/topup', '资金账户充值'],
      ['POST', '/api/v1/wallet/adjust', '资金账户调整'],
      ['GET', '/api/v1/wallet/transactions', '资金流水列表'],
      ['GET', '/api/v1/wallet/transactions/export', '资金流水导出'],
      ['GET', '/api/v1/grants', '授信记录列表'],
      ['POST', '/api/v1/grants', '创建授信记录'],
      ['GET', '/api/v1/audit-logs', '审计日志'],
      ['GET', '/api/v1/data-exchange/items', '数据交换审批候选'],
      ['POST', '/api/v1/data-exchange/items', '创建审批候选或受控场景授权'],
      ['PUT', '/api/v1/data-exchange/items/{id}', '更新审批状态'],
      ['GET', '/api/v1/data-exchange/exchanges', '出站交换账本与最新回执'],
      ['POST', '/api/v1/data-exchange/exchanges', '登记已生成的交换制品'],
      ['GET', '/api/v1/data-exchange/exchanges/{id}/activity', '查看候选、交付和回执历史'],
      ['POST', '/api/v1/data-exchange/exchanges/{id}/deliveries', '登记交付或重投'],
      ['POST', '/api/v1/data-exchange/exchanges/{id}/receipts', '登记 X-Insight 回执'],
      ['POST', '/api/v1/data-exchange/exchanges/{id}/artifact-delete', '标记制品已删除'],
      ['GET', '/api/v1/settings', '部署级设置（仅平台管理员）'],
      ['PUT', '/api/v1/settings/{key}', '更新部署级设置（仅平台管理员）'],
      ['GET', '/api/v1/license', 'License 状态（仅平台管理员）'],
      ['PUT', '/api/v1/license', '导入离线 License（仅平台管理员）'],
      ['GET', '/api/v1/users', '用户列表'],
      ['POST', '/api/v1/users', '创建用户'],
      ['PUT', '/api/v1/users/{id}', '更新用户'],
      ['DELETE', '/api/v1/users/{id}', '删除用户'],
      ['GET', '/api/v1/sensitive-words', '全局输入阻断词库（仅平台管理员）'],
      ['POST', '/api/v1/sensitive-words', '新增全局输入阻断词（仅平台管理员）'],
      ['PUT', '/api/v1/sensitive-words', '替换全局输入阻断词（仅平台管理员）'],
      ['DELETE', '/api/v1/sensitive-words', '删除全局输入阻断词（仅平台管理员）'],
    ],
  },
  {
    area: 'Model Governance',
    endpoints: [
      ['GET', '/api/v1/model-prices', '查看基础模型价格（平台/组织管理员）'],
      ['POST', '/api/v1/model-prices', '创建或覆盖基础模型价格（仅平台管理员）'],
      ['PUT', '/api/v1/model-prices/{id}', '更新基础模型价格（仅平台管理员）'],
      ['DELETE', '/api/v1/model-prices/{id}', '删除基础模型价格（仅平台管理员）'],
      ['GET', '/api/v1/model-names', '模型名称列表'],
      ['GET', '/api/v1/model-registry', '本组织模型注册表；平台管理员可显式选择组织'],
      ['POST', '/api/v1/model-registry', '在本组织创建模型注册记录；跨组织请求返回 403'],
      ['PUT', '/api/v1/model-registry/{id}', '更新本组织记录；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['GET', '/api/v1/model-catalog', '组织模型目录；project_id 决定组织，项目角色必须提供 project_id'],
      ['GET', '/api/v1/model-aliases', '本组织模型别名；平台管理员可显式选择组织'],
      ['POST', '/api/v1/model-aliases', '在本组织创建模型别名；跨组织请求返回 403'],
      ['PUT', '/api/v1/model-aliases/{id}', '更新本组织别名；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['GET', '/api/v1/model-sets', '本组织模型集合；平台管理员可显式选择组织'],
      ['POST', '/api/v1/model-sets', '原子创建本组织模型集合及其全部初始条目'],
      ['GET', '/api/v1/model-sets/{id}/items', '读取本组织模型集合条目；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['PUT', '/api/v1/model-sets/{id}/items', '原子替换本组织模型集合条目；不会暴露部分结果'],
      ['GET', '/api/v1/org-model-prices', '查看本组织计费倍率；平台可显式选择组织'],
      ['POST', '/api/v1/org-model-prices', '创建或覆盖组织计费倍率（仅平台管理员）'],
      ['PUT', '/api/v1/org-model-prices/{id}', '更新组织计费倍率（仅平台管理员）'],
      ['DELETE', '/api/v1/org-model-prices/{id}', '删除组织计费倍率（仅平台管理员）'],
      ['GET', '/api/v1/reconciliation', '用量计量对账'],
    ],
  },
  {
    area: 'Notifications',
    endpoints: [
      ['GET', '/api/v1/notification-events', '通知事件列表'],
      ['GET', '/api/v1/notification-deliveries', '通知投递列表'],
      ['GET', '/api/v1/notification-webhooks', '通知 Webhook 列表'],
      ['POST', '/api/v1/notification-webhooks', '创建通知 Webhook'],
      ['PUT', '/api/v1/notification-webhooks/{id}', '更新通知 Webhook'],
      ['POST', '/api/v1/notification-webhooks/{id}/verify-scope', '确认隔离 Webhook 的组织/项目/目标范围'],
    ],
  },
];

const columns = [
  { title: 'Method', dataIndex: 'method', width: 100, render: (v: string) => <Tag color={v === 'GET' ? 'blue' : v === 'POST' ? 'green' : 'orange'}>{v}</Tag> },
  { title: 'Path', dataIndex: 'path', width: 280, render: (v: string) => <Text code>{v}</Text> },
  { title: '说明', dataIndex: 'description' },
];

export default function ApiDocsPage() {
  const [errorDocs, setErrorDocs] = useState<any[]>([]);

  useEffect(() => {
    api.getErrorDocs()
      .then((data: any) => setErrorDocs(data?.data || []))
      .catch(() => setErrorDocs([]));
  }, []);

  const errorColumns = [
    { title: 'Code', dataIndex: 'code', width: 250, render: (value: string) => <Text id={`error-${value}`} code>{value}</Text> },
    { title: 'HTTP', dataIndex: 'http_status', width: 90 },
    { title: '说明', dataIndex: 'summary' },
    { title: '处理建议', dataIndex: 'resolution' },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="系统治理"
        title="API 文档"
        description="当前 API 合同索引。开发者工作台使用同一登录会话和项目级 API，不暴露上游凭证。"
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {endpointGroups.map(group => (
          <Surface key={group.area} title={group.area}>
            <div className="ur-table-shell">
              <Table
                columns={columns}
                dataSource={group.endpoints.map(([method, path, description]) => ({
                  key: `${method}:${path}`,
                  method,
                  path,
                  description,
                }))}
                pagination={false}
                rowKey="key"
                size="small"
              />
            </div>
          </Surface>
        ))}
        <Surface title="Error Docs" description="Gateway 错误响应中的 doc_url 指向本表锚点。">
          <div className="ur-table-shell">
            <Table
              columns={errorColumns}
              dataSource={errorDocs}
              pagination={false}
              rowKey="code"
              size="small"
            />
          </div>
        </Surface>
      </div>
    </div>
  );
}
