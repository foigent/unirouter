import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Table, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import { api, ApiError } from '../api/client';
import { EmptyState, MetricTile, PageHeader } from '../ui';
import { formatInteger } from '../utils/formatNumber';

interface ArtifactIdentity {
  path: string;
  sha256: string;
}

interface ProvenancePassportContract {
  contract_version: string;
  report_version: string;
  sample_request: Record<string, unknown>;
  artifacts: {
    schema: ArtifactIdentity;
    fixtures: ArtifactIdentity[];
    golden_report: ArtifactIdentity;
  };
  authority_boundary: {
    second_connector_registered: boolean;
    second_connector_runtime_owned: boolean;
    credentials_accepted: boolean;
    cursor_or_checkpoint_accepted: boolean;
    synchronous_runtime_dependency: boolean;
    runtime_mutation: string;
  };
}

interface FieldEvidence {
  field: string;
  classification: string;
  evidence_path: string;
  reason?: string;
}

interface ConnectorAttestation {
  connector_id: string;
  connector_version: string;
  source_product: string;
  source_schema: string;
  event_schema: string;
  manifest_sha256: string;
  source_mapping_sha256: string;
  compatibility_sha256: string;
  conformance_contract_sha256: string;
  vector_bundle_sha256: string;
  independent_reports: string[];
  conformance_cases: number;
  conformance_classes: number;
  status: string;
  runtime_registered: boolean;
  product_runtime_owned: boolean;
  credential_owned: boolean;
  checkpoint_owned: boolean;
  core_vendor_branch_required: boolean;
}

interface ProvenancePassportReport {
  report_version: string;
  passport_id: string;
  source: {
    profile: string;
    schema_id: string;
    schema_sha256: string;
    source_instance: string;
    source_revision: string;
    request_ref: string;
    terminal_status: string;
    source_bytes: number;
    source_hash: string;
    envelope_hash: string;
    idempotency_key: string;
    exact_bytes_preserved: boolean;
    adapter_fidelity: string;
    raw_payload_present: boolean;
    payload_capture_mode: string;
    timeline_complete: boolean;
  };
  field_groups: {
    direct: FieldEvidence[];
    derived: FieldEvidence[];
    declared: FieldEvidence[];
    missing: FieldEvidence[];
  };
  capture_gaps: Array<{
    gap_id: string;
    affected_claim: string;
    gap_type: string;
    reason_code: string;
    severity: string;
    observed_at: string;
    payload_capture_mode: string;
  }>;
  compatibility: {
    endpoint: string;
    page_schema: string;
    connector_id: string;
    connector_version: string;
    event_schema_ref: string;
    event_schema_version: string;
    existing_dto_unchanged: boolean;
    source_bytes_unchanged: boolean;
    event_identity_unchanged: boolean;
    feed_wire_unchanged: boolean;
    page_behavior_unchanged: boolean;
    cursor_behavior_unchanged: boolean;
    credential_unchanged: boolean;
  };
  protocol: {
    repository: string;
    version: string;
    commit: string;
    release_digest: string;
  };
  reference_connector: ConnectorAttestation;
  second_connector: ConnectorAttestation;
  openinference: {
    profile: string;
    contract_version: string;
    golden_fixture_sha256: string;
    creates_source_truth: boolean;
    is_connector_runtime: boolean;
    raw_content_included: boolean;
  };
  runtime_boundary: {
    product_registry_connector_ids: string[];
    second_connector_registered: boolean;
    second_connector_runtime_owned: boolean;
    synchronous_dependency: boolean;
    runtime_mutation: string;
  };
}

interface ConnectorRow extends ConnectorAttestation {
  key: string;
  role: string;
}

const classNames: Record<string, string> = {
  direct: '直接事实',
  derived: '派生事实',
  declared: '声明语义',
  missing: '缺失 / Gap',
};

const compactRef = (value: string) => value.length > 30
  ? `${value.slice(0, 18)}…${value.slice(-9)}`
  : value;

export default function ProvenancePassportPage() {
  const [contract, setContract] = useState<ProvenancePassportContract | null>(null);
  const [input, setInput] = useState('');
  const [report, setReport] = useState<ProvenancePassportReport | null>(null);
  const [selectedKey, setSelectedKey] = useState('reference');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const preview = (body: string) => {
    setLoading(true);
    setError('');
    return api.previewProvenancePassport(body)
      .then((next: ProvenancePassportReport) => {
        setReport(next);
        setSelectedKey(current => (current === 'second' ? current : 'reference'));
      })
      .catch((previewError: unknown) => {
        setReport(null);
        setSelectedKey('reference');
        setError(previewError instanceof ApiError
          ? `校验失败（${previewError.status}）：${previewError.message}`
          : 'Provenance Passport 不满足严格合同。');
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    let active = true;
    api.getProvenancePassportContract()
      .then((next: ProvenancePassportContract) => {
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
          : 'Provenance Passport 合同暂不可用。');
      });
    return () => { active = false; };
  }, []);

  const connectors: ConnectorRow[] = report ? [
    { ...report.reference_connector, key: 'reference', role: '首个 Reference Connector' },
    { ...report.second_connector, key: 'second', role: '异构一致性 Connector' },
  ] : [];
  const selectedConnector = connectors.find(item => item.key === selectedKey) || connectors[0] || null;
  const fieldRows = report
    ? (Object.entries(report.field_groups) as Array<[string, FieldEvidence[]]>)
      .flatMap(([classification, fields]) => fields.map(field => ({
        ...field,
        key: `${classification}:${field.field}`,
        classification,
      })))
    : [];

  const connectorColumns = useMemo(() => [
    {
      title: '角色',
      dataIndex: 'role',
      width: 180,
    },
    {
      title: 'Connector',
      dataIndex: 'connector_id',
      width: 260,
      render: (value: string) => <span className="ur-mono-break">{value}</span>,
    },
    {
      title: '一致性',
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <Tag color={value === 'pass' ? 'green' : 'red'}>{value}</Tag>,
    },
    {
      title: '运行态',
      dataIndex: 'runtime_registered',
      render: (value: boolean) => value ? 'UniRouter 已注册' : '仅共享一致性',
    },
  ], []);

  const fieldColumns = useMemo(() => [
    {
      title: '分类',
      dataIndex: 'classification',
      width: 125,
      render: (value: string) => (
        <Tag color={value === 'direct' ? 'blue' : value === 'missing' ? 'orange' : 'grey'}>
          {classNames[value] || value}
        </Tag>
      ),
    },
    {
      title: '字段 / Claim',
      dataIndex: 'field',
      width: 250,
      render: (value: string) => <span className="ur-mono-break">{value}</span>,
    },
    {
      title: '证据路径',
      dataIndex: 'evidence_path',
      render: (value: string) => <span className="ur-mono-break">{value}</span>,
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
        eyebrow="可观测与集成"
        title="Provenance & Capture-Gap Passport"
        description="把来源原字节、共享事件身份、直接/派生/声明/缺失证据、四类 Capture Gap 与 Connector 一致性放到同一个可复核工作区。"
      />
      <Banner
        type="info"
        description="LiteLLM 是共享协议权威中的第二个异构 Connector，仅用于跨语言一致性证明；它没有进入 UniRouter 运行时 registry，也不由 UniRouter 持有其凭据、Cursor 或 Checkpoint。OpenInference 仍是元数据观察投影，不被误称为来源事实或 Connector 运行时。"
        style={{ marginBottom: 16 }}
      />

      <div className="ur-context-explorer ur-governance-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>来源 Passport 与 Connector 证据</Typography.Title>
              <Typography.Text className="ur-panel-hint">
                {contract ? `${contract.contract_version} · product protocol v${report?.protocol.version || '—'}` : '正在读取离线合同'}
              </Typography.Text>
            </div>
            <Tag color="blue">exact-source-bytes</Tag>
          </div>

          <div className="ur-table-shell">
            <Table
              rowKey="key"
              columns={connectorColumns}
              dataSource={connectors}
              pagination={false}
              scroll={{ x: 700 }}
              onRow={(connector?: ConnectorRow) => ({
                onClick: () => connector && setSelectedKey(connector.key),
                'aria-selected': connector?.key === selectedConnector?.key,
              })}
            />
          </div>

          {report && (
            <div className="ur-kpi-grid ur-governance-kpis">
              <MetricTile label="直接事实" value={formatInteger(report.field_groups.direct.length)} tone="blue" />
              <MetricTile label="派生 / 声明" value={formatInteger(report.field_groups.derived.length + report.field_groups.declared.length)} tone="green" />
              <MetricTile label="缺失 Claims" value={formatInteger(report.field_groups.missing.length)} tone="amber" />
              <MetricTile label="Source bytes" value={formatInteger(report.source.source_bytes)} tone="blue" />
            </div>
          )}

          <Typography.Title heading={6}>字段来源与缺口</Typography.Title>
          <div className="ur-table-shell">
            <Table
              rowKey="key"
              columns={fieldColumns}
              dataSource={fieldRows}
              pagination={false}
              scroll={{ x: 650 }}
            />
          </div>

          <Typography.Title heading={6}>当前 Passport JSON</Typography.Title>
          <TextArea
            aria-label="Provenance Passport JSON"
            value={input}
            onChange={value => {
              setInput(value);
              setReport(null);
              setSelectedKey('reference');
              setError('');
            }}
            autosize={{ minRows: 9, maxRows: 16 }}
          />
          <div className="ur-governance-actions">
            <Button type="primary" loading={loading} onClick={() => void preview(input)}>生成离线 Passport</Button>
            <Button disabled={!contract} onClick={restoreFixture}>恢复安全示例</Button>
          </div>
          {error && <Banner type="danger" description={error} style={{ marginTop: 12 }} />}
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="Provenance Passport 检查器">
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">Passport Inspector</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>来源、兼容性与隔离边界</Typography.Title>
            </div>
            <Tag color={selectedConnector?.runtime_registered ? 'green' : 'blue'}>
              {selectedConnector?.runtime_registered ? 'Reference runtime' : 'conformance-only'}
            </Tag>
          </div>
          {report && selectedConnector ? (
            <dl className="ur-financial-truth-detail">
              <dt>Request</dt><dd className="ur-mono-break">{report.source.request_ref}</dd>
              <dt>终态 / Revision</dt><dd>{report.source.terminal_status} / {report.source.source_revision}</dd>
              <dt>Source 原字节</dt><dd>{report.source.exact_bytes_preserved ? '精确保留' : '未证明'}</dd>
              <dt>Source hash</dt><dd className="ur-mono-break">{compactRef(report.source.source_hash)}</dd>
              <dt>Envelope hash</dt><dd className="ur-mono-break">{compactRef(report.source.envelope_hash)}</dd>
              <dt>Idempotency</dt><dd className="ur-mono-break">{compactRef(report.source.idempotency_key)}</dd>
              <dt>固定 Capture Gaps</dt><dd>{formatInteger(report.capture_gaps.length)} 条</dd>
              <dt>Feed endpoint</dt><dd className="ur-mono-break">{report.compatibility.endpoint}</dd>
              <dt>DTO / Feed / Cursor</dt><dd>{report.compatibility.existing_dto_unchanged && report.compatibility.feed_wire_unchanged && report.compatibility.cursor_behavior_unchanged ? '保持兼容' : '存在冲突'}</dd>
              <dt>当前 Connector</dt><dd className="ur-mono-break">{selectedConnector.connector_id}@{selectedConnector.connector_version}</dd>
              <dt>Source / Event Schema</dt><dd className="ur-mono-break">{selectedConnector.source_schema} → {selectedConnector.event_schema}</dd>
              <dt>Manifest</dt><dd className="ur-mono-break">{compactRef(selectedConnector.manifest_sha256)}</dd>
              <dt>Mapping</dt><dd className="ur-mono-break">{compactRef(selectedConnector.source_mapping_sha256)}</dd>
              <dt>Conformance</dt><dd>{selectedConnector.conformance_cases}/{selectedConnector.conformance_cases} cases · {selectedConnector.conformance_classes} classes · 2 languages</dd>
              <dt>供应商核心分支</dt><dd>{selectedConnector.core_vendor_branch_required ? '需要' : '不需要'}</dd>
              <dt>UniRouter runtime</dt><dd>{selectedConnector.runtime_registered ? '已注册 Reference' : '未注册'}</dd>
              <dt>凭据 / Checkpoint</dt><dd>{selectedConnector.credential_owned || selectedConnector.checkpoint_owned ? '产品持有部分状态' : 'UniRouter 不持有'}</dd>
              <dt>OpenInference</dt><dd>{report.openinference.creates_source_truth ? '来源事实' : 'metadata observer'}</dd>
              <dt>同步依赖 / 运行时变更</dt><dd>{report.runtime_boundary.synchronous_dependency ? '是' : '否'} / {report.runtime_boundary.runtime_mutation}</dd>
            </dl>
          ) : (
            <EmptyState
              title="等待严格映射"
              description="安全示例载入后会自动生成 Passport 并选中 Reference Connector；无需点击展开右侧检查器。"
              minHeight={260}
            />
          )}
        </aside>
      </div>
    </div>
  );
}
