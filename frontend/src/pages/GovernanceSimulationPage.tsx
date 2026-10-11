import '../i18n/security';
import type { MessageKey } from '../i18n/catalog';
import { translate as t, useI18n, labelFor } from '../i18n';
import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Table, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { api, ApiError } from '../api/client';
import { EmptyState, MetricTile, PageHeader } from '../ui';
import { formatInteger } from '../utils/formatNumber';

interface ArtifactIdentity {
  path: string;
  sha256: string;
}

interface GovernanceContract {
  contract_version: string;
  report_version: string;
  release_version: string;
  policy_version: string;
  selection_version: string;
  sample_request: Record<string, unknown>;
  artifacts: {
    schema: ArtifactIdentity;
    fixtures: ArtifactIdentity[];
    golden_report: ArtifactIdentity;
  };
}

interface RouteCandidate {
  channel_id: string;
  channel_name: string;
  provider: string;
  key_id?: string;
  order?: number;
  priority: number;
  weight: number;
  reason: string;
}

interface RouteChange {
  field: string;
  baseline: string;
  candidate: string;
  risk: string;
  reason: string;
}

interface ProviderPackChange {
  pack_id: string;
  baseline_version: string;
  candidate_version: string;
  risk: string;
  reasons: string[];
}

interface GovernanceReport {
  report_version: string;
  contract_version: string;
  simulation_id: string;
  input_payload_hash: string;
  input_semantic_hash: string;
  evaluation_mode: string;
  baseline: {
    snapshot_id: string;
    snapshot_hash: string;
    candidates: RouteCandidate[];
    exclusions: RouteCandidate[];
  };
  candidate: {
    snapshot_id: string;
    snapshot_hash: string;
    candidates: RouteCandidate[];
    exclusions: RouteCandidate[];
  };
  route_changes: RouteChange[];
  provider_pack_changes: ProviderPackChange[];
  summary: {
    unchanged: number;
    compatible: number;
    review_required: number;
    breaking: number;
  };
  overall_risk: string;
  release: {
    release_id: string;
    revision: string;
    action: string;
    from_stage: string;
    simulated_stage: string;
    rollback_to_revision: string;
    audit_event_id: string;
    decision: string;
    promotion_gate: string;
    runtime_promotion_authorized: boolean;
    runtime_mutation: string;
  };
  runtime_mutation: string;
}

type Selection =
  | { kind: 'provider'; index: number }
  | { kind: 'route'; index: number };

const riskLabel: Record<string, string> = {
  unchanged: '无变化',
  compatible: '兼容',
  review_required: '需要评审',
  breaking: '阻断性变化',
};

const riskColor: Record<string, 'green' | 'blue' | 'orange' | 'red'> = {
  unchanged: 'green',
  compatible: 'blue',
  review_required: 'orange',
  breaking: 'red',
};

const riskMetricLabel: Record<string, string> = {
  unchanged: '无变化',
  compatible: '兼容',
  review_required: '需评审',
  breaking: '阻断',
};

function RiskTag({ risk }: { risk: string }) {
  useI18n();
  return <Tag color={riskColor[risk] || 'grey'}>{labelFor(riskLabel, risk)}</Tag>;
}

export default function GovernanceSimulationPage() {
  const { locale } = useI18n();
  const [contract, setContract] = useState<GovernanceContract | null>(null);
  const [input, setInput] = useState('');
  const [report, setReport] = useState<GovernanceReport | null>(null);
  const [selection, setSelection] = useState<Selection | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<{key: MessageKey; values?: Record<string, string | number>} | null>(null);

  useEffect(() => {
    let active = true;
    api.getGovernanceSimulationContract()
      .then((next: GovernanceContract) => {
        if (!active) return;
        setContract(next);
        setInput(JSON.stringify(next.sample_request, null, 2));
      })
      .catch((loadError: unknown) => {
        if (!active) return;
        setError(loadError instanceof ApiError ? {key: '合同加载失败（{status}）：{detail}', values: {status: loadError.status, detail: loadError.message}} : {key: '离线治理合同暂不可用。'});
      });
    return () => { active = false; };
  }, []);

  const selectedProvider = selection?.kind === 'provider'
    ? report?.provider_pack_changes[selection.index]
    : undefined;
  const selectedRoute = selection?.kind === 'route'
    ? report?.route_changes[selection.index]
    : undefined;

  const runPreview = () => {
    if (!input.trim()) {
      setError({key: '请粘贴或载入符合离线合同的 JSON 快照。'});
      return;
    }
    setLoading(true);
    setError(null);
    api.previewGovernanceSimulation(input)
      .then((next: GovernanceReport) => {
        setReport(next);
        setSelection(next.provider_pack_changes.length > 0
          ? { kind: 'provider', index: 0 }
          : next.route_changes.length > 0
            ? { kind: 'route', index: 0 }
            : null);
      })
      .catch((previewError: unknown) => {
        setReport(null);
        setSelection(null);
        setError(previewError instanceof ApiError ? {key: '模拟失败（{status}）：{detail}', values: {status: previewError.status, detail: previewError.message}} : {key: '模拟失败：离线快照不可用。'});
      })
      .finally(() => setLoading(false));
  };

  const loadFile = (file?: File) => {
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) {
      setError({key: '文件超过 2 MiB 离线模拟上限。'});
      return;
    }
    file.text()
      .then(text => {
        setInput(text);
        setReport(null);
        setSelection(null);
        setError(null);
      })
      .catch(() => setError({key: '无法读取所选文件。'}));
  };

  const providerColumns = useMemo(() => [
    {
      title: 'Provider Pack',
      dataIndex: 'pack_id',
      width: 240,
    },
    {
      title: t("版本变化"),
      width: 180,
      render: (_: unknown, row: ProviderPackChange) =>
        `${row.baseline_version || t("新增")} → ${row.candidate_version || t("移除")}`,
    },
    {
      title: t("风险"),
      dataIndex: 'risk',
      width: 130,
      render: (value: string) => <RiskTag risk={value} />,
    },
  ], [locale]);

  const routeColumns = useMemo(() => [
    { title: t("路由/策略字段"), dataIndex: 'field', width: 190 },
    {
      title: t("风险"),
      dataIndex: 'risk',
      width: 130,
      render: (value: string) => <RiskTag risk={value} />,
    },
    { title: t("原因"), dataIndex: 'reason', width: 240 },
  ], [locale]);

  return (
    <div className="ur-page ur-security-workspace">
      <PageHeader
        eyebrow={t("模型接入")}
        title={t("变更防火墙")}
        description={t("用同一套路由内核离线比较策略、通道和 Provider Pack；结果只生成审计证据，不提升任何运行时配置。")}
      />

      <Banner
        type="info"
        description={t("这是零网络、零持久化的治理模拟。shadow / canary / enforced 仅表示被评估的目标模式；真正上线仍需独立外部授权。")}
        closeIcon={null}
        style={{ marginBottom: 16 }}
      />
      <div className="ur-context-explorer ur-governance-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("不可变快照对比")}</Typography.Title>
              <Typography.Text className="ur-panel-hint">
                {contract
                  ? `${contract.contract_version} · ${contract.selection_version}`
                  : t("正在读取离线合同")}
              </Typography.Text>
            </div>
            <Tag color="blue">zero runtime mutation</Tag>
          </div>
          <TextArea
            aria-label={t("治理模拟 JSON")}
            value={input}
            onChange={value => {
              setInput(value);
              setReport(null);
              setSelection(null);
              setError(null);
            }}
            autosize={{ minRows: 10, maxRows: 18 }}
            placeholder={t("载入 baseline / candidate 快照和 release intent")}
          />
          <div className="ur-governance-actions">
            <label className="semi-button semi-button-tertiary semi-button-light" tabIndex={0}>

              {t("载入 JSON 文件")}
              <input
                aria-label={t("载入治理模拟 JSON 文件")}
                type="file"
                accept="application/json,.json"
                hidden
                onChange={event => loadFile(event.target.files?.[0])}
              />
            </label>
            <Button type="primary" loading={loading} onClick={runPreview}>{t("运行离线模拟")}</Button>
            <Button
              disabled={!contract}
              onClick={() => {
                if (!contract) return;
                setInput(JSON.stringify(contract.sample_request, null, 2));
                setReport(null);
                setSelection(null);
                setError(null);
              }}
            >

              {t("恢复安全示例")}
            </Button>
          </div>
          {error && <Banner type="danger" description={error ? t(error.key, error.values) : undefined} style={{ marginTop: 12 }} />}

          {report && (
            <>
              <div className="ur-kpi-grid ur-governance-kpis">
                <MetricTile label={t("总体风险")} value={labelFor(riskMetricLabel, report.overall_risk)} tone={report.overall_risk === 'breaking' ? 'red' : 'amber'} />
                <MetricTile label={t("需评审变化")} value={formatInteger(report.summary.review_required)} tone="amber" />
                <MetricTile label={t("阻断变化")} value={formatInteger(report.summary.breaking)} tone="red" />
                <MetricTile label={t("候选路由")} value={formatInteger(report.candidate.candidates.length)} tone="blue" />
              </div>

              <Typography.Title heading={6}>Provider Change Firewall</Typography.Title>
              <div className="ur-table-shell">
                <Table
                  rowKey="pack_id"
                  columns={providerColumns}
                  dataSource={report.provider_pack_changes}
                  pagination={false}
                  scroll={{ x: 600 }}
                  onRow={(row?: ProviderPackChange) => {
                    const index = row ? report.provider_pack_changes.indexOf(row) : 0;
                    return {
                      onClick: () => setSelection({ kind: 'provider', index: Math.max(index, 0) }),
                      'aria-selected': selection?.kind === 'provider' && selection.index === Math.max(index, 0),
                    };
                  }}
                />
              </div>

              <Typography.Title heading={6}>{t("路由与策略差异")}</Typography.Title>
              <div className="ur-table-shell">
                <Table
                  rowKey={(row?: RouteChange) => row?.field || 'missing'}
                  columns={routeColumns}
                  dataSource={report.route_changes}
                  pagination={false}
                  scroll={{ x: 620 }}
                  onRow={(row?: RouteChange) => {
                    const index = row ? report.route_changes.indexOf(row) : 0;
                    return {
                      onClick: () => setSelection({ kind: 'route', index: Math.max(index, 0) }),
                      'aria-selected': selection?.kind === 'route' && selection.index === Math.max(index, 0),
                    };
                  }}
                />
              </div>
            </>
          )}
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label={t("治理变更检查器")}>
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">{t("治理变更检查器")}</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("证据与上线边界")}</Typography.Title>
            </div>
            <RiskTag risk={report?.overall_risk || 'unchanged'} />
          </div>
          {report ? (
            <dl className="ur-financial-truth-detail">
              <dt>{t("模拟 ID")}</dt><dd>{report.simulation_id}</dd>
              <dt>{t("评估模式")}</dt><dd>{report.evaluation_mode}</dd>
              <dt>{t("发布决策")}</dt><dd>{report.release.decision}</dd>
              <dt>{t("模拟阶段")}</dt><dd>{report.release.from_stage} → {report.release.simulated_stage}</dd>
              <dt>{t("运行时变更")}</dt><dd>{report.runtime_mutation}</dd>
              <dt>{t("运行时提升授权")}</dt><dd>{report.release.runtime_promotion_authorized ? t("是") : t("否")}</dd>
              <dt>{t("选中变化")}</dt>
              <dd>
                {selectedProvider
                  ? `${selectedProvider.pack_id} · ${labelFor(riskLabel, selectedProvider.risk)}`
                  : selectedRoute
                    ? `${selectedRoute.field} · ${labelFor(riskLabel, selectedRoute.risk)}`
                    : t("没有语义变化")}
              </dd>
              <dt>{t("原因")}</dt>
              <dd>
                {selectedProvider
                  ? selectedProvider.reasons.join('、')
                  : selectedRoute?.reason || '—'}
              </dd>
              <dt>Baseline</dt>
              <dd className="ur-mono-break">
                {selectedProvider
                  ? selectedProvider.baseline_version || '—'
                  : selectedRoute?.baseline || report.baseline.snapshot_hash}
              </dd>
              <dt>Candidate</dt>
              <dd className="ur-mono-break">
                {selectedProvider
                  ? selectedProvider.candidate_version || '—'
                  : selectedRoute?.candidate || report.candidate.snapshot_hash}
              </dd>
              <dt>{t("语义输入哈希")}</dt><dd className="ur-mono-break">{report.input_semantic_hash}</dd>
              <dt>{t("审计事件")}</dt><dd className="ur-mono-break">{report.release.audit_event_id}</dd>
              <dt>{t("上线门禁")}</dt><dd>{report.release.promotion_gate}</dd>
            </dl>
          ) : (
            <EmptyState
              title={t("等待离线模拟")}
              description={t("检查器始终可见；运行后会自动选中第一项 Provider 变化，无需再次点击打开。")}
              minHeight={320}
            />
          )}
        </aside>
      </div>
    </div>
  );
}
