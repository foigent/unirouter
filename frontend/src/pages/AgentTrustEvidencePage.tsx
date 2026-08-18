import { Button } from '@douyinfe/semi-ui';
import { useNavigate } from 'react-router-dom';
import { PageHeader } from '../ui';
import { OfflineAgentTrustWorkspace } from './AgentTrustPage';

export default function AgentTrustEvidencePage() {
  const navigate = useNavigate();
  return (
    <div className="ur-page ur-page-wide">
      <PageHeader
        eyebrow="模型治理 · 验证工具"
        title="Agent 离线合同验证"
        description="离线验证权限衰减、祖先预算、TTL、撤销和签名收据；fixture 不是生产信任根，本页不授予模型、Tool 或 MCP 运行时权限。"
        actions={<Button onClick={() => navigate('/agent-trust')}>返回高级模型限制</Button>}
      />
      <OfflineAgentTrustWorkspace />
    </div>
  );
}
