import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Table, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { api, ApiError } from '../api/client';
import { EmptyState, MetricTile, PageHeader } from '../ui';
import { formatInteger } from '../utils/formatNumber';

interface ArtifactIdentity {
  path: string;
  sha256: string;
}

interface RiskControlContract {
  contract_version: string;
  report_version: string;
  assertion_version: string;
  proposal_version: string;
  policy_version: string;
  sample_request: Record<string, unknown>;
  artifacts: {
    schema: ArtifactIdentity;
    fixtures: ArtifactIdentity[];
    golden_report: ArtifactIdentity;
    dependencies: ArtifactIdentity[];
  };
  authority_boundary: {
    remote_risk_dependency: boolean;
    provider_or_shadow_traffic: boolean;
    runtime_policy_read: boolean;
    runtime_enforcement_authorized: boolean;
    runtime_promotion_authorized: boolean;
    human_review_required: boolean;
    external_decision_gate: string;
    runtime_mutation: string;
  };
}

interface ProposedControl {
  control_id: string;
  target_owner: string;
  proposed_effect: string;
  rationale_code: string;
  evidence_refs: string[];
  enforcement_authorized: boolean;
}

interface RiskControlReport {
  report_version: string;
  review_id: string;
  tenant_ref: string;
  input_semantic_hash: string;
  evidence_binding_hash: string;
  evidence: {
    agent_trust_decision: string;
    agent_trust_report_hash: string;
    receipt_payload_hash: string;
    receipt_cryptographically_valid: boolean;
    receipt_runtime_trusted: boolean;
    provenance_passport_hash: string;
    source_hash: string;
    envelope_hash: string;
    terminal_status: string;
    capture_gap_count: number;
    governance_request_hash: string;
    governance_overall_risk: string;
    source_scope: {
      org_ref: string;
      project_ref: string;
      source_instance: string;
      scope_hash: string;
      tenant_binding_status: string;
    };
  };
  advice: string;
  dimensions: {
    authority: string;
    change: string;
    provenance: string;
    financial: string;
  };
  assertion: {
    assertion_version: string;
    assertion_id: string;
    tenant_ref: string;
    audience: string;
    subject_ref: string;
    issued_at: string;
    expires_at: string;
    risk_epoch: string;
    scope: string;
    advice: string;
    evidence_binding_hash: string;
    authority_effect: string;
    signature_state: string;
    missing_evidence_can_increase_privilege: boolean;
    synchronous_dependency: boolean;
    runtime_enforcement_authorized: boolean;
    assertion_hash: string;
  };
  proposal: {
    proposal_version: string;
    proposal_id: string;
    state: string;
    trigger: string;
    controls: ProposedControl[];
    lifecycle: Array<{
      state: string;
      occurred_at: string;
      evidence_ref: string;
    }>;
    simulation: {
      contract_version: string;
      input_semantic_hash: string;
      evaluation_mode: string;
      overall_risk: string;
      release_decision: string;
      runtime_mutation: string;
      reused_directly: boolean;
    };
    human_review_required: boolean;
    self_promoted: boolean;
    runtime_promotion_authorized: boolean;
    promotion_gate: string;
    proposal_hash: string;
  };
  replay_status: string;
  runtime_boundary: {
    mode: string;
    persistent: boolean;
    remote_risk_dependency: boolean;
    provider_or_shadow_traffic: boolean;
    credentials_accepted: boolean;
    runtime_policy_read: boolean;
    runtime_enforcement_authorized: boolean;
    runtime_promotion_authorized: boolean;
    runtime_mutation: string;
    external_decision_gate: string;
  };
}

const adviceNames: Record<string, string> = {
  allow_baseline: '保持基线路由',
  shadow_only: '仅建议 / 影子评估',
  deny_advice: '建议拒绝',
};

const riskNames: Record<string, string> = {
  low: '低',
  elevated: '升高',
  high: '高',
  known_gap: '已知缺口',
};

const compactRef = (value: string) => value.length > 30
  ? `${value.slice(0, 18)}…${value.slice(-9)}`
  : value;

export default function RiskControlPage() {
  const [contract, setContract] = useState<RiskControlContract | null>(null);
  const [input, setInput] = useState('');
  const [report, setReport] = useState<RiskControlReport | null>(null);
  const [selectedControlID, setSelectedControlID] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const preview = (body: string) => {
    setLoading(true);
    setError('');
    return api.previewRiskControl(body)
      .then((next: RiskControlReport) => {
        setReport(next);
        setSelectedControlID(current => (
          next.proposal.controls.some(control => control.control_id === current)
            ? current
            : next.proposal.controls[0]?.control_id || ''
        ));
      })
      .catch((previewError: unknown) => {
        setReport(null);
        setSelectedControlID('');
        setError(previewError instanceof ApiError
          ? `校验失败（${previewError.status}）：${previewError.message}`
          : '风险建议与控制提案不满足严格合同。');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    let active = true;
    api.getRiskControlContract()
      .then((next: RiskControlContract) => {
        if (!active) return;
        setContract(next);
        const body = JSON.stringify(next.sample_request, null, 2);
        setInput(body);
        void preview(body);
      })
      .catch((loadError: unknown) => {
        if (!active) return;
        setError(loadError instanceof ApiError
          ? `合同加载失败（${loadError.status}）：${loadError.message}`
          : '风险建议合同暂不可用。');
      });
    return () => { active = false; };
  }, []);

  const selectedControl = report?.proposal.controls.find(
    control => control.control_id === selectedControlID,
  ) || report?.proposal.controls[0] || null;

  const controlColumns = useMemo(() => [
    {
      title: '控制建议',
      dataIndex: 'control_id',
      width: 220,
      render: (value: string) => <span className="ur-mono-break">{value}</span>,
    },
    {
      title: '动作所有者',
      dataIndex: 'target_owner',
      width: 155,
      render: (value: string) => <Tag color="blue">{value}</Tag>,
    },
    {
      title: '建议效果',
      dataIndex: 'proposed_effect',
      render: (value: string) => <span className="ur-mono-break">{value}</span>,
    },
    {
      title: '执行授权',
      dataIndex: 'enforcement_authorized',
      width: 105,
      render: (value: boolean) => value ? <Tag color="red">已授权</Tag> : <Tag>无</Tag>,
    },
  ], []);

  const restoreFixture = () => {
    if (!contract) return;
    const body = JSON.stringify(contract.sample_request, null, 2);
    setInput(body);
    void preview(body);
  };

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="模型接入"
        title="Risk Advice & Control Review"
        description="把 B8 治理模拟、B10 Agent Trust 与 B11 Provenance 组合成有租户、Audience、TTL 与 Epoch 边界的路由建议，再生成必须人工审阅的控制提案。"
      />
      <Banner
        type="warning"
        description="当前只做离线 advice / shadow evidence。Assertion 未签发、不是生产信任输入；B10 tenant_ref 与 B11 Source org/project 的产品本地映射尚未验证，已生成独立人工复核项。提案不会自我晋级，也不会读取或修改运行时策略。P7-D04 的 KPI、阈值与试点范围确定前，不允许强制执行。"
        style={{ marginBottom: 16 }}
      />

      <div className="ur-context-explorer ur-governance-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>风险维度与证据控制建议</Typography.Title>
              <Typography.Text className="ur-panel-hint">
                {contract
                  ? `${contract.contract_version} · ${contract.policy_version}`
                  : '正在读取离线合同'}
              </Typography.Text>
            </div>
            <Tag color={report?.advice === 'deny_advice' ? 'red' : 'orange'}>
              {report ? adviceNames[report.advice] || report.advice : 'offline-only'}
            </Tag>
          </div>

          {report && (
            <div className="ur-kpi-grid ur-governance-kpis">
              <MetricTile label="权限风险" value={riskNames[report.dimensions.authority] || report.dimensions.authority} tone="amber" />
              <MetricTile label="变更风险" value={riskNames[report.dimensions.change] || report.dimensions.change} tone="amber" />
              <MetricTile label="来源证据" value={riskNames[report.dimensions.provenance] || report.dimensions.provenance} tone="violet" />
              <MetricTile label="Capture Gaps" value={formatInteger(report.evidence.capture_gap_count)} tone="amber" />
            </div>
          )}

          <Typography.Title heading={6}>证据派生的控制提案</Typography.Title>
          <div className="ur-table-shell">
            <Table
              rowKey="control_id"
              columns={controlColumns}
              dataSource={report?.proposal.controls || []}
              pagination={false}
              scroll={{ x: 760 }}
              onRow={(control?: ProposedControl) => ({
                onClick: () => control && setSelectedControlID(control.control_id),
                'aria-selected': control?.control_id === selectedControl?.control_id,
              })}
            />
          </div>

          <Typography.Title heading={6}>当前组合审查 JSON</Typography.Title>
          <TextArea
            aria-label="风险建议与控制提案 JSON"
            value={input}
            onChange={value => {
              setInput(value);
              setReport(null);
              setSelectedControlID('');
              setError('');
            }}
            autosize={{ minRows: 10, maxRows: 18 }}
          />
          <div className="ur-governance-actions">
            <Button type="primary" loading={loading} onClick={() => void preview(input)}>
              生成离线建议
            </Button>
            <Button disabled={!contract} onClick={restoreFixture}>恢复安全示例</Button>
          </div>
          {error && <Banner type="danger" description={error} style={{ marginTop: 12 }} />}
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="Risk Control 检查器">
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">Risk & Control Inspector</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>Assertion、证据与晋级边界</Typography.Title>
            </div>
            <Tag color="orange">{report?.proposal.state || '等待审查'}</Tag>
          </div>
          {report ? (
            <dl className="ur-financial-truth-detail">
              <dt>路由建议</dt><dd>{adviceNames[report.advice] || report.advice}</dd>
              <dt>Assertion scope</dt><dd>{report.assertion.scope}</dd>
              <dt>租户 / Audience</dt><dd className="ur-mono-break">{compactRef(report.assertion.tenant_ref)} / {report.assertion.audience}</dd>
              <dt>Risk epoch / TTL</dt><dd>{report.assertion.risk_epoch} / {report.assertion.issued_at} → {report.assertion.expires_at}</dd>
              <dt>权限影响</dt><dd>{report.assertion.authority_effect}</dd>
              <dt>Assertion 签发</dt><dd>{report.assertion.signature_state}</dd>
              <dt>Agent Trust</dt><dd>{report.evidence.agent_trust_decision} / {report.evidence.receipt_cryptographically_valid ? '签名有效' : '签名无效'}</dd>
              <dt>生产信任根</dt><dd>{report.evidence.receipt_runtime_trusted ? '已授权' : '未授权'}</dd>
              <dt>Source / Envelope</dt><dd className="ur-mono-break">{compactRef(report.evidence.source_hash)} / {compactRef(report.evidence.envelope_hash)}</dd>
              <dt>Source 作用域</dt><dd>{report.evidence.source_scope.org_ref} / {report.evidence.source_scope.project_ref} / {report.evidence.source_scope.source_instance}</dd>
              <dt>租户→Source 映射</dt><dd>{report.evidence.source_scope.tenant_binding_status === 'unverified_product_local_mapping' ? '未验证（需人工复核）' : report.evidence.source_scope.tenant_binding_status}</dd>
              <dt>Source scope hash</dt><dd className="ur-mono-break">{report.evidence.source_scope.scope_hash}</dd>
              <dt>数字孪生</dt><dd>{report.proposal.simulation.overall_risk} / {report.proposal.simulation.release_decision}</dd>
              <dt>当前建议项</dt><dd>{selectedControl?.control_id || '无'}</dd>
              <dt>动作所有者</dt><dd>{selectedControl?.target_owner || '无'}</dd>
              <dt>建议效果</dt><dd className="ur-mono-break">{selectedControl?.proposed_effect || '无需提案'}</dd>
              <dt>人工审阅</dt><dd>{report.proposal.human_review_required ? '必须' : '不需要'}</dd>
              <dt>自我晋级</dt><dd>{report.proposal.self_promoted ? '是' : '否'}</dd>
              <dt>外部决策门</dt><dd>{report.proposal.promotion_gate}</dd>
              <dt>同步风险依赖</dt><dd>{report.runtime_boundary.remote_risk_dependency ? '是' : '否'}</dd>
              <dt>真实 / Shadow 流量</dt><dd>{report.runtime_boundary.provider_or_shadow_traffic ? '已发送' : '未发送'}</dd>
              <dt>执行 / 晋级授权</dt><dd>{report.runtime_boundary.runtime_enforcement_authorized ? '有' : '无'} / {report.runtime_boundary.runtime_promotion_authorized ? '有' : '无'}</dd>
              <dt>运行时变更</dt><dd>{report.runtime_boundary.runtime_mutation}</dd>
              <dt>Assertion hash</dt><dd className="ur-mono-break">{report.assertion.assertion_hash}</dd>
              <dt>Proposal hash</dt><dd className="ur-mono-break">{report.proposal.proposal_hash}</dd>
            </dl>
          ) : (
            <EmptyState
              title="等待严格组合审查"
              description="安全示例载入后会自动生成建议并选中首个控制项；右侧检查器常驻，无需点击展开。"
              minHeight={260}
            />
          )}
        </aside>
      </div>
    </div>
  );
}
