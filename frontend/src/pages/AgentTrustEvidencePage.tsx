import '../i18n/security';
import { translate as t, useI18n } from '../i18n';
import { Button } from '@douyinfe/semi-ui';
import { useNavigate } from 'react-router-dom';
import { PageHeader } from '../ui';
import { OfflineAgentTrustWorkspace } from './AgentTrustPage';

export default function AgentTrustEvidencePage() {
  useI18n();
  const navigate = useNavigate();
  return (
    <div className="ur-page ur-page-wide ur-security-workspace">
      <PageHeader
        eyebrow={t("模型治理 · 验证工具")}
        title={t("Agent 离线合同验证")}
        description={t("离线验证权限衰减、祖先预算、TTL、撤销和签名收据；fixture 不是生产信任根，本页不授予模型、Tool 或 MCP 运行时权限。")}
        actions={<Button onClick={() => navigate('/agent-trust')}>{t("返回高级模型限制")}</Button>}
      />
      <OfflineAgentTrustWorkspace />
    </div>
  );
}
