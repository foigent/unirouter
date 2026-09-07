import { useState } from 'react';
import { Banner, Input, Space, Typography } from '@douyinfe/semi-ui';
import { Surface } from '../../ui';
import { quotePosixArg } from '../../utils/shell';

export default function ModelClientSetup({ model, active, chat }: { model: string; active: boolean; chat: boolean }) {
  const [baseURL, setBaseURL] = useState(`${window.location.origin}/v1`);
  const [provider, setProvider] = useState('unirouter');
  let validURL = false;
  try { const u = new URL(baseURL); validURL = ['https:', 'http:'].includes(u.protocol) && !u.username && !u.password && !u.search && !u.hash; } catch { /* Incomplete user input. */ }
  const validProvider = /^[a-zA-Z0-9_-]{1,80}$/.test(provider);
  const setup = JSON.stringify({ baseURL, apiKey: '<UNIROUTER_APPLICATION_KEY>', model }, null, 2);
  const sync = `uniroute-openclaw-sync --base-url ${quotePosixArg(baseURL)} --provider ${quotePosixArg(provider)} --api-key-env UNIROUTER_API_KEY`;
  return <Surface title="客户端接入" description="使用 UniRouter 网关地址、应用 Key 和稳定模型名。应用 Key 需要包含此模型的访问权限。">
    <Space vertical align="start" style={{ width: '100%' }}>
      {!active && <Banner type="info" closeIcon={null} description="尚未启用稳定发布。先完成验证、批准与发布；下方配置不代表已经可以调用。"/>}
      <Typography.Text>网关地址默认取当前页面入口；若客户端使用其他正式入口，请在复制前修改。</Typography.Text>
      <Input aria-label="客户端网关 Base URL" value={baseURL} onChange={setBaseURL}/>
      {chat && validURL && <div><pre className="ur-dev-code">{setup}</pre><Typography.Text copyable={{ content: setup }}>复制客户端配置</Typography.Text></div>}
      {!validURL && <Typography.Text type="warning">请输入不含账号、密码、查询参数的 HTTP(S) 网关地址。</Typography.Text>}
      {chat ? <>
        <Typography.Text>OpenClaw：先用上面的地址和应用 Key 配置一个 Provider，再同步其模型参数。将应用 Key 安全注入 UNIROUTER_API_KEY 环境变量；这里不会读取或保存 Key。</Typography.Text>
        <Input aria-label="已有 OpenClaw Provider 名称" value={provider} onChange={setProvider}/>
        {validURL && validProvider && <div><pre className="ur-dev-code">{sync}</pre><Typography.Text copyable={{ content: sync }}>复制同步命令</Typography.Text></div>}
        <Typography.Text type="tertiary">命令默认仅预览；核对结果后加 --write 写入所选 Provider 的模型配置。输出参数和思考选项由应用 Key 可见的实际合同生成，不从本页批准状态推断，也不切换全局默认模型。发送前可用 /v1/chat/completions/preflight 检查实际请求。</Typography.Text>
      </> : <Typography.Text>此模型使用原生协议入口，请在 API 文档中选择对应协议；不要套用 Chat Completions 配置。</Typography.Text>}
    </Space>
  </Surface>;
}
