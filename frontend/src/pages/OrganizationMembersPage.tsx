import { lazy, Suspense, useEffect, useState } from 'react';
import { Banner, Button, Input, Spin, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import { isOrgAdminRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, Surface } from '../ui';
import { customerContextBlockerCopy, effectiveCustomerOrgID, resolveCustomerContext } from '../utils/customerContext';
import UsersPage from './UsersPage';

const MemberLifecyclePage = lazy(() => import('./MemberLifecyclePage'));
const ProjectManagementPage = lazy(() => import('./ProjectManagementPage'));

const { TabPane } = Tabs;
const { Paragraph, Text, Title } = Typography;

type OrganizationSection = 'people' | 'projects';
type AdvancedSection = 'directory' | 'lifecycle';

type DirectoryHealth = {
  connection: string;
  freshness: string;
  reconciliation: string;
  coverage: string;
};

type DirectorySourceState = {
  observation: {
    source_ref: string;
    authority: string;
    connection: string;
    source_revision?: string;
    observed_at?: string;
  };
  health: DirectoryHealth;
};

type DirectoryUnit = {
  unit_ref: string;
  parent_unit_ref?: string;
  kind: string;
  name: string;
  source_revision: string;
};

function SectionLoading() {
  return <div className="ur-route-loading" role="status" aria-label="正在加载工作区"><Spin size="large" /></div>;
}

function normalizeSection(value: string | null, canManageOrganization: boolean): OrganizationSection {
  if (!canManageOrganization) return 'projects';
  if (value === 'people' || value === 'projects') return value;
  return 'people';
}

function isAdvancedSection(value: string | null): value is AdvancedSection {
  return value === 'directory' || value === 'lifecycle';
}

type BootstrapResult = {
  organization_id: number;
  organization_name: string;
  wallet_balance_micros: number;
  idempotent: boolean;
};

function FirstCustomerBootstrap() {
  const [organizationName, setOrganizationName] = useState('');
  const [clientRequestId] = useState(() => `first-customer-${globalThis.crypto.randomUUID()}`);
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<BootstrapResult | null>(null);

  const submit = async () => {
    const normalizedName = organizationName.trim();
    if (!normalizedName) {
      Toast.warning('请输入首个客户组织名称');
      return;
    }
    setSubmitting(true);
    try {
      const payload = await api.bootstrapFirstCustomer(normalizedName, clientRequestId) as BootstrapResult;
      setResult(payload);
      Toast.success(payload.idempotent ? '已读回同一次初始化结果' : '首个客户组织与零余额钱包已创建');
      try {
        await useAuthStore.getState().loadProfile();
      } catch {
        Toast.warning('客户组织已创建，但当前会话刷新失败。请刷新页面后继续，不要重复初始化。');
      }
    } catch (error: any) {
      Toast.error(`初始化失败${error?.message ? `：${error.message}` : ''}`);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="ur-embedded-page">
      <Banner
        type="warning"
        description="每套本地部署只服务一个客户。本操作会创建唯一的 active 客户组织和 CNY 零余额钱包；不会自动创建用户、项目、上游凭据、应用 Key 或测试流量。"
      />
      <Surface title="初始化客户组织" description="请输入这套 UniRouter 实际服务的客户企业名称。后端会在提交时复核当前部署仍满足首次初始化条件。">
        <div style={{ display: 'grid', gap: 12, maxWidth: 620 }}>
          <Input
            aria-label="首个客户组织名称"
            value={organizationName}
            onChange={setOrganizationName}
            maxLength={160}
            disabled={Boolean(result)}
            placeholder="输入真实客户组织名称"
          />
          <div>
            <Button
              theme="solid"
              type="primary"
              loading={submitting}
              disabled={Boolean(result) || !organizationName.trim()}
              onClick={submit}
            >
              创建组织与零余额钱包
            </Button>
          </div>
          {result ? (
            <Banner
              type="success"
              description={`已创建 ${result.organization_name}（ID ${result.organization_id}），钱包余额 ${result.wallet_balance_micros} micro-CNY。页面会刷新到该客户上下文；下一步请创建客户管理员和首个项目，再配置上游连接与应用 Key。`}
            />
          ) : null}
        </div>
      </Surface>
    </div>
  );
}

function healthColor(value: string) {
  if (value === 'connected' || value === 'fresh' || value === 'reconciled' || value === 'complete') return 'green';
  if (value === 'stale' || value === 'pending' || value === 'gapped' || value === 'conflict' || value === 'error') return 'red';
  if (value === 'disabled') return 'orange';
  return 'grey';
}

function DirectoryFoundation({ orgId }: { orgId?: number }) {
  const [sources, setSources] = useState<DirectorySourceState[]>([]);
  const [tree, setTree] = useState<DirectoryUnit[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    setTree([]);
    api.getEnterpriseDirectorySources(orgId)
      .then(async (payload: any) => {
        if (cancelled) return;
        const nextSources = Array.isArray(payload?.data) ? payload.data as DirectorySourceState[] : [];
        setSources(nextSources);
        const treeSource = nextSources.find(source => (
          source.observation.authority === 'hcm'
          && source.observation.connection === 'connected'
          && Boolean(source.observation.source_revision)
        ));
        if (!treeSource) return;
        const treePayload: any = await api.getEnterpriseDirectoryTree(treeSource.observation.source_ref, undefined, orgId);
        if (!cancelled) setTree(Array.isArray(treePayload?.data) ? treePayload.data as DirectoryUnit[] : []);
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : '目录投影状态读取失败');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [orgId, reloadKey]);

  return (
    <div className="ur-embedded-page">
      <div className="ur-embedded-heading">
        <div>
          <Title heading={4} style={{ margin: 0 }}>企业组织目录</Title>
          <Text type="tertiary">HCM/企业目录拥有人员、正式部门、经理和成本中心真值；UniRouter 只保存版本化本地投影。</Text>
        </div>
        <Tag color="grey">POC 阶段接入</Tag>
      </div>

      <Banner
        type="info"
        description="当前没有可用于验证的企业环境，因此不连接 SCIM、飞书、钉钉、Entra ID、AD 或 HCM，也不接收目录凭据。真实目录接入延后到客户 POC；此页只冻结来源边界和未来同步状态，不展示模拟人员或部门。"
      />

      <div className="ur-org-directory-foundation">
        <Surface title="数据所有权" description="同一字段只有一个权威来源，避免 HCM、IdP 和产品本地配置互相覆盖。">
          <dl className="ur-financial-truth-detail">
            <dt>HCM / HRIS</dt><dd>人员、正式部门、经理、任职状态、成本中心和生效日期</dd>
            <dt>IdP / 企业目录</dt><dd>登录账号、认证状态和目录组</dd>
            <dt>UniRouter</dt><dd>产品角色、项目成员、API Key、模型调用授权和共享限额</dd>
            <dt>其他 Gateway</dt><dd>Tool 与 MCP 权限保持在各自产品</dd>
          </dl>
        </Surface>

        <Surface title="同步闭环" description="真实接入后以本地投影服务请求路径，不把外部目录可用性放进模型请求关键路径。">
          <ol className="ur-plain-list">
            <li>首次全量快照和稳定外部身份映射</li>
            <li>事件/Webhook/Delta 增量和幂等版本检查</li>
            <li>周期对账、Checkpoint、冲突与 Capture Gap</li>
            <li>Joiner/Mover/Leaver 的本地暂停、撤权与历史有效期</li>
          </ol>
        </Surface>
      </div>

      {loading ? (
        <Surface>
          <div className="ur-route-loading" role="status" aria-label="正在读取企业目录状态"><Spin size="large" /></div>
        </Surface>
      ) : error ? (
        <Surface>
          <EmptyState
            title="无法读取目录投影状态"
            description={`${error}。这不会影响本地账号或模型请求处理；外部目录也不在请求关键路径。`}
            actions={<Button onClick={() => setReloadKey(value => value + 1)}>重试</Button>}
            minHeight={220}
          />
        </Surface>
      ) : sources.length === 0 ? (
        <Surface>
        <EmptyState
          title="尚未配置企业目录来源"
          description="第一个真实 Connector 必须在客户 POC 中按目标企业选择；在此之前继续使用 UniRouter 本地账号、角色和项目成员能力，不创建通用高权限目录插件，也不把本地账号手工编辑冒充 HCM 同步。"
          minHeight={220}
        />
        </Surface>
      ) : (
        <div className="ur-org-directory-foundation">
          <Surface title="目录来源健康" description="连接、新鲜度、对账和覆盖是四个独立轴；任一轴异常都不能被一个绿色总状态掩盖。">
            <div className="ur-directory-source-list">
              {sources.map(source => (
                <div className="ur-directory-source-row" key={`${source.observation.authority}:${source.observation.source_ref}`}>
                  <div>
                    <Text strong>{source.observation.source_ref}</Text>
                    <Text type="tertiary">{source.observation.authority.toUpperCase()} · {source.observation.source_revision || '尚无投影版本'}</Text>
                  </div>
                  <div className="ur-directory-health-tags">
                    {Object.entries(source.health).map(([axis, value]) => (
                      <Tag key={axis} color={healthColor(value)}>{axis}: {value}</Tag>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </Surface>
          <Surface title="当前有效组织树" description="只读取本地不可变投影；历史请求不会因后续组织变化被重写。">
            {tree.length === 0 ? (
              <Text type="tertiary">当前没有可展示的有效组织单元；不会用模拟部门填充此区域。</Text>
            ) : (
              <dl className="ur-financial-truth-detail">
                {tree.map(unit => (
                  <div key={unit.unit_ref} className="ur-directory-unit-row">
                    <dt>{unit.name}</dt>
                    <dd>{unit.kind} · {unit.parent_unit_ref || '根节点'} · {unit.source_revision}</dd>
                  </div>
                ))}
              </dl>
            )}
          </Surface>
        </div>
      )}
    </div>
  );
}

export default function OrganizationMembersPage() {
  const user = useAuthStore(state => state.user);
  const organizationRole = organizationRoleForUser(user);
  const customerContext = resolveCustomerContext(user);
  const orgId = effectiveCustomerOrgID(user);
  const canManageOrganization = isOrgAdminRole(organizationRole);
  const platformAdmin = isPlatformAdminRole(organizationRole);
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedSection = searchParams.get('section');
  const activeSection = normalizeSection(requestedSection, canManageOrganization);
  const advancedSection: AdvancedSection = isAdvancedSection(requestedSection) ? requestedSection : 'directory';
  const [advancedOpen, setAdvancedOpen] = useState(() => isAdvancedSection(requestedSection));

  useEffect(() => {
    if (isAdvancedSection(requestedSection)) setAdvancedOpen(true);
  }, [requestedSection]);

  const changeSection = (next: string) => {
    const params = new URLSearchParams(searchParams);
    if (next === 'people') params.delete('section');
    else params.set('section', next);
    setAdvancedOpen(false);
    setSearchParams(params, { replace: true });
  };

  const changeAdvancedSection = (next: string) => {
    if (!isAdvancedSection(next)) return;
    const params = new URLSearchParams(searchParams);
    params.set('section', next);
    setSearchParams(params, { replace: true });
  };

  const toggleAdvanced = () => {
    const nextOpen = !advancedOpen;
    setAdvancedOpen(nextOpen);
    if (!nextOpen && isAdvancedSection(requestedSection)) {
      const params = new URLSearchParams(searchParams);
      params.delete('section');
      setSearchParams(params, { replace: true });
    }
  };

  if (!canManageOrganization) {
    return (
      <Suspense fallback={<SectionLoading />}>
        <ProjectManagementPage />
      </Suspense>
    );
  }

  if (customerContext.state !== 'ready') {
    const blocker = customerContextBlockerCopy(customerContext.state);
    return (
      <div className="ur-page ur-page-wide ur-organization-members-page">
        <PageHeader
          eyebrow="部署设置"
          title={customerContext.state === 'not_initialized' ? '初始化客户组织' : blocker.title}
          description="客户组织上下文是账号、项目、用量和成本归属的共同边界。确认唯一客户前，UniRouter 不会使用部署技术组织代替。"
        />
        {platformAdmin && customerContext.state === 'not_initialized' ? (
          <FirstCustomerBootstrap />
        ) : (
          <Surface>
            <EmptyState title={blocker.title} description={blocker.description} minHeight={260} />
          </Surface>
        )}
      </div>
    );
  }

  return (
    <div className="ur-page ur-page-wide ur-organization-members-page">
      <PageHeader
        eyebrow="访问与安全"
        title="人员与权限"
        description="管理这套部署唯一客户的账号、客户角色和项目成员。企业目录接入与离线合同检查属于高级准备项，不代表已经连接真实 HCM、SCIM 或 IdP。"
      />

      <Tabs activeKey={activeSection} onChange={changeSection} type="line">
        <TabPane tab="账号与身份" itemKey="people">
          {activeSection === 'people' ? <UsersPage embedded /> : null}
        </TabPane>
        <TabPane tab="项目与成员" itemKey="projects">
          {activeSection === 'projects' ? (
            <Suspense fallback={<SectionLoading />}>
              <ProjectManagementPage embedded />
            </Suspense>
          ) : null}
        </TabPane>
      </Tabs>

      <Surface
        title="高级诊断与集成准备"
        description="默认关闭。这里保存尚未连接真实目录时的来源边界和离线事件合同，不参与日常账号、项目或请求处理。"
      >
        <Button aria-expanded={advancedOpen} onClick={toggleAdvanced}>
          {advancedOpen ? '收起高级诊断' : '展开高级诊断'}
        </Button>
        {advancedOpen ? (
          <div style={{ marginTop: 16 }}>
            <Banner
              type="warning"
              description="除非页面明确显示真实目录来源及同步版本，否则下方内容只是接入准备或离线校验，不能作为客户目录已连接、人员已同步或权限已实际撤销的证明。"
              style={{ marginBottom: 12 }}
            />
            <Tabs activeKey={advancedSection} onChange={changeAdvancedSection} type="line">
              <TabPane tab="目录接入准备" itemKey="directory">
                {advancedSection === 'directory' ? <DirectoryFoundation orgId={orgId} /> : null}
              </TabPane>
              <TabPane tab="离线生命周期合同" itemKey="lifecycle">
                {advancedSection === 'lifecycle' ? (
                  <Suspense fallback={<SectionLoading />}>
                    <MemberLifecyclePage embedded />
                  </Suspense>
                ) : null}
              </TabPane>
            </Tabs>
          </div>
        ) : null}
      </Surface>

      <Paragraph type="tertiary" className="ur-org-members-footnote">
        人员、登录账号、工作负载身份和 API Key Owner 不等价；无法证明的个人归属必须保留为共享或未归属。
      </Paragraph>
    </div>
  );
}
