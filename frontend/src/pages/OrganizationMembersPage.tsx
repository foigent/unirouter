import '../i18n/people';
import { translate as t, useI18n } from '../i18n';
import { lazy, Suspense, useEffect, useState } from 'react';
import { Banner, Button, Spin, Tabs, Tag, Typography } from '@douyinfe/semi-ui';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api/client';
import { isOrgAdminRole, organizationRoleForUser } from '../navigation';
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
  return <div className="ur-route-loading" role="status" aria-label={t("正在加载工作区")}><Spin size="large" /></div>;
}

function normalizeSection(value: string | null, canManageOrganization: boolean): OrganizationSection {
  if (!canManageOrganization) return 'projects';
  if (value === 'people' || value === 'projects') return value;
  return 'people';
}

function isAdvancedSection(value: string | null): value is AdvancedSection {
  return value === 'directory' || value === 'lifecycle';
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
  const [error, setError] = useState<{ detail?: string } | null>(null);
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
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
        if (!cancelled) setError({ detail: reason instanceof Error ? reason.message : undefined });
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
          <Title heading={4} style={{ margin: 0 }}>{t("企业组织目录")}</Title>
          <Text type="tertiary">{t("HCM/企业目录拥有人员、正式部门、经理和成本中心真值；UniRouter 只保存版本化本地投影。")}</Text>
        </div>
        <Tag color="grey">{t("POC 阶段接入")}</Tag>
      </div>

      <Banner
        type="info"
        closeIcon={null}
        description={t("当前没有可用于验证的企业环境，因此不连接 SCIM、飞书、钉钉、Entra ID、AD 或 HCM，也不接收目录凭据。真实目录接入延后到客户 POC；此页只冻结来源边界和未来同步状态，不展示模拟人员或部门。")}
      />

      <div className="ur-org-directory-foundation">
        <Surface title={t("数据所有权")} description={t("同一字段只有一个权威来源，避免 HCM、IdP 和产品本地配置互相覆盖。")}>
          <dl className="ur-financial-truth-detail">
            <dt>HCM / HRIS</dt><dd>{t("人员、正式部门、经理、任职状态、成本中心和生效日期")}</dd>
            <dt>{t("IdP / 企业目录")}</dt><dd>{t("登录账号、认证状态和目录组")}</dd>
            <dt>UniRouter</dt><dd>{t("产品角色、项目成员、API Key、高级模型限制和共享限额")}</dd>
            <dt>{t("其他 Gateway")}</dt><dd>{t("Tool 与 MCP 权限保持在各自产品")}</dd>
          </dl>
        </Surface>

        <Surface title={t("同步闭环")} description={t("真实接入后以本地投影服务请求路径，不把外部目录可用性放进模型请求关键路径。")}>
          <ol className="ur-plain-list">
            <li>{t("首次全量快照和稳定外部身份映射")}</li>
            <li>{t("事件/Webhook/Delta 增量和幂等版本检查")}</li>
            <li>{t("周期对账、Checkpoint、冲突与 Capture Gap")}</li>
            <li>{t("Joiner/Mover/Leaver 的本地暂停、撤权与历史有效期")}</li>
          </ol>
        </Surface>
      </div>

      {loading ? (
        <Surface>
          <div className="ur-route-loading" role="status" aria-label={t("正在读取企业目录状态")}><Spin size="large" /></div>
        </Surface>
      ) : error ? (
        <Surface>
          <EmptyState
            title={t("无法读取目录投影状态")}
            description={`${error.detail || t('目录投影状态读取失败')} · ${t('这不会影响本地账号或模型请求处理；外部目录也不在请求关键路径。')}`}
            actions={<Button onClick={() => setReloadKey(value => value + 1)}>{t("重试")}</Button>}
            minHeight={220}
          />
        </Surface>
      ) : sources.length === 0 ? (
        <Surface>
        <EmptyState
          title={t("尚未配置企业目录来源")}
          description={t("第一个真实 Connector 必须在客户 POC 中按目标企业选择；在此之前继续使用 UniRouter 本地账号、角色和项目成员能力，不创建通用高权限目录插件，也不把本地账号手工编辑冒充 HCM 同步。")}
          minHeight={220}
        />
        </Surface>
      ) : (
        <div className="ur-org-directory-foundation">
          <Surface title={t("目录来源健康")} description={t("连接、新鲜度、对账和覆盖是四个独立轴；任一轴异常都不能被一个绿色总状态掩盖。")}>
            <div className="ur-directory-source-list">
              {sources.map(source => (
                <div className="ur-directory-source-row" key={`${source.observation.authority}:${source.observation.source_ref}`}>
                  <div>
                    <Text strong>{source.observation.source_ref}</Text>
                    <Text type="tertiary">{source.observation.authority.toUpperCase()} · {source.observation.source_revision || t("尚无投影版本")}</Text>
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
          <Surface title={t("当前有效组织树")} description={t("只读取本地不可变投影；历史请求不会因后续组织变化被重写。")}>
            {tree.length === 0 ? (
              <Text type="tertiary">{t("当前没有可展示的有效组织单元；不会用模拟部门填充此区域。")}</Text>
            ) : (
              <dl className="ur-financial-truth-detail">
                {tree.map(unit => (
                  <div key={unit.unit_ref} className="ur-directory-unit-row">
                    <dt>{unit.name}</dt>
                    <dd>{unit.kind} · {unit.parent_unit_ref || t("根节点")} · {unit.source_revision}</dd>
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
  useI18n();
  const user = useAuthStore(state => state.user);
  const organizationRole = organizationRoleForUser(user);
  const customerContext = resolveCustomerContext(user);
  const orgId = effectiveCustomerOrgID(user);
  const canManageOrganization = isOrgAdminRole(organizationRole);
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
          eyebrow={t("部署设置")}
		  title={blocker.title}
		  description={t("正常的本地单客户部署会自动采用唯一根组织。这里出现阻断，表示数据库中没有根组织、存在多个无法判定的根组织，或组织已停用。")}
        />
		<Surface>
		  <EmptyState title={blocker.title} description={blocker.description} minHeight={260} />
		</Surface>
      </div>
    );
  }

  return (
    <div className="ur-page ur-page-wide ur-organization-members-page">
      <PageHeader
        eyebrow={t("访问与安全")}
        title={t("人员与权限")}
        description={t("管理这套部署唯一客户的账号、客户角色和项目成员。企业目录接入与离线合同检查属于高级准备项，不代表已经连接真实 HCM、SCIM 或 IdP。")}
      />

      <Tabs activeKey={activeSection} onChange={changeSection} type="line">
        <TabPane tab={t("账号与身份")} itemKey="people">
          {activeSection === 'people' ? <UsersPage embedded /> : null}
        </TabPane>
        <TabPane tab={t("项目与成员")} itemKey="projects">
          {activeSection === 'projects' ? (
            <Suspense fallback={<SectionLoading />}>
              <ProjectManagementPage embedded />
            </Suspense>
          ) : null}
        </TabPane>
      </Tabs>

      <Surface
        title={t("高级诊断与集成准备")}
        description={t("默认关闭。这里保存尚未连接真实目录时的来源边界和离线事件合同，不参与日常账号、项目或请求处理。")}
      >
        <Button aria-expanded={advancedOpen} onClick={toggleAdvanced}>
          {advancedOpen ? t("收起高级诊断") : t("展开高级诊断")}
        </Button>
        {advancedOpen ? (
          <div style={{ marginTop: 16 }}>
            <Banner
              type="warning"
              closeIcon={null}
              description={t("除非页面明确显示真实目录来源及同步版本，否则下方内容只是接入准备或离线校验，不能作为客户目录已连接、人员已同步或权限已实际撤销的证明。")}
              style={{ marginBottom: 12 }}
            />
            <Tabs activeKey={advancedSection} onChange={changeAdvancedSection} type="line">
              <TabPane tab={t("目录接入准备")} itemKey="directory">
                {advancedSection === 'directory' ? <DirectoryFoundation orgId={orgId} /> : null}
              </TabPane>
              <TabPane tab={t("离线生命周期合同")} itemKey="lifecycle">
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

        {t("人员、登录账号、工作负载身份和 API Key Owner 不等价；无法证明的个人归属必须保留为共享或未归属。")}
      </Paragraph>
    </div>
  );
}
