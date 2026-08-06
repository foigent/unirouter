import { lazy, Suspense, useEffect, useState } from 'react';
import { Banner, Button, Input, Spin, Tabs, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import { isOrgAdminRole, isPlatformAdminRole, organizationRoleForUser } from '../navigation';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, Surface } from '../ui';
import UsersPage from './UsersPage';

const MemberLifecyclePage = lazy(() => import('./MemberLifecyclePage'));
const ProjectManagementPage = lazy(() => import('./ProjectManagementPage'));

const { TabPane } = Tabs;
const { Paragraph, Text, Title } = Typography;

type OrganizationSection = 'bootstrap' | 'directory' | 'people' | 'projects' | 'lifecycle';

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

function normalizeSection(value: string | null, canManageOrganization: boolean, platformAdmin: boolean): OrganizationSection {
  if (!canManageOrganization) return 'projects';
  if (value === 'bootstrap' && platformAdmin) return value;
  if (value === 'directory' || value === 'people' || value === 'projects' || value === 'lifecycle') return value;
  return 'people';
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
        description="仅在 DATA_RESET 空基线已验收、且收到独立 BOOTSTRAP baton 后使用。本操作只创建一个 active 客户组织和 CNY 零余额钱包；不会创建用户、项目、Provider/Feed 凭据、应用 Key 或测试流量。"
      />
      <Surface title="首客户初始化" description="后端会在同一事务中重新验证唯一 admin、最小组织壳、登录后允许状态和所有 reset-owned 表的严格空基线。">
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
              description={`已创建 ${result.organization_name}（ID ${result.organization_id}），钱包余额 ${result.wallet_balance_micros} micro-CNY。后续用户、项目与凭据仍需按 BOOTSTRAP 顺序单独创建。`}
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
  const orgId = useAuthStore(state => state.user?.org_id);
  const canManageOrganization = isOrgAdminRole(organizationRole);
  const platformAdmin = isPlatformAdminRole(organizationRole);
  const [searchParams, setSearchParams] = useSearchParams();
  const activeSection = normalizeSection(searchParams.get('section'), canManageOrganization, platformAdmin);

  const changeSection = (next: string) => {
    const params = new URLSearchParams(searchParams);
    if (next === 'people') params.delete('section');
    else params.set('section', next);
    setSearchParams(params, { replace: true });
  };

  if (!canManageOrganization) {
    return (
      <Suspense fallback={<SectionLoading />}>
        <ProjectManagementPage />
      </Suspense>
    );
  }

  return (
    <div className="ur-page ur-page-wide ur-organization-members-page">
      <PageHeader
        eyebrow="访问与安全"
        title="组织与成员"
        description="现在可直接创建和管理本地账号、角色及项目成员；企业目录投影延后到客户 POC，正式组织真值届时仍由企业 HCM/目录拥有。"
      />

      <Tabs activeKey={activeSection} onChange={changeSection} type="line">
        {platformAdmin ? (
          <TabPane tab="首客户初始化" itemKey="bootstrap">
            {activeSection === 'bootstrap' ? <FirstCustomerBootstrap /> : null}
          </TabPane>
        ) : null}
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
        <TabPane tab="组织架构（POC）" itemKey="directory">
          {activeSection === 'directory' ? <DirectoryFoundation orgId={orgId} /> : null}
        </TabPane>
        <TabPane tab="生命周期证据" itemKey="lifecycle">
          {activeSection === 'lifecycle' ? (
            <Suspense fallback={<SectionLoading />}>
              <MemberLifecyclePage embedded />
            </Suspense>
          ) : null}
        </TabPane>
      </Tabs>

      <Paragraph type="tertiary" className="ur-org-members-footnote">
        人员、登录账号、工作负载身份和 API Key Owner 不等价；无法证明的个人归属必须保留为共享或未归属。
      </Paragraph>
    </div>
  );
}
