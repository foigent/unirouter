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
    area: 'Developer Console',
    endpoints: [
      ['GET', '/api/v1/projects', '当前账号可访问项目'],
      ['GET', '/api/v1/keys?project_id={id}', '项目 API Key 列表'],
      ['POST', '/api/v1/keys', '创建项目 API Key'],
      ['GET', '/api/v1/model-catalog?project_id={id}', '项目可用模型目录'],
      ['GET', '/api/v1/logs?project_id={id}', '项目调用日志'],
      ['GET', '/api/v1/logs?project_id={id}&request_id={rid}', '按 Request ID 查询项目请求'],
      ['POST', '/v1/chat/completions', 'Playground 直接使用项目 API Key 调用'],
    ],
  },
  {
    area: 'PRD2 Product',
    endpoints: [
      ['GET', '/api/v1/usage/analytics', '用量钻取聚合与 CSV 导出'],
      ['GET', '/api/v1/internal-status', '内部状态与事件历史'],
      ['GET', '/api/v1/error-docs', '错误码文档和 doc_url 注册表'],
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
      ['GET', '/api/v1/license', 'License 状态'],
      ['PUT', '/api/v1/license', '导入离线 License'],
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
        eyebrow="System"
        title="API 文档"
        description="PRD2 当前可用接口索引。Developer Console 使用同一登录会话和项目级 API，不暴露上游 provider key。"
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
