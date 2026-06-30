import { Typography, Table, Tag } from '@douyinfe/semi-ui';
import { PageHeader, Surface } from '../ui';

const { Text } = Typography;

const endpointGroups = [
  {
    area: 'Auth',
    endpoints: [
      ['POST', '/api/v1/auth/login', '管理员登录并返回 JWT'],
      ['GET', '/api/v1/me', '当前登录用户信息'],
      ['PUT', '/api/v1/me/password', '修改当前用户密码'],
    ],
  },
  {
    area: 'Gateway',
    endpoints: [
      ['GET', '/v1/models', 'OpenAI-compatible 模型列表'],
      ['POST', '/v1/chat/completions', 'OpenAI-compatible Chat Completions'],
      ['POST', '/v1/embeddings', 'OpenAI-compatible Embeddings'],
    ],
  },
  {
    area: 'Admin',
    endpoints: [
      ['GET', '/api/v1/dashboard/stats', 'Dashboard 聚合数据'],
      ['GET', '/api/v1/channels', '渠道列表'],
      ['GET', '/api/v1/keys', 'API Key 列表'],
      ['GET', '/api/v1/logs', '调用日志查询与筛选'],
      ['GET', '/api/v1/wallet', '钱包信息'],
      ['GET', '/api/v1/audit-logs', '审计日志'],
      ['GET', '/api/v1/settings', '系统设置'],
    ],
  },
];

const columns = [
  { title: 'Method', dataIndex: 'method', width: 100, render: (v: string) => <Tag color={v === 'GET' ? 'blue' : v === 'POST' ? 'green' : 'orange'}>{v}</Tag> },
  { title: 'Path', dataIndex: 'path', width: 280, render: (v: string) => <Text code>{v}</Text> },
  { title: '说明', dataIndex: 'description' },
];

export default function ApiDocsPage() {
  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="System"
        title="API 文档"
        description="PRD1 当前可用接口索引。完整路径快照见仓库内 docs/tech-ref/05-openapi-spec.yaml。"
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {endpointGroups.map(group => (
          <Surface key={group.area} title={group.area}>
            <div className="ur-table-shell">
              <Table
                columns={columns}
                dataSource={group.endpoints.map(([method, path, description]) => ({ method, path, description }))}
                pagination={false}
                rowKey="path"
                size="small"
              />
            </div>
          </Surface>
        ))}
      </div>
    </div>
  );
}
