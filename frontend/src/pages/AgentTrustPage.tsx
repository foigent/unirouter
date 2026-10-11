import '../i18n/security';
import type { MessageKey } from '../i18n/catalog';
import { translate as t, useI18n, labelFor } from '../i18n';
import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Table, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { api, ApiError } from '../api/client';
import { EmptyState, MetricTile, PageHeader } from '../ui';
import { formatInteger } from '../utils/formatNumber';
import ModelAuthorizationWorkspace from './ModelAuthorizationWorkspace';
import { useNavigate } from 'react-router-dom';

interface ArtifactIdentity {
  path: string;
  sha256: string;
}

interface AgentTrustContract {
  contract_version: string;
  report_version: string;
  receipt_version: string;
  sample_request: Record<string, unknown>;
  artifacts: {
    schema: ArtifactIdentity;
    fixtures: ArtifactIdentity[];
    golden_report: ArtifactIdentity;
  };
  authority_boundary: {
    signing_key_accepted: boolean;
    fixture_public_key_runtime_trust: boolean;
    synchronous_runtime_dependency: boolean;
    runtime_enforcement_authorized: boolean;
    runtime_mutation: string;
  };
}

interface AgentLimits {
  max_depth: number;
  max_children: number;
  max_descendants: number;
  cost_budget_cents: number;
  token_budget: number;
  rpm_limit: number;
  rps_limit: number;
  concurrency_limit: number;
  allowed_models: string[];
  allowed_endpoints: string[];
}

interface AuthorityNode {
  agent_ref: string;
  parent_agent_ref: string;
  depth: number;
  state: string;
  lineage: string[];
  effective_limits: AgentLimits;
  remaining_ttl_seconds: string;
  revocation_event_ref: string;
}

interface AgentTrustReport {
  report_version: string;
  review_id: string;
  tenant_ref: string;
  authority_snapshot_hash: string;
  effective_authority_tree: AuthorityNode[];
  attenuation_diffs: Array<{
    agent_ref: string;
    parent_agent_ref: string;
    reduced_fields: string[];
    removed_models: string[];
    removed_endpoints: string[];
  }>;
  ancestor_budgets: Array<{
    agent_ref: string;
    lineage: string[];
    effective_limits: AgentLimits;
    ancestor_budget_refs: string[];
    remaining_ttl_seconds: string;
  }>;
  revocation_timeline: Array<{
    event_ref: string;
    agent_ref: string;
    revoked_at: string;
    reason: string;
  }>;
  digital_twin: {
    overall_risk: string;
    route_changes: unknown[];
    provider_pack_changes: unknown[];
    release_decision: string;
    synchronous_dependency: boolean;
    runtime_mutation: string;
  };
  receipt_verification: {
    receipt_id: string;
    key_id: string;
    key_version: string;
    payload_hash: string;
    cryptographically_valid: boolean;
    tenant_bound: boolean;
    audience_bound: boolean;
    decision_bound: boolean;
    trust_profile: string;
    runtime_trusted: boolean;
  };
  decision: string;
  synchronous_runtime_dependency: boolean;
  runtime_enforcement_authorized: boolean;
  runtime_mutation: string;
}

const stateNames: Record<string, string> = {
  active: '有效',
  expired: '已过期',
  revoked: '已撤销',
  ancestor_revoked: '祖先已失效',
};

const decisionNames: Record<string, string> = {
  allow: '允许',
  shadow_only: '仅影子审查',
  deny: '拒绝',
};

const compactRef = (value: string) => value.length > 24
  ? `${value.slice(0, 15)}…${value.slice(-8)}`
  : value;

export function OfflineAgentTrustWorkspace() {
  const { locale } = useI18n();
  const [contract, setContract] = useState<AgentTrustContract | null>(null);
  const [input, setInput] = useState('');
  const [report, setReport] = useState<AgentTrustReport | null>(null);
  const [selectedRef, setSelectedRef] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<{key: MessageKey; values?: Record<string, string | number>} | null>(null);

  const preview = (body: string) => {
    setLoading(true);
    setError(null);
    return api.previewAgentTrust(body)
      .then((next: AgentTrustReport) => {
        setReport(next);
        setSelectedRef(current => (
          next.effective_authority_tree.some(node => node.agent_ref === current)
            ? current
            : next.effective_authority_tree[0]?.agent_ref || ''
        ));
      })
      .catch((previewError: unknown) => {
        setReport(null);
        setSelectedRef('');
        setError(previewError instanceof ApiError ? {key: '校验失败（{status}）：{detail}', values: {status: previewError.status, detail: previewError.message}} : {key: 'Agent 信任审查不满足严格合同。'});
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    let active = true;
    api.getAgentTrustContract()
      .then((next: AgentTrustContract) => {
        if (!active) return;
        setContract(next);
        const body = JSON.stringify(next.sample_request, null, 2);
        setInput(body);
        void preview(body);
      })
      .catch((loadError: unknown) => {
        if (!active) return;
        setError(loadError instanceof ApiError ? {key: '合同加载失败（{status}）：{detail}', values: {status: loadError.status, detail: loadError.message}} : {key: 'Agent Trust 合同暂不可用。'});
      });
    return () => { active = false; };
  }, []);

  const selectedNode = report?.effective_authority_tree.find(node => node.agent_ref === selectedRef)
    || report?.effective_authority_tree[0]
    || null;
  const selectedBudget = report?.ancestor_budgets.find(item => item.agent_ref === selectedNode?.agent_ref);
  const selectedDiff = report?.attenuation_diffs.find(item => item.agent_ref === selectedNode?.agent_ref);

  const columns = useMemo(() => [
    {
      title: t("深度"),
      dataIndex: 'depth',
      width: 80,
      render: (value: number) => `L${value}`,
    },
    {
      title: 'Agent',
      dataIndex: 'agent_ref',
      width: 250,
      render: (value: string) => <span className="ur-mono-break">{compactRef(value)}</span>,
    },
    {
      title: t("状态"),
      dataIndex: 'state',
      width: 120,
      render: (value: string) => (
        <Tag color={value === 'active' ? 'green' : value === 'revoked' ? 'red' : 'orange'}>
          {labelFor(stateNames, value)}
        </Tag>
      ),
    },
    {
      title: t("剩余 TTL"),
      dataIndex: 'remaining_ttl_seconds',
      render: (value: string) => t('{value} 秒', {value}),
    },
  ], [locale]);

  const restoreFixture = () => {
    if (!contract) return;
    const body = JSON.stringify(contract.sample_request, null, 2);
    setInput(body);
    void preview(body);
  };

  return (
    <div>
      <Banner
        type="info"
        description={t("离线 Agent 能力证据仍保留为人工审查工具：不接收签名私钥，不读取运行时能力，不同步依赖风险服务，也不会晋级策略或执行撤销。Fixture 公钥只证明密码学有效，不是生产信任根。")}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />

      <div className="ur-context-explorer ur-governance-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("高级模型限制合同与离线证据")}</Typography.Title>
              <Typography.Text className="ur-panel-hint">
                {contract ? `${contract.contract_version} · ${contract.receipt_version}` : t("正在读取离线合同")}
              </Typography.Text>
            </div>
            <Tag color="grey">{t("规划中 · metadata-only")}</Tag>
          </div>

          <div className="ur-table-shell">
            <Table
              rowKey="agent_ref"
              columns={columns}
              dataSource={report?.effective_authority_tree || []}
              pagination={false}
              scroll={{ x: 640 }}
              onRow={(node?: AuthorityNode) => ({
                onClick: () => node && setSelectedRef(node.agent_ref),
                'aria-selected': node?.agent_ref === selectedNode?.agent_ref,
              })}
            />
          </div>

          <Typography.Title heading={6}>{t("当前审查 JSON")}</Typography.Title>
          <TextArea
            aria-label={t("Agent Trust 审查 JSON")}
            value={input}
            onChange={value => {
              setInput(value);
              setReport(null);
              setSelectedRef('');
              setError(null);
            }}
            autosize={{ minRows: 10, maxRows: 18 }}
          />
          <div className="ur-governance-actions">
            <Button type="primary" loading={loading} onClick={() => void preview(input)}>{t("运行离线审查")}</Button>
            <Button disabled={!contract} onClick={restoreFixture}>{t("恢复安全示例")}</Button>
          </div>
          {error && <Banner type="danger" description={error ? t(error.key, error.values) : undefined} style={{ marginTop: 12 }} />}

          {report && (
            <div className="ur-kpi-grid ur-governance-kpis">
              <MetricTile label={t("权限节点")} value={formatInteger(report.effective_authority_tree.length)} tone="blue" />
              <MetricTile label={t("衰减证明")} value={formatInteger(report.attenuation_diffs.length)} tone="green" />
              <MetricTile label={t("撤销事件")} value={formatInteger(report.revocation_timeline.length)} tone={report.revocation_timeline.length ? 'amber' : 'blue'} />
              <MetricTile label={t("治理结论")} value={labelFor(decisionNames, report.decision)} tone={report.decision === 'deny' ? 'red' : 'amber'} />
            </div>
          )}
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label={t("Agent Trust 检查器")}>
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">{t("信任证据检查器")}</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("权限与收据证据")}</Typography.Title>
            </div>
            <Tag color={selectedNode?.state === 'active' ? 'green' : 'red'}>
              {selectedNode ? labelFor(stateNames, selectedNode.state) : t("等待审查")}
            </Tag>
          </div>
          {report && selectedNode ? (
            <dl className="ur-financial-truth-detail">
              <dt>Agent</dt><dd className="ur-mono-break">{selectedNode.agent_ref}</dd>
              <dt>{t("父级")}</dt><dd className="ur-mono-break">{selectedNode.parent_agent_ref || 'root'}</dd>
              <dt>{t("深度 / 谱系")}</dt><dd>L{selectedNode.depth} / {formatInteger(selectedNode.lineage.length)}  {t("节点")}</dd>
              <dt>{t("剩余 TTL")}</dt><dd>{selectedNode.remaining_ttl_seconds}  {t("秒")}</dd>
              <dt>{t("祖先预算")}</dt><dd>{formatInteger(selectedBudget?.ancestor_budget_refs.length || 0)}  {t("层共同扣减")}</dd>
              <dt>{t("成本上限")}</dt><dd>{selectedBudget?.effective_limits.cost_budget_cents ?? 0} cents</dd>
              <dt>{t("Token 上限")}</dt><dd>{selectedBudget?.effective_limits.token_budget ?? 0}</dd>
              <dt>{t("并发上限")}</dt><dd>{selectedBudget?.effective_limits.concurrency_limit ?? 0}</dd>
              <dt>{t("衰减字段")}</dt><dd>{selectedDiff?.reduced_fields.join('、') || t("根能力 / 无")}</dd>
              <dt>{t("撤销证明")}</dt><dd className="ur-mono-break">{selectedNode.revocation_event_ref || t("无")}</dd>
              <dt>{t("数字孪生风险")}</dt><dd>{report.digital_twin.overall_risk}</dd>
              <dt>{t("路由 / Pack 差异")}</dt><dd>{report.digital_twin.route_changes.length} / {report.digital_twin.provider_pack_changes.length}</dd>
              <dt>{t("签名验证")}</dt><dd>{report.receipt_verification.cryptographically_valid ? t("密码学有效") : t("无效")}</dd>
              <dt>{t("Key / 版本")}</dt><dd>{report.receipt_verification.key_id} / {report.receipt_verification.key_version}</dd>
              <dt>{t("租户 / Audience")}</dt><dd>{report.receipt_verification.tenant_bound && report.receipt_verification.audience_bound ? t("已绑定") : t("未绑定")}</dd>
              <dt>{t("生产信任根")}</dt><dd>{report.receipt_verification.runtime_trusted ? t("已授权") : t("未授权（fixture）")}</dd>
              <dt>{t("同步运行时依赖")}</dt><dd>{report.synchronous_runtime_dependency ? t("是") : t("否")}</dd>
              <dt>{t("运行时执行授权")}</dt><dd>{report.runtime_enforcement_authorized ? t("是") : t("否")}</dd>
              <dt>{t("运行时变更")}</dt><dd>{report.runtime_mutation}</dd>
              <dt>{t("收据哈希")}</dt><dd className="ur-mono-break">{report.receipt_verification.payload_hash}</dd>
            </dl>
          ) : (
            <EmptyState
              title={t("等待严格审查")}
              description={t("安全示例载入后会自动校验并选中首个 Agent；无需点击展开右侧检查器。")}
              minHeight={260}
            />
          )}
        </aside>
      </div>
    </div>
  );
}

export default function AgentTrustPage() {
  useI18n();
  const navigate = useNavigate();
  return (
    <div className="ur-page ur-page-wide ur-security-workspace">
      <PageHeader
        eyebrow={t("访问与安全")}
        title={t("高级模型限制")}
        description={t("在项目与应用 Key 已经授予访问后，按凭证、项目、人员或 Agent 进一步收窄模型与端点；本页不能为用户开通基础访问。")}
        actions={<Button onClick={() => navigate('/agent-trust-evidence')}>{t("离线合同验证")}</Button>}
      />
      <ModelAuthorizationWorkspace />
    </div>
  );
}
