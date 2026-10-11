import '../i18n/people';
import { translate as t, translateLabel, useI18n } from '../i18n';
import { useEffect, useMemo, useState } from 'react';
import { Banner, Button, Table, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import type { MessageKey } from '../i18n/catalog';
import { api, ApiError } from '../api/client';
import { useAuthStore } from '../store/auth';
import { EmptyState, MetricTile, PageHeader, Surface } from '../ui';
import { effectiveCustomerOrgID } from '../utils/customerContext';
import { formatInteger } from '../utils/formatNumber';

interface ArtifactIdentity {
  path: string;
  sha256: string;
}

interface LifecycleEvent {
  event_id: string;
  operation: string;
  revision: string;
  subject_ref: string;
  after: {
    status: string;
    memberships: unknown[];
    capabilities: unknown[];
  };
}

interface LifecycleContract {
  contract_version: string;
  report_version: string;
  operations: string[];
  sample_events: LifecycleEvent[];
  artifacts: {
    schema: ArtifactIdentity;
    fixtures: ArtifactIdentity[];
    golden_report: ArtifactIdentity;
  };
  authority_boundary: {
    directory_vendor_selected: boolean;
    idp_network_access: boolean;
    runtime_mutation: string;
  };
}

interface Revocation {
  kind: string;
  ref: string;
  reason: string;
}

interface LifecycleReport {
  report_version: string;
  event_id: string;
  subject_ref: string;
  operation: string;
  revision: string;
  state_status: string;
  input_semantic_hash: string;
  effective_memberships: unknown[];
  effective_capabilities: unknown[];
  revocations: Revocation[];
  controls: {
    deny_admin_sessions: boolean;
    revoke_live_admin_sessions: boolean;
    disable_personal_keys_before_delete: boolean;
    agent_capability_recheck_seconds: string;
  };
  local_projection: {
    user_status: string;
    membership_storage: string;
    personal_key_handling: string;
    session_handling: string;
  };
  vendor_adapter_authorized: boolean;
  runtime_mutation: string;
}

interface IAMConvergenceAction {
  id: number;
  snapshot_ref: string;
  subject_kind: string;
  subject_ref: string;
  local_user_id?: number;
  action: string;
  reason: string;
  result: string;
  before_status: string;
  after_status: string;
  created_at: string;
}

const operationNames: Record<string, string> = {
  create: '创建',
  update: '更新 / 恢复',
  suspend: '暂停',
  deprovision: '退场',
};

const statusNames: Record<string, string> = {
  active: '有效',
  suspended: '已暂停',
  deprovisioned: '已退场',
};

export default function MemberLifecyclePage({ embedded = false }: { embedded?: boolean }) {
  const { locale } = useI18n();
  const user = useAuthStore(state => state.user);
  const orgId = effectiveCustomerOrgID(user);
  const [contract, setContract] = useState<LifecycleContract | null>(null);
  const [input, setInput] = useState('');
  const [report, setReport] = useState<LifecycleReport | null>(null);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<{ key: MessageKey; detail?: string; status?: number } | null>(null);
	const [iamActions, setIAMActions] = useState<IAMConvergenceAction[]>([]);
	const [iamEvidenceLoading, setIAMEvidenceLoading] = useState(true);
	const [iamEvidenceError, setIAMEvidenceError] = useState<{ key: MessageKey; detail?: string; status?: number } | null>(null);

  const preview = (body: string) => {
    setLoading(true);
    setError(null);
    return api.previewMemberLifecycle(body)
      .then((next: LifecycleReport) => setReport(next))
      .catch((previewError: unknown) => {
        setReport(null);
        setError(previewError instanceof ApiError
          ? { key: '校验失败', status: previewError.status, detail: previewError.message }
          : { key: '成员生命周期事件不满足严格合同。' });
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    let active = true;
    api.getMemberLifecycleContract()
      .then((next: LifecycleContract) => {
        if (!active) return;
        setContract(next);
        const first = next.sample_events[0];
        if (first) {
          const body = JSON.stringify(first, null, 2);
          setInput(body);
          void preview(body);
        }
      })
      .catch((loadError: unknown) => {
        if (!active) return;
        setError(loadError instanceof ApiError
          ? { key: '合同加载失败', status: loadError.status, detail: loadError.message }
          : { key: '成员生命周期合同暂不可用。' });
      });
    return () => { active = false; };
  }, []);

	useEffect(() => {
		let active = true;
		setIAMEvidenceLoading(true);
		setIAMEvidenceError(null);
		api.getEnterpriseDirectoryIAMActions(20, orgId)
			.then((payload: any) => {
				if (!active) return;
				setIAMActions(Array.isArray(payload?.data) ? payload.data as IAMConvergenceAction[] : []);
			})
			.catch((loadError: unknown) => {
				if (!active) return;
				setIAMEvidenceError(loadError instanceof ApiError
					? { key: '本地证据读取失败', status: loadError.status, detail: loadError.message }
					: { key: '本地收敛证据暂不可用。' });
			})
			.finally(() => {
				if (active) setIAMEvidenceLoading(false);
			});
		return () => { active = false; };
	}, [orgId]);

  const selectEvent = (event: LifecycleEvent, index: number) => {
    const body = JSON.stringify(event, null, 2);
    setSelectedIndex(index);
    setInput(body);
    void preview(body);
  };

  const columns = useMemo(() => [
    {
      title: t("阶段"),
      dataIndex: 'operation',
      width: 150,
      render: (value: string) => <Tag color={value === 'deprovision' ? 'red' : value === 'suspend' ? 'orange' : 'blue'}>{operationNames[value] ? translateLabel(operationNames[value]) : value}</Tag>,
    },
    { title: t("事件"), dataIndex: 'event_id', width: 250 },
    { title: t("修订"), dataIndex: 'revision', width: 90 },
    {
      title: t("结果状态"),
      render: (_: unknown, event: LifecycleEvent) => statusNames[event.after.status] ? translateLabel(statusNames[event.after.status]) : event.after.status,
    },
  ], [locale]);

  return (
    <div className={embedded ? 'ur-embedded-page ur-people-workspace' : 'ur-page ur-people-workspace'}>
      {embedded ? (
        <div className="ur-embedded-heading">
          <div>
            <Typography.Title heading={4} style={{ margin: 0 }}>{t("离线生命周期合同检查")}</Typography.Title>
            <Typography.Text type="tertiary">{t("高级诊断工具：验证示例事件格式和预期访问收敛，不代表已经连接企业目录。")}</Typography.Text>
          </div>
          <Tag color="blue">{t("离线合同")}</Tag>
        </div>
      ) : <PageHeader
        eyebrow={t("访问与安全")}
        title={t("成员生命周期")}
        description={t("先用供应商无关的合同证明创建、更新、暂停和退场会收敛成员关系、会话与 Agent 能力，再由未来单独批准的目录适配器接入。")}
      />}
      <Banner
        type="info"
        description={t("页面中的示例 JSON 可编辑，但预演始终只读：不连接 SCIM / IdP、不接收凭据、不写数据库，也不会修改真实成员或权限。真实本地收敛记录会在独立区域明确标注。")}
        style={{ marginBottom: 16 }}
      />

		<Surface
			title={t("本地 IAM 收敛证据")}
			description={t("只有覆盖完整的明确终止、明确禁用或显式 tombstone 才能暂停已绑定账号；入职、调岗和复职不会自动授予或恢复权限。")}
		>
			{iamEvidenceLoading ? (
				<Typography.Text type="tertiary">{t("正在读取本地收敛记录…")}</Typography.Text>
			) : iamEvidenceError ? (
				<Banner type="warning" description={`${t(iamEvidenceError.key)}${iamEvidenceError.status ? ` (${iamEvidenceError.status})` : ''}${iamEvidenceError.detail ? `: ${iamEvidenceError.detail}` : ''}`} />
			) : iamActions.length === 0 ? (
				<Typography.Text type="tertiary">{t("尚无本地收敛记录；不会用离线示例冒充真实目录动作。")}</Typography.Text>
			) : (
				<div className="ur-directory-source-list">
					{iamActions.slice(0, 5).map(action => (
						<div className="ur-directory-source-row" key={action.id}>
							<div>
								<Typography.Text strong>{action.subject_kind}:{action.subject_ref}</Typography.Text>
								<Typography.Text type="tertiary">{action.reason} · {action.before_status || t("无")} → {action.after_status || t("无")}</Typography.Text>
							</div>
							<div className="ur-directory-health-tags">
								<Tag color={action.action === 'disable_user' ? 'red' : action.action === 'review_required' ? 'orange' : 'grey'}>{action.action}</Tag>
								<Tag color={action.result === 'applied' ? 'green' : action.result === 'conflict' ? 'red' : 'blue'}>{action.result}</Tag>
							</div>
						</div>
					))}
				</div>
			)}
		</Surface>

      <div className="ur-context-explorer ur-governance-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("离线示例链与事件格式")}</Typography.Title>
              <Typography.Text className="ur-panel-hint">
                {contract ? t('{version} · 未连接真实目录供应商，仅校验事件格式', { version: contract.contract_version }) : t("正在读取离线合同")}
              </Typography.Text>
            </div>
            <Tag color="blue">IdP-neutral</Tag>
          </div>

          <div className="ur-table-shell">
            <Table
              rowKey="event_id"
              columns={columns}
              dataSource={contract?.sample_events || []}
              pagination={false}
              scroll={{ x: 660 }}
              onRow={(event?: LifecycleEvent) => {
                const index = event && contract ? contract.sample_events.indexOf(event) : 0;
                return {
                  onClick: () => event && selectEvent(event, Math.max(index, 0)),
                  'aria-selected': selectedIndex === Math.max(index, 0),
                };
              }}
            />
          </div>

          <Typography.Title heading={6}>{t("离线示例事件 JSON（可编辑，不会写入系统）")}</Typography.Title>
          <TextArea
            aria-label={t("成员生命周期事件 JSON")}
            value={input}
            onChange={value => {
              setInput(value);
              setReport(null);
              setError(null);
            }}
            autosize={{ minRows: 9, maxRows: 16 }}
          />
          <div className="ur-governance-actions">
            <Button type="primary" loading={loading} onClick={() => void preview(input)}>{t("运行离线校验")}</Button>
            <Button
              disabled={!contract?.sample_events[selectedIndex]}
              onClick={() => {
                const event = contract?.sample_events[selectedIndex];
                if (event) selectEvent(event, selectedIndex);
              }}
            >

              {t("恢复选中示例")}
            </Button>
          </div>
          {error && <Banner type="danger" description={`${t(error.key)}${error.status ? ` (${error.status})` : ''}${error.detail ? `: ${error.detail}` : ''}`} style={{ marginTop: 12 }} />}

          {report && (
            <>
              <Typography.Paragraph type="tertiary" style={{ marginTop: 16 }}>

                {t("以下数字只统计当前离线示例校验结果中的数组项，不是客户组织、真实项目或生产权限的统计。")}
              </Typography.Paragraph>
              <div className="ur-kpi-grid ur-governance-kpis">
                <MetricTile label={t("示例保留项目关系")} value={formatInteger(report.effective_memberships.length)} tone="blue" />
                <MetricTile label={t("示例保留访问能力")} value={formatInteger(report.effective_capabilities.length)} tone="green" />
                <MetricTile label={t("示例需撤销访问项")} value={formatInteger(report.revocations.length)} tone={report.revocations.length ? 'amber' : 'blue'} />
                <MetricTile label={t("实际运行时写入")} value={report.runtime_mutation === 'none' ? t("无") : report.runtime_mutation} tone="blue" />
              </div>
            </>
          )}
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label={t("成员生命周期检查器")}>
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">{t("生命周期检查器")}</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>{t("访问收敛证据")}</Typography.Title>
            </div>
            <Tag color={report?.state_status === 'deprovisioned' ? 'red' : report?.state_status === 'suspended' ? 'orange' : 'green'}>
              {statusNames[report?.state_status || ''] ? translateLabel(statusNames[report?.state_status || '']) : t('等待校验')}
            </Tag>
          </div>
          {report ? (
            <dl className="ur-financial-truth-detail">
              <dt>{t("事件")}</dt><dd>{report.event_id}</dd>
              <dt>{t("操作")}</dt><dd>{operationNames[report.operation] ? translateLabel(operationNames[report.operation]) : report.operation}</dd>
              <dt>{t("修订")}</dt><dd>{report.revision}</dd>
              <dt>{t("账号投影")}</dt><dd>{report.local_projection.user_status}</dd>
              <dt>{t("成员存储")}</dt><dd>{report.local_projection.membership_storage}</dd>
              <dt>{t("个人 Key")}</dt><dd>{report.local_projection.personal_key_handling}</dd>
              <dt>{t("管理员会话")}</dt><dd>{report.local_projection.session_handling}</dd>
              <dt>{t("拒绝旧会话")}</dt><dd>{report.controls.deny_admin_sessions ? t("是") : t("否")}</dd>
              <dt>{t("撤销在线会话")}</dt><dd>{report.controls.revoke_live_admin_sessions ? t("是") : t("否")}</dd>
              <dt>{t("Agent 权限重检")}</dt><dd>≤ {report.controls.agent_capability_recheck_seconds}  {t("秒")}</dd>
              <dt>{t("目录适配授权")}</dt><dd>{report.vendor_adapter_authorized ? t("是") : t("否")}</dd>
              <dt>{t("运行时变更")}</dt><dd>{report.runtime_mutation}</dd>
              <dt>{t("语义哈希")}</dt><dd className="ur-mono-break">{report.input_semantic_hash}</dd>
              <dt>{t("撤销明细")}</dt>
              <dd>
                {report.revocations.length
                  ? report.revocations.map(item => `${item.kind}:${item.reason}`).join('；')
                  : t("无")}
              </dd>
            </dl>
          ) : (
            <EmptyState
              title={t("等待严格事件")}
              description={t("安全示例载入后会自动校验并选中首个阶段；不需要点击展开右侧检查器。")}
              minHeight={240}
            />
          )}
        </aside>
      </div>
    </div>
  );
}
