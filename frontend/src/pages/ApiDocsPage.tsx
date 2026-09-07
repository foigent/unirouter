import { useEffect, useMemo, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { Banner, Input, Typography, Table, Tag, Select } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { EmptyState, PageHeader, Surface } from '../ui';
import {
  authzContextForUser,
  canInvokeProject,
  canonicalProjectMembershipRole,
  hasLicensedFeature,
  isOrgAdminRole,
  licenseRestrictionMessage,
} from '../navigation';
import { useAuthStore } from '../store/auth';
import { loadAllPages } from '../utils/loadAllPages';
import { projectDisplayName } from '../utils/projectDisplay';
import ProjectApiQuickstart from '../features/apiDocs/ProjectApiQuickstart';
import {
  type EndpointGroup,
  visibleEndpointGroups as filterVisibleEndpointGroups,
} from '../features/apiDocs/access';

const { Text } = Typography;

const legacyModelMutationRetiredDescription = '已退役：固定返回 HTTP 410（legacy_model_mutation_retired）；请使用 canonical 模型部署与发布 API';

const apiAreaLabels: Record<string, string> = {
  Auth: '认证与会话',
  Platform: '服务状态',
  Gateway: '模型网关',
  'Async Media': '异步媒体',
  'Private Integration': '私有集成',
  'Access Provisioning': '访问开通',
  Admin: '平台管理',
  'Model Governance': '模型治理',
  Notifications: '通知',
  'Error Docs': '错误说明',
};

function contractStateLabel(value?: string) {
  if (value === 'runtime_supported') return '运行时支持';
  if (value === 'protocol_compatible') return '协议兼容';
  if (value === 'contract_only') return '仅合同声明';
  if (value === 'verified') return '已独立验证';
  if (value === 'unsupported') return '未支持';
  return value || '-';
}

const endpointGroups: EndpointGroup[] = [
  {
    area: 'Auth',
    endpoints: [
      ['POST', '/api/v1/auth/login', '登录并返回 JWT'],
      ['POST', '/api/v1/auth/logout', '注销并撤销当前会话'],
      ['GET', '/api/v1/auth/oidc/config', 'OIDC 登录入口配置'],
      ['GET', '/api/v1/auth/oidc/start', 'OIDC 登录跳转地址'],
      ['GET', '/api/v1/auth/oidc/callback', 'OIDC 回调处理'],
      ['GET', '/api/v1/me', '当前登录用户信息'],
      ['PUT', '/api/v1/me/password', '修改当前用户密码'],
    ],
  },
  {
    area: 'Platform',
    endpoints: [
      ['GET', '/', '服务元信息'],
      ['GET', '/docs', '后端 API 合同索引'],
      ['GET', '/health/live', '存活探针'],
      ['GET', '/health/ready', '依赖就绪探针'],
      ['GET', '/metrics', 'Prometheus 指标（运行时可配置）'],
    ],
  },
  {
    area: 'Gateway',
    endpoints: [
      ['GET', '/v1/models', '兼容 OpenAI 协议的模型列表'],
      ['POST', '/v1/chat/completions', '兼容 OpenAI 协议的对话补全'],
      ['POST', '/v1/chat/completions/preflight', '使用相同 Chat 请求只读核对合同、路由资格与出站参数；parameter_errors 提示不支持的输出参数及接受字段；不调用 Provider，不代表真实任务已通过'],
      ['POST', '/v1/embeddings', '兼容 OpenAI 协议的向量接口'],
      ['POST', '/v1/responses', '严格 OpenAI Responses 非流式文本子集；Bearer 鉴权、仅元数据幂等、绕过响应缓存'],
      ['POST', '/v1/messages', '严格 Anthropic Messages 非流式文本子集；x-api-key + anthropic-version、仅元数据幂等、绕过响应缓存'],
      ['POST', '/v1/agent-capabilities/exchange', '根 API Key 显式登记 Supervisor/工作流并换取 5 分钟能力令牌'],
      ['POST', '/v1/agent-capabilities/delegate', '父 Agent 显式登记一个子 Agent；权限只能缩小'],
      ['POST', '/v1/agent-capabilities/refresh', '约第 3 分钟刷新；并发刷新收敛为同一个令牌'],
      ['POST', '/v1/agent-capabilities/revoke', '根 API Key 撤销工作流，阻止所有副本的新准入'],
    ],
  },
  {
    area: 'Async Media',
    endpoints: [
      ['POST', '/v1/media/jobs', '项目根 API Key 提交单张 GPT Image V1 异步任务；必须提供 Idempotency-Key'],
      ['GET', '/v1/media/jobs/{id}', '读取同一项目内的任务状态，不返回 Prompt、凭据或对象地址'],
      ['POST', '/v1/media/jobs/{id}/cancel', '请求取消；终态成功竞争返回 too_late，不自动退款'],
      ['GET', '/v1/media/jobs/{id}/content', '在固定七天期限内通过鉴权代理读取图像，不签发公开 URL'],
      ['GET', '/api/v1/media-jobs/contract', '读取 Image V1 运行边界、启用状态、保留和计费事实合同'],
      ['GET', '/api/v1/media-jobs', '按授权项目读取图像任务列表'],
      ['GET', '/api/v1/media-jobs/{id}', '在常驻检查器中读取任务元数据'],
      ['POST', '/api/v1/media-jobs/{id}/cancel', '项目操作员请求取消任务'],
      ['GET', '/api/v1/media-jobs/{id}/content', '控制台通过 JWT 鉴权代理读取未到期图像'],
    ],
  },
  {
    area: 'Private Integration',
    endpoints: [
      ['GET', '/api/v1/integrations/xinsight/completed-executions', 'X-Insight 使用项目绑定、网络绑定且不可调用模型的专用 service key 拉取终态执行元数据'],
    ],
  },
  {
    area: 'Access Provisioning',
    endpoints: [
      ['POST', '/api/v1/access-requests', '提交本人、受托或管理员直接授权请求；迁移、Bootstrap 与恢复模式不接受公共客户端自报'],
      ['GET', '/api/v1/access-requests', '分页读取本人请求，或在 live 项目管理权限下读取项目审核队列'],
      ['GET', '/api/v1/access-requests/{ref}', '读取一个 actor-scoped 请求；不可见对象与不存在对象同为 404'],
      ['POST', '/api/v1/access-requests/{ref}/decision', '按 expected revision 审批或拒绝 needs_review 请求'],
      ['POST', '/api/v1/access-requests/{ref}/cancel', '提交者或 Workload sponsor 幂等取消尚未决策的请求'],
      ['GET', '/api/v1/access-grants', '分页读取当前账号可见的项目访问授权'],
      ['GET', '/api/v1/access-grants/{ref}', '读取一个 actor-scoped Access Grant'],
      ['POST', '/api/v1/access-grants/{ref}/suspend', '管理员暂停 Access Grant'],
      ['POST', '/api/v1/access-grants/{ref}/resume', '管理员在 live subject 复核后恢复 Access Grant'],
      ['POST', '/api/v1/access-grants/{ref}/revoke', '管理员永久撤销 Grant 及其逻辑 Credential families'],
      ['GET', '/api/v1/access-grants/{ref}/credentials', '分页读取无 secret 的逻辑 Credential family 与版本元数据'],
      ['POST', '/api/v1/access-grants/{ref}/credentials', '为有权主体签发一次性可见的 Credential secret'],
      ['POST', '/api/v1/access-grants/{ref}/credentials/{key_id}/rotate', '原子替换指定 secret version；旧 secret 在提交后立即失效'],
      ['POST', '/api/v1/access-grants/{ref}/credentials/{key_id}/revoke', '撤销指定逻辑 Credential family 及全部版本'],
      ['GET', '/api/v1/access-provisioning/legacy-preview', '组织管理员只读预览 legacy Key 与 Workload 分类，不执行回填'],
      ['GET', '/api/v1/model-readiness', 'connection 模式无需项目即可检查网关授权、供应商连接/凭证与路由；caller 模式再检查项目调用条件；不发 Provider/Feed 请求'],
    ],
  },
  {
    area: '项目工作台',
    endpoints: [
      ['GET', '/api/v1/projects', '当前账号可访问项目'],
      ['GET', '/api/v1/projects/{id}', '项目详情'],
      ['PUT', '/api/v1/projects/{id}', '更新项目'],
      ['GET', '/api/v1/projects/{id}/model-sets', '项目模型集合绑定'],
      ['PUT', '/api/v1/projects/{id}/model-sets', '更新项目模型集合绑定'],
      ['GET', '/api/v1/projects/{id}/members', '查看项目成员与同组织可加入用户'],
      ['PUT', '/api/v1/projects/{id}/members/{user_id}', '显式创建或更新项目成员关系；账号组织角色与项目成员角色独立，普通 project_admin 不得覆盖既有 project_admin'],
      ['DELETE', '/api/v1/projects/{id}/members/{user_id}', '移除显式项目成员关系；member 可保留零项目，普通 project_admin 不得移除自己或其他 project_admin'],
      ['GET', '/api/v1/projects/{id}/budget', '查看项目预算（账务管理员仅有预算权限）'],
      ['PUT', '/api/v1/projects/{id}/budget', '仅更新项目预算金额与周期并原子记录审计'],
      ['GET', '/api/v1/keys?project_id={id}', '项目访问凭证列表'],
      ['GET', '/api/v1/keys/{id}', '访问凭证详情'],
      ['POST', '/api/v1/keys', '创建项目访问凭证'],
      ['PUT', '/api/v1/keys/{id}', '更新访问凭证'],
      ['POST', '/api/v1/keys/{id}/disable', '停用访问凭证；project_user 仅限本人 personal credential，project_admin 可管理本项目 active credential'],
      ['GET', '/api/v1/model-catalog?project_id={id}', '项目可用模型目录'],
      ['GET', '/api/v1/logs?project_id={id}', '项目调用日志'],
      ['GET', '/api/v1/logs/{id}', '请求日志详情'],
      ['GET', '/api/v1/logs/{id}/trace', '请求路径事件'],
      ['GET', '/api/v1/logs?project_id={id}&request_id={rid}', '按服务器操作 ID（Request ID）查询项目请求'],
      ['POST', '/v1/chat/completions', '请求调试台直接使用项目访问凭证调用'],
    ],
  },
  {
    area: '产品与运行状态',
    endpoints: [
      ['GET', '/api/v1/usage/analytics', '用量钻取聚合与 CSV 导出'],
      ['GET', '/api/v1/internal-status', '内部状态与事件历史'],
      ['GET', '/api/v1/runtime/status', '运行时、容量与外部监控状态'],
      ['GET', '/api/v1/error-docs', '错误码文档和 doc_url 注册表'],
    ],
  },
  {
    area: '成本优化',
    endpoints: [
      ['GET', '/api/v1/cost-optimization/summary', '读取 Prompt Cache 净节省、证据覆盖与未接入能力边界'],
      ['GET', '/api/v1/cost-optimization/trend', '按日读取 Prompt Cache 节省趋势'],
      ['GET', '/api/v1/cost-optimization/breakdown', '按项目、模型、供应商或上游通道拆解节省'],
      ['GET', '/api/v1/cost-optimization/opportunities', '读取由确定性证据规则生成的复核候选'],
    ],
  },
  {
    area: 'Admin',
    endpoints: [
      ['GET', '/api/v1/dashboard/stats', 'Dashboard 聚合数据'],
      ['GET', '/api/v1/channels', '上游通道列表'],
      ['POST', '/api/v1/channels', legacyModelMutationRetiredDescription],
      ['GET', '/api/v1/channels/{id}', '上游通道详情'],
      ['PUT', '/api/v1/channels/{id}', legacyModelMutationRetiredDescription],
      ['DELETE', '/api/v1/channels/{id}', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/channels/{id}/test', legacyModelMutationRetiredDescription],
      ['GET', '/api/v1/channels/{id}/provider-models', '用已保存凭证读取供应商当前可用的模型 ID，不回显凭证'],
      ['GET', '/api/v1/model-endpoints', '读取模型、供应商端点、真实检测与当前路由的唯一权威投影'],
      ['GET', '/api/v1/model-deployments', '读取模型部署五条独立事实轴；安全只读，不要求 Gateway License'],
      ['GET', '/api/v1/model-deployments/{deploymentID}/model-discovery', '使用所属连接和明确凭证读取北京业务空间的模型目录；仅管理员可用，目录不证明调用权限或执行资格，不运行推理探测'],
      ['GET', '/api/v1/model-deployments/{deploymentID}', '按持久 deployment ID 和 operation 精确读取，不回退到其他端点'],
      ['POST', '/api/v1/model-deployments', '在同一事务中创建或精确重放连接、公共模型、部署、凭证绑定与 operation；仅对编译合同与数据库支持合同完全一致的 M88 DeepSeek Flash Chat streaming tuple 写入 enabled/declared 能力投影，不调用供应商，也不产生探测、批准、路由或 Release；要求 Gateway License 和 Idempotency-Key（24 小时、按操作者隔离，不保存请求或密钥）'],
      ['POST', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/probes', '以精确 deployment、operation 与 credential binding 对 Chat Completions、Embeddings、Responses 或 Messages 执行有界真实探测；后两者必须使用 operation 绑定的 endpointprotocol profile 与不可变 public path，本地 fixture 不能充当探测'],
      ['POST', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/production-approvals', '仅 platform_admin 可追加仍然 current 的 Responses/Messages HTTPS 结构批准；要求 Gateway License，但单独批准不产生 M77 release-item 执行绑定、在线路由或生产流量证明'],
      ['GET', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/capability-probe-plan', '无付费调用地读取精确共享探测计划；需可管理当前绑定'],
      ['POST', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/capability-probe-groups', '显式发起一次共享探测，原子保存逐项事实；不会自动批准或重试'],
      ['GET', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/capability-probe-groups/{sourceRef}', '按完整观察身份回读；当前资格独立计算，不重发请求'],
      ['GET', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}', '读取精确 typed capability 的探测证据、首选批准与当前资格；原始 capability 投影不构成路由权威'],
      ['POST', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/probes', '运行服务端固定的 typed capability probe；M88 仅开放 DeepSeek V4 Flash × Chat × streaming，成功不会自动批准路由'],
      ['POST', '/api/v1/model-deployments/{deploymentID}/operations/{operationID}/capabilities/{capabilityID}/routing-approvals', '仅 platform_admin 将一份仍然 current 的 typed probe 追加为当前环境路由资格；不等于 released、SLA 或长期稳定证明'],
      ['POST', '/api/v1/provider-connections/{connectionID}/pause', '紧急暂停一个供应商连接；不改变其下模型部署状态，缺少或过期 Gateway License 时仍可止损'],
      ['POST', '/api/v1/provider-credential-bindings/{bindingID}/revoke', '不可逆撤销并擦除一个供应商凭证密文；保留身份和证据关系，缺少或过期 Gateway License 时仍可止损'],
      ['GET', '/api/v1/model-deployment-releases', '按组织读取公共模型、服务端权威 baseline/候选项和可操作 canonical v2 release；platform_admin、org_admin、org_viewer 可读且不要求 Gateway License'],
      ['POST', '/api/v1/model-deployment-releases', '以服务端 baseline、精确 deployment/operation 选择和 Idempotency-Key 原子编写 sealed validated release；要求组织写角色与 Gateway License，但编写不会激活或改变流量'],
      ['GET', '/api/v1/model-deployment-releases/{releaseRef}', '读取一个 canonical v2 release、当前运行槽与服务端动作描述；platform_admin、org_admin、org_viewer 可读且不要求 Gateway License'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/activate-initial', '以 Idempotency-Key 将无父级 validated release 原子设为首个 stable；要求 Gateway License'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/activate-successor', '以 Idempotency-Key 原子激活首个不可变 successor、稳定请求 Alias、目录和项目模型集；只支持首次 canonical-to-alias 切换，要求 Gateway License'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/activate-staging-successor', '以独立 Idempotency namespace 原子替换从未获得 Slot、sealed/可执行 Release、Alias 或既有 successor receipt 的 staging canonical，并建立首个稳定请求身份；要求 Gateway License'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/start-canary', '以 release/slot 双版本和 Idempotency-Key 启动最高 50% 灰度；要求 Gateway License'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/promote', '把精确 active canary 提升为 stable；要求 Gateway License 和当前生产资格'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/cancel-canary', '无 License 也可执行的安全收缩：移除精确 canary 并保留 stable parent；仍要求组织写角色和 Idempotency-Key'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/rollback-stable', '恢复当前 active release 保留的 stable parent；与 cancel-canary 分离，要求 Gateway License、当前父级资格和 Idempotency-Key'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/cutover-legacy', '仅在 Chat 与 Embeddings 可执行计划完整同等后原子替换精确 sealed v1 stable parent；要求组织写角色、Gateway License 和 Idempotency-Key'],
      ['POST', '/api/v1/model-deployment-releases/{releaseRef}/rollback-legacy', '安全收缩到精确 sealed 且当前仍可执行的 v1 parent；v2 健康失败不阻断，仍要求组织写角色和 Idempotency-Key，但不要求 Gateway License'],
      ['POST', '/api/v1/model-endpoints/activate', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/model-endpoints/canary', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/model-endpoints/recover', legacyModelMutationRetiredDescription],
      ['GET', '/api/v1/provider-packs', '列出离线 fixture-verified Provider Pack、能力和精确摘要'],
      ['GET', '/api/v1/routing/preview', '只读预览模型的路由候选、排除原因与快照状态'],
      ['GET', '/api/v1/runtime-changes', '分页读取确定性发布记录，并区分当前稳定路由与保留的旧审计记录'],
      ['POST', '/api/v1/runtime-changes', legacyModelMutationRetiredDescription],
      ['GET', '/api/v1/runtime-changes/slot', '读取一个模型的稳定与灰度发布槽'],
      ['GET', '/api/v1/runtime-changes/{ref}', '读取发布候选、Hash、Pack Pin、模拟报告和生命周期'],
      ['POST', '/api/v1/runtime-changes/{ref}/validate', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/runtime-changes/{ref}/activate-initial', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/runtime-changes/{ref}/shadow', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/runtime-changes/{ref}/canary', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/runtime-changes/{ref}/promote', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/runtime-changes/{ref}/rollback', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/runtime-changes/{ref}/reject', legacyModelMutationRetiredDescription],
      ['GET', '/api/v1/channel-keys', '上游凭证列表'],
      ['POST', '/api/v1/channel-keys', legacyModelMutationRetiredDescription],
      ['PUT', '/api/v1/channel-keys/{id}', legacyModelMutationRetiredDescription],
      ['POST', '/api/v1/channel-keys/{id}/rotate', legacyModelMutationRetiredDescription],
      ['DELETE', '/api/v1/channel-keys/{id}', legacyModelMutationRetiredDescription],
      ['GET', '/api/v1/keys', '访问凭证列表'],
      ['GET', '/api/v1/organizations', '返回这套部署唯一的客户组织；未初始化、不唯一或已停用时不猜测目标'],
      ['POST', '/api/v1/bootstrap/first-customer', '从精确 strict-reset、runtime-reset 空壳或 fresh-migration 前态初始化首客户；dry_run=true 只读预检并列出空壳归位对象，提交时重新校验；fresh production 保持发布证据门禁，漂移返回 409'],
      ['GET', '/api/v1/limit-policies', '查看跨副本 RPM/RPS/TPM/并发策略及其版本'],
      ['POST', '/api/v1/limit-policies', '创建 disabled/shadow/enforce 共享限额策略'],
      ['PUT', '/api/v1/limit-policies/{id}', '按当前 version 更新策略；冲突返回 409，成功后版本递增'],
      ['GET', '/api/v1/guardrail-policies', '查看输入、非流式和流式输出安全策略、检测器版本与流式能力'],
      ['POST', '/api/v1/guardrail-policies', '创建影子观察、审计、脱敏或阻断策略'],
      ['PUT', '/api/v1/guardrail-policies/{id}', '按当前 version 更新安全策略；平台强制规则只能叠加收紧'],
      ['POST', '/api/v1/projects', '创建项目'],
      ['GET', '/api/v1/logs', '请求日志查询与筛选'],
      ['GET', '/api/v1/logs/{id}/trace', '请求路径事件'],
      ['GET', '/api/v1/wallet', '资金账户信息'],
      ['POST', '/api/v1/wallet/topup', '资金账户充值'],
      ['POST', '/api/v1/wallet/adjust', '资金账户调整'],
      ['GET', '/api/v1/wallet/transactions', '资金流水列表'],
      ['GET', '/api/v1/wallet/transactions/export', '资金流水导出'],
      ['GET', '/api/v1/grants', '授信记录列表'],
      ['POST', '/api/v1/grants', '创建授信记录'],
      ['GET', '/api/v1/audit-logs', '审计日志'],
      ['GET', '/api/v1/data-exchange/items', '数据交换审批候选'],
      ['POST', '/api/v1/data-exchange/items', '创建审批候选或受控场景授权'],
      ['PUT', '/api/v1/data-exchange/items/{id}', '更新审批状态'],
      ['GET', '/api/v1/data-exchange/exchanges', '出站交换账本与最新回执'],
      ['POST', '/api/v1/data-exchange/exchanges', '登记已生成的交换制品'],
      ['GET', '/api/v1/data-exchange/exchanges/{id}/activity', '查看候选、交付和回执历史'],
      ['POST', '/api/v1/data-exchange/exchanges/{id}/deliveries', '登记交付或重投'],
      ['POST', '/api/v1/data-exchange/exchanges/{id}/receipts', '登记 X-Insight 回执'],
      ['POST', '/api/v1/data-exchange/exchanges/{id}/artifact-delete', '标记制品已删除'],
      ['GET', '/api/v1/data-exchange/xinsight/connections/{project_id}', '读取项目级 X-Insight 实时连接状态'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/install', '校验完整 frozen invitation，并幂等登记 Connector installation 与加密 invitation'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/pause', '暂停实时连接但不禁用凭证'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/resume', '恢复实时连接'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/credentials', '登记 receiver-generated 初始 staged feed-only 凭证（不接收或返回明文 token）'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/credentials/activate', '激活已安装的初始 staged 凭证'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/credentials/{credential_id}/revoke', '不可逆撤销 feed 凭证并暂停连接'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/rotation/prepare', '用非秘密 registration bundle 准备两阶段 staged 替换凭证'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/rotation/activate', '原子晋升 staged 并撤销旧凭证'],
      ['POST', '/api/v1/data-exchange/xinsight/connections/{project_id}/rotation/cancel', '撤销 staged 并保留旧凭证'],
      ['GET', '/api/v1/settings', '部署级设置（仅平台管理员）'],
      ['PUT', '/api/v1/settings/{key}', '更新部署级设置（仅平台管理员）'],
      ['GET', '/api/v1/license', 'License 状态（仅平台管理员）'],
      ['PUT', '/api/v1/license', '导入离线 License（仅平台管理员）'],
      ['GET', '/api/v1/users', '用户列表'],
      ['POST', '/api/v1/users', '创建用户'],
      ['PUT', '/api/v1/users/{id}', '更新用户'],
      ['DELETE', '/api/v1/users/{id}', '删除用户'],
      ['GET', '/api/v1/sensitive-words', '全局输入阻断词库（仅平台管理员）'],
      ['POST', '/api/v1/sensitive-words', '新增全局输入阻断词（仅平台管理员）'],
      ['PUT', '/api/v1/sensitive-words', '替换全局输入阻断词（仅平台管理员）'],
      ['DELETE', '/api/v1/sensitive-words', '删除全局输入阻断词（仅平台管理员）'],
    ],
  },
  {
    area: 'Model Governance',
    endpoints: [
      ['GET', '/api/v1/model-call-authorizations/contract', '读取只收窄权限的模型调用 PEP 合同；不授予 Tool/MCP 权限'],
      ['GET', '/api/v1/model-call-authorizations', '按组织读取模型调用授权策略'],
      ['POST', '/api/v1/model-call-authorizations', '创建访问凭证、项目、人员或 Agent 维度的授权草稿'],
      ['POST', '/api/v1/model-call-authorizations/{ref}/activate', '按修订激活拒绝优先、允许范围求交集的策略'],
      ['POST', '/api/v1/model-call-authorizations/{ref}/revoke', '按修订撤销一条生效策略并原子记录审计'],
      ['GET', '/api/v1/workload-applications/contract', '读取应用身份、客户端兼容等级和可用性轴合同；不授予 Tool/MCP 权限'],
      ['GET', '/api/v1/workload-applications', '按组织/项目读取应用与 Agent 登记'],
      ['POST', '/api/v1/workload-applications', '在同租户启用项目中登记企业应用、Agent、工作流或隔离 Adapter'],
      ['GET', '/api/v1/workload-applications/{ref}', '读取应用身份、项目、Client Profile 和凭证汇总'],
      ['PUT', '/api/v1/workload-applications/{ref}', '按修订更新非身份元数据和启停状态'],
      ['POST', '/api/v1/workload-applications/{ref}/archive', '归档应用并停用绑定凭证，不删除历史执行证据'],
      ['GET', '/api/v1/workload-applications/{ref}/credentials', '读取应用绑定凭证元数据，不返回完整密钥'],
      ['POST', '/api/v1/workload-applications/{ref}/credentials/{key_id}/bind', '把同租户同项目的未绑定普通 Key 绑定到应用'],
      ['GET', '/api/v1/workload-applications/{ref}/readiness', '统一解析客户端、端点、凭证、授权、限额、模型和上游路由阻塞原因'],
      ['GET', '/api/v1/deployment/contract', '读取 Deployment/Endpoint Profile 合同；不保存 TLS 私钥、不修改运行时'],
      ['GET', '/api/v1/deployment/profiles', '读取 ECS、本地 Compose 与 Kubernetes 部署期望状态（仅平台管理员）'],
      ['POST', '/api/v1/deployment/profiles', '登记部署期望状态，不执行 Ingress 或运行时变更（仅平台管理员）'],
      ['PUT', '/api/v1/deployment/profiles/{ref}', '按修订更新部署期望状态，不执行运行时变更（仅平台管理员）'],
      ['GET', '/api/v1/deployment/endpoints', '读取非秘密 Gateway Endpoint 期望与观测状态'],
      ['POST', '/api/v1/deployment/endpoints', '登记 Gateway Endpoint；公网必须 HTTPS，拒绝私钥内容（仅平台管理员）'],
      ['PUT', '/api/v1/deployment/endpoints/{ref}', '更新 Endpoint 期望状态且保持观测状态不变（仅平台管理员）'],
      ['GET', '/api/v1/model-prices', '查看 CNY/百万 tokens 基础价格与供应商 Prompt Cache 倍率（平台、组织或账务管理员）'],
      ['POST', '/api/v1/model-prices', '创建或覆盖 CNY/百万 tokens 基础价格与 Prompt Cache 倍率（仅平台管理员）'],
      ['PUT', '/api/v1/model-prices/{id}', '更新 CNY/百万 tokens 基础价格与 Prompt Cache 倍率（仅平台管理员）'],
      ['DELETE', '/api/v1/model-prices/{id}', '删除基础模型价格（仅平台管理员）'],
      ['GET', '/api/v1/model-names', '模型名称列表'],
      ['GET', '/api/v1/model-execution-profiles', '读取版本冻结的模型合同；source=published 分页读取正式发布的精确 Definition，public_model_id 按稳定模型筛选，两者互斥。候选合同不代表资格或激活，不自动创建价格或凭证'],
      ['GET', '/api/v1/model-registry', '唯一客户的模型注册表；部署管理员也自动绑定该客户上下文'],
      ['POST', '/api/v1/model-registry', '在本组织创建模型注册记录；跨组织请求返回 403'],
      ['PUT', '/api/v1/model-registry/{id}', '更新本组织记录；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['GET', '/api/v1/invocation-optimization/contract', '读取强制 opt-in、最高 10% Canary、无明文证据和 Exact Cache 运行时就绪状态'],
      ['GET', '/api/v1/invocation-optimization/policies', '读取组织级调用优化策略与 Draft/Validated/Shadow/Canary/Rolled back 阶段'],
      ['POST', '/api/v1/invocation-optimization/policies', '创建默认关闭的调用优化草稿；不读写缓存、不修改 Prompt'],
      ['GET', '/api/v1/invocation-optimization/policies/{ref}', '读取一条租户隔离策略与精确修订'],
      ['GET', '/api/v1/invocation-optimization/policies/{ref}/readiness', '读取当前修订的 Cache 运行时、Shadow 样本、eligible 与失败门禁；不授予启用或流量权限'],
      ['PUT', '/api/v1/invocation-optimization/policies/{ref}', '按 expected_revision 更新草稿或已回滚策略；不能关闭请求 opt-in'],
      ['POST', '/api/v1/invocation-optimization/policies/{ref}/validate', '校验并冻结策略修订，不改变请求执行'],
      ['POST', '/api/v1/invocation-optimization/policies/{ref}/shadow', '启动元数据 Shadow；不读缓存响应、不修改 Prompt'],
      ['POST', '/api/v1/invocation-optimization/policies/{ref}/canary', '在 Shadow 样本、质量、运行时门禁通过后启动最高 10% 确定性灰度'],
      ['POST', '/api/v1/invocation-optimization/policies/{ref}/rollback', '停止后续优化分配并保留不可变证据'],
      ['GET', '/api/v1/model-catalog', '组织模型目录；org_viewer 可只读本组织，member 必须提供 project_id 并通过 live membership'],
      ['GET', '/api/v1/model-aliases', '唯一客户的模型别名；部署管理员也自动绑定该客户上下文'],
      ['POST', '/api/v1/model-aliases', '在本组织创建模型别名；跨组织请求返回 403'],
      ['PUT', '/api/v1/model-aliases/{id}', '更新本组织别名；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['GET', '/api/v1/model-sets', '唯一客户的模型集合；项目管理员只读其有权项目的候选集合'],
      ['POST', '/api/v1/model-sets', '原子创建本组织模型集合及其全部初始条目'],
      ['GET', '/api/v1/model-sets/{id}/items', '读取本组织模型集合条目；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['PUT', '/api/v1/model-sets/{id}/items', '原子替换本组织模型集合条目；不会暴露部分结果'],
      ['GET', '/api/v1/org-model-prices', '查看唯一客户的计费倍率；部署管理员自动绑定客户上下文'],
      ['POST', '/api/v1/org-model-prices', '创建或覆盖组织计费倍率（仅平台管理员）'],
      ['PUT', '/api/v1/org-model-prices/{id}', '更新组织计费倍率（仅平台管理员）'],
      ['DELETE', '/api/v1/org-model-prices/{id}', '删除组织计费倍率（仅平台管理员）'],
      ['GET', '/api/v1/reconciliation', '估算/实际用量及供应商 Prompt Cache 分项对账'],
      ['POST', '/api/v1/provider-cost/reconciliations', '平台管理员处理未决供应商成本；请求不接受金额，collect 仅重放权威用量与冻结客户价；observation 的 refund/subsidy 只生成零收费收据，不写钱包交易'],
      ['GET', '/api/v1/provider-cost/pending', '平台管理员按明确组织查看最多 100 条未决供应商成本元数据，并返回请求冻结时的 billing_mode'],
      ['GET', '/api/v1/provider-cogs/offline-contract', '查看供应商成本离线预览合同；不接凭据、不访问供应商、不保存数据'],
      ['POST', '/api/v1/provider-cogs/offline-preview', '在内存中校验供应商成本证据；聚合成本不归因且不会修改客户账'],
      ['GET', '/api/v1/governance-simulation/offline-contract', '平台管理员读取离线路由/策略模拟与 Provider Change Firewall 合同；不接收凭据、不提升运行时'],
      ['POST', '/api/v1/governance-simulation/offline-preview', '平台管理员用同一套路由内核比较不可变快照；shadow/canary/enforced 仅为模拟目标模式'],
      ['GET', '/api/v1/member-lifecycle/offline-contract', '平台或组织管理员读取 IdP 中立的成员生命周期合同；不选择 SCIM 供应商'],
      ['POST', '/api/v1/member-lifecycle/offline-preview', '离线评估创建、更新、暂停和退场事件；不修改用户、会话、成员、能力或目录'],
      ['GET', '/api/v1/enterprise-directory/sources', '平台或组织管理员读取目录来源元数据及连接、新鲜度、对账和覆盖健康；不返回凭据'],
      ['GET', '/api/v1/enterprise-directory/tree', '按明确 source_ref 和生效时间读取本地组织投影；请求路径不访问外部 HCM/IdP'],
      ['GET', '/api/v1/enterprise-directory/iam-actions', '读取租户内持久化的本地 IAM 收敛证据；无目录写入或权限授予能力'],
      ['GET', '/api/v1/enterprise-finance/fiscal-periods', '读取租户财务期间及版本化关账状态'],
      ['POST', '/api/v1/enterprise-finance/fiscal-periods', '创建不重叠且初始为 open 的财务期间'],
      ['POST', '/api/v1/enterprise-finance/fiscal-periods/{period_ref}/transition', '按 expected_version 和原因推进财务期间；closed 不可重开'],
      ['GET', '/api/v1/enterprise-finance/cost-centers', '按生效时间读取来源修订绑定的成本中心主数据投影'],
      ['POST', '/api/v1/enterprise-finance/cost-centers', '追加成本中心不可变版本；不宣称 ERP/HCM 真值'],
      ['GET', '/api/v1/enterprise-finance/budgets', '读取企业预算最新版本；默认仅报表且不改变 wallet 结算'],
      ['POST', '/api/v1/enterprise-finance/budgets', '创建企业预算草稿'],
      ['POST', '/api/v1/enterprise-finance/budgets/{budget_ref}/approve', '用 expected_version 与 maker-checker 追加批准版本'],
      ['GET', '/api/v1/enterprise-finance/budget-positions', '读取预算、精确分摊实际额和证据覆盖；承诺与预测未建模时显式显示缺口'],
      ['GET', '/api/v1/enterprise-finance/rate-cards', '读取各经济层最新 Rate Card、来源证据与请求定价权威状态'],
      ['POST', '/api/v1/enterprise-finance/rate-cards', '创建四类 Token 绝对单价及可选非 Token 单位价格的 Rate Card 草稿；可提交完整 UTC 日内价带；标识由服务端生成'],
      ['POST', '/api/v1/enterprise-finance/rate-cards/import-official-draft', '提交 HTTPS 来源、修订、获取时间、原始载荷 SHA-256、转换版本、绝对单价及可选 UTC 日内价带，只生成待复核目录价草稿；不联网、不自动激活'],
      ['POST', '/api/v1/enterprise-finance/rate-cards/bootstrap-legacy-drafts', '将旧 model_prices 和组织倍率显式转换成带来源证据的目录价草稿；缺 provider 跳过，不会自动切换运行模式'],
      ['POST', '/api/v1/enterprise-finance/rate-cards/preflight', '切换前逐模型验证 active Rate Card 完整性、合同优先和 provider 唯一性；只读且不修改费率'],
      ['POST', '/api/v1/enterprise-finance/rate-cards/{rate_card_ref}/approve', '用 expected_version 与 maker-checker 追加批准版本'],
      ['POST', '/api/v1/enterprise-finance/rate-cards/{rate_card_ref}/activate', '校验四项价格与完整 UTC 日内价带后激活，或用精确 supersedes ref/version 原子替换当前费率；过期来源拒绝激活'],
      ['GET', '/api/v1/enterprise-finance/pricing-snapshots/{request_ref}', '读取租户内不可变请求计价快照；不提供公开写入端点'],
      ['GET', '/api/v1/enterprise-finance/allocation-runs', '按财务期间读取来源修订与规则修订绑定的不可变分摊运行'],
      ['GET', '/api/v1/enterprise-finance/allocation-runs/{allocation_run_ref}', '读取单次分摊运行及全部请求级结果'],
      ['POST', '/api/v1/enterprise-finance/allocation-runs', '写入完整直接、规则、未分摊与冲突结果；不修改客户结算'],
      ['GET', '/api/v1/enterprise-finance/allocation-summary', '读取期间 Source 总额、覆盖率、未决项和关账就绪状态'],
      ['GET', '/api/v1/enterprise-finance/adjustment-journals', '读取关账后报表调整与冲销流水；GL 导出未配置'],
      ['POST', '/api/v1/enterprise-finance/adjustment-journals', '创建关账后报表调整或冲销草稿'],
      ['POST', '/api/v1/enterprise-finance/adjustment-journals/{journal_ref}/approve', '用 expected_version 与 maker-checker 追加批准版本；不做 ERP/GL 过账'],
      ['GET', '/api/v1/agent-trust/offline-contract', '平台或组织管理员读取 Agent Trust、签名收据与数字孪生合同；fixture 公钥不是生产信任根'],
      ['POST', '/api/v1/agent-trust/offline-preview', '离线校验权限衰减、祖先预算、TTL、撤销与签名；不接私钥、不授权运行时执行'],
      ['GET', '/api/v1/provenance-passport/offline-contract', '平台或组织管理员读取精确 Source、Capture Gap 与两类 Connector 证据合同'],
      ['POST', '/api/v1/provenance-passport/offline-preview', '从完整 Source bytes 生成 Passport；不修改 Feed、Cursor、Checkpoint 或运行时 registry'],
      ['GET', '/api/v1/risk-control/offline-contract', '平台或组织管理员读取 B8/B10/B11 组合后的风险建议与人工审阅控制提案合同'],
      ['POST', '/api/v1/risk-control/offline-preview', '生成有租户、Audience、TTL、Epoch 边界的离线建议；不读运行时策略、不发流量、不晋级或执行'],
    ],
  },
  {
    area: 'Notifications',
    endpoints: [
      ['GET', '/api/v1/notification-events', '通知事件列表；项目管理员必须提供 project_id'],
      ['POST', '/api/v1/notification-events/{id}/acknowledge', '确认当前范围内的未关闭通知事件；项目管理员必须提供 project_id'],
      ['POST', '/api/v1/notification-events/{id}/resolve', '解决当前范围内的未解决通知事件；项目管理员必须提供 project_id'],
      ['GET', '/api/v1/notification-deliveries', '通知投递列表；项目管理员必须提供 project_id'],
      ['GET', '/api/v1/notification-webhooks', '通知 Webhook 列表；项目管理员必须提供 project_id'],
      ['POST', '/api/v1/notification-webhooks', '创建项目通知 Webhook；项目管理员必须提交 project_id'],
      ['PUT', '/api/v1/notification-webhooks/{id}', '更新通知 Webhook；项目管理员必须提供 project_id'],
      ['POST', '/api/v1/notification-webhooks/{id}/verify-scope', '确认隔离 Webhook 的组织/项目/目标范围；项目管理员必须提供 project_id'],
    ],
  },
];

const columns = [
  { title: 'Method', dataIndex: 'method', width: 100, render: (v: string) => <Tag color={v === 'GET' ? 'blue' : v === 'POST' ? 'green' : 'orange'}>{v}</Tag> },
  { title: 'Path', dataIndex: 'path', width: 280, render: (v: string) => <Text code>{v}</Text> },
  { title: '说明', dataIndex: 'description' },
];

interface ProjectOption {
  id: number;
  name: string;
  slug?: string;
  current_user_membership_role?: string;
}

interface CatalogModelOption {
  model_name: string;
  request_model?: string;
  aliases?: string | string[];
}

interface ErrorDoc {
  code: string;
  http_status: number;
  summary: string;
  resolution: string;
}

const commonDeveloperErrors = new Set([
  'validation_error.invalid_request',
  'auth_error.invalid_api_key',
  'auth_error.api_key_disabled',
  'auth_error.ip_not_allowed',
  'auth_error.model_not_allowed',
  'limit_policy.rps_exceeded',
  'limit_policy.rpm_exceeded',
  'limit_policy.tpm_exceeded',
  'billing_error.insufficient_balance',
  'server_error.upstream_error',
  'server_error.service_unavailable',
]);

export default function ApiDocsPage() {
  const location = useLocation();
  const user = useAuthStore((state) => state.user);
  const licenseAccess = user?.license_access;
  const [projects, setProjects] = useState<ProjectOption[]>([]);
  const [selectedProjectID, setSelectedProjectID] = useState<number>();
  const [errorDocs, setErrorDocs] = useState<ErrorDoc[]>([]);
  const [catalogModels, setCatalogModels] = useState<CatalogModelOption[]>([]);
  const [errorQuery, setErrorQuery] = useState('');
  const [clientProfiles, setClientProfiles] = useState<any[]>([]);
  const [gatewayEndpoints, setGatewayEndpoints] = useState<any[]>([]);
  const selectedProject = useMemo(
    () => projects.find(project => project.id === selectedProjectID),
    [projects, selectedProjectID],
  );
  const authz = authzContextForUser(
    user,
    selectedProject?.current_user_membership_role,
    selectedProjectID !== undefined,
  );
  const selectedMembershipRole = canonicalProjectMembershipRole(selectedProject?.current_user_membership_role);
  const isMemberQuickstart = authz.organizationRole === 'member' && selectedMembershipRole !== 'project_admin';
  const isProjectAdminReference = authz.organizationRole === 'member' && selectedMembershipRole === 'project_admin';
  const gatewayAvailable = hasLicensedFeature(licenseAccess, 'gateway');
  const modelGovernanceAvailable = hasLicensedFeature(licenseAccess, 'model_governance');
  const agentCapabilitiesAvailable = hasLicensedFeature(licenseAccess, 'agent_capabilities');
  const canLoadErrorDocs = Boolean(authz.organizationRole);
  const visibleEndpointGroups = filterVisibleEndpointGroups(endpointGroups, authz, {
    agentCapabilities: agentCapabilitiesAvailable,
  });
  const modelOptions = useMemo(() => Array.from(new Set(catalogModels
    .map(model => String(model.request_model || model.model_name || '').trim())
    .filter(Boolean))), [catalogModels]);
  const targetErrorCode = useMemo(() => {
    if (!location.hash.startsWith('#error-')) return '';
    return decodeURIComponent(location.hash.slice('#error-'.length));
  }, [location.hash]);
  const memberErrorDocs = useMemo(() => {
    const query = errorQuery.trim().toLowerCase();
    return errorDocs.filter(doc => {
      if (query) {
        return [doc.code, doc.summary, doc.resolution, String(doc.http_status)]
          .some(value => value.toLowerCase().includes(query));
      }
      return commonDeveloperErrors.has(doc.code) || doc.code === targetErrorCode;
    }).slice(0, 20);
  }, [errorDocs, errorQuery, targetErrorCode]);

  useEffect(() => {
    loadAllPages<ProjectOption>((page, size) => api.getProjects(page, size, { status: 'active' }))
      .then(items => {
        setProjects(items);
        setSelectedProjectID(current => current && items.some(item => item.id === current) ? current : items[0]?.id);
      })
      .catch(() => {
        setProjects([]);
        setSelectedProjectID(undefined);
      });
  }, []);

  useEffect(() => {
    if (!isMemberQuickstart || !selectedProjectID || !modelGovernanceAvailable) {
      setCatalogModels([]);
      return;
    }
    let active = true;
    loadAllPages<CatalogModelOption>((page, size) => api.getModelCatalog(page, size, {
      project_id: selectedProjectID,
      routable_only: true,
    })).then(items => {
      if (active) setCatalogModels(items);
    }).catch(() => {
      if (active) setCatalogModels([]);
    });
    return () => { active = false; };
  }, [isMemberQuickstart, modelGovernanceAvailable, selectedProjectID]);

  useEffect(() => {
    if (!canLoadErrorDocs) {
      setErrorDocs([]);
      return;
    }
    api.getErrorDocs()
      .then((data: any) => setErrorDocs(data?.data || []))
      .catch(() => setErrorDocs([]));
  }, [canLoadErrorDocs]);

  useEffect(() => {
    if (!isOrgAdminRole(authz.organizationRole)) {
      setClientProfiles([]);
      setGatewayEndpoints([]);
      return;
    }
    Promise.all([
      api.getWorkloadApplicationContract(),
      api.getGatewayEndpoints().catch(() => ({ data: [] })),
    ]).then(([contract, endpoints]) => {
      setClientProfiles(contract?.client_profiles || []);
      setGatewayEndpoints(endpoints?.data || []);
    }).catch(() => {
      setClientProfiles([]);
      setGatewayEndpoints([]);
    });
  }, [authz.organizationRole]);

  useEffect(() => {
    if (!isMemberQuickstart || !targetErrorCode || memberErrorDocs.length === 0) return;
    const target = document.getElementById(`error-${targetErrorCode}`);
    if (target && 'scrollIntoView' in target) target.scrollIntoView({ block: 'center' });
  }, [isMemberQuickstart, memberErrorDocs, targetErrorCode]);

  const errorColumns = [
    { title: 'Code', dataIndex: 'code', width: 250, render: (value: string) => <Text id={`error-${value}`} code>{value}</Text> },
    { title: 'HTTP', dataIndex: 'http_status', width: 90 },
    { title: '说明', dataIndex: 'summary' },
    { title: '处理建议', dataIndex: 'resolution' },
  ];
  const compatibilityColumns = [
    { title: '客户端', dataIndex: 'display_name', width: 260 },
    { title: '协议路径', dataIndex: 'supported_endpoints', render: (value: string[]) => (value || []).map(path => <Tag key={path}>{path}</Tag>) },
    { title: '运行时', dataIndex: 'runtime_state', width: 150, render: (value: string) => <Tag color={value === 'runtime_supported' ? 'green' : value === 'protocol_compatible' ? 'blue' : 'orange'}>{contractStateLabel(value)}</Tag> },
    { title: '证据等级', dataIndex: 'evidence_level', width: 150, render: (value: string) => <Tag color={value === 'verified' ? 'green' : value === 'protocol_compatible' ? 'blue' : 'red'}>{contractStateLabel(value)}</Tag> },
    { title: 'POC', dataIndex: 'recommended_for_poc', width: 80, render: (value: boolean) => value ? '推荐' : '不推荐' },
    { title: '配置边界', dataIndex: 'configuration_note' },
  ];

  const projectSelector = projects.length > 0 ? (
    <Select
      aria-label="API 文档项目范围"
      value={selectedProjectID}
      optionList={projects.map(project => ({
        label: `${projectDisplayName(project)} · ${canonicalProjectMembershipRole(project.current_user_membership_role) || '无显式项目角色'}`,
        value: project.id,
      }))}
      onChange={value => setSelectedProjectID(Number(value))}
      style={{ width: 280 }}
    />
  ) : undefined;

  if (isMemberQuickstart) {
    const baseUrl = typeof window !== 'undefined' ? window.location.origin : 'https://your-unirouter.example';
    const gatewayRestriction = licenseAccess && !gatewayAvailable
      ? licenseRestrictionMessage(licenseAccess, 'gateway')
      : '当前模型网关不可用。';
    const modelWarning = licenseAccess && !modelGovernanceAvailable
      ? `${licenseRestrictionMessage(licenseAccess, 'model_governance')} 请使用管理员提供的模型或别名。`
      : undefined;

    return (
      <div className="ur-page">
        <PageHeader
          eyebrow="项目空间"
          title="API 快速接入"
          description="只展示当前项目调用所需的信息；供应商、部署、财务和组织管理接口不属于普通项目账号。"
          actions={projectSelector}
        />
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {!selectedProject ? (
            <EmptyState
              title="尚无可访问项目"
              description="先提交访问申请或请项目管理员把你加入项目；获得项目成员关系后才能创建项目 API Key。"
              actions={<Link to="/access">查看访问申请与授权</Link>}
              minHeight={280}
            />
          ) : (
            <>
              <Banner
                type="info"
                description={`当前项目：${projectDisplayName(selectedProject)}。项目 API Key 在“项目工作台”创建；完整密钥只显示一次。`}
                closeIcon={null}
              />
              <ProjectApiQuickstart
                baseUrl={baseUrl}
                modelOptions={modelOptions}
                canInvoke={canInvokeProject(authz)}
                gatewayAvailable={gatewayAvailable}
                unavailableReason={gatewayRestriction}
                modelWarning={modelWarning}
                troubleshootingHref="#developer-errors"
              />
              <Surface title="继续操作" description="接入、凭证、调试和用量都在同一个项目工作台完成。">
                <div className="ur-chip-row">
                  <Link to="/developer#project-access">打开项目凭证</Link>
                  {canInvokeProject(authz) && <Link to="/developer#project-debug">打开请求调试</Link>}
                  <Link to="/developer#project-models">查看已启用模型</Link>
                </div>
              </Surface>
            </>
          )}
          {canLoadErrorDocs && (
            <Surface
              title="常见错误与排查"
              description="网关错误响应中的 doc_url 会打开这里；默认只列常见调用错误，可按错误码、HTTP 状态或说明搜索。"
            >
              <div id="developer-errors" className="ur-dev-docs">
                <Input
                  aria-label="搜索错误码"
                  value={errorQuery}
                  onChange={setErrorQuery}
                  placeholder="搜索错误码、HTTP 状态或处理建议"
                  showClear
                />
                <div className="ur-table-shell">
                  <Table
                    columns={errorColumns}
                    dataSource={memberErrorDocs}
                    pagination={false}
                    rowKey="code"
                    size="small"
                    empty={<EmptyState title="没有匹配的错误说明" description="请核对完整错误码后重试。" minHeight={140} />}
                  />
                </div>
              </div>
            </Surface>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow={isProjectAdminReference ? '项目管理' : '系统与集成'}
        title={isProjectAdminReference ? '项目 API 参考' : 'API 与兼容性'}
        description={isProjectAdminReference
          ? '按当前项目管理员权限展示项目、访问、通知和模型调用合同；部署、供应商与财务接口不会在此展示。'
          : '供管理人员查看其权限范围内的 API、客户端兼容性、证据等级与配置边界；协议兼容不等于已完成真实客户端认证。'}
        actions={projectSelector}
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {isOrgAdminRole(authz.organizationRole) && <Surface title="Agent 客户端兼容矩阵" description="等级从代码合同返回；只有通过独立客户端或端到端证据的能力才标记 verified。">
          <Banner
            type={gatewayEndpoints.some(endpoint => endpoint.status === 'active' && endpoint.observed_status === 'ready') ? 'success' : 'warning'}
            description={gatewayEndpoints.some(endpoint => endpoint.status === 'active' && endpoint.observed_status === 'ready')
              ? '存在已登记、已启用且独立观测为 ready 的附加 Gateway Endpoint；真实客户端认证与模型调用仍需单独验证。'
              : '尚未登记并独立验证附加 Gateway Endpoint；这不等同于当前 UniRouter 服务不可用。当前服务健康看“运行状态”，现场可用性以真实 API 调用为准。'}
            closeIcon={null}
            style={{ marginBottom: 12 }}
          />
          <div className="ur-table-shell">
            <Table columns={compatibilityColumns} dataSource={clientProfiles} rowKey="profile_id" pagination={false} scroll={{ x: 1100 }} />
          </div>
          <Text type="tertiary">OpenAI Agents SDK 的默认 Responses 路径仍为 contract_only / unsupported；第二次联调必须显式使用 Chat Completions 适配路径。</Text>
        </Surface>}
        <Surface title="请求身份与幂等" description="这些字段用途不同，不能互相替代。">
          <div style={{ display: 'grid', gap: 8 }}>
            <Text><Text code>X-Request-ID</Text>：UniRouter 每次请求强制生成的操作 ID，用于串联网关、计费、审计和故障切换。</Text>
            <Text><Text code>X-UniRouter-Client-Request-ID</Text>：调用方自己的关联值，只用于查找，不作为全局财务主键。</Text>
            <Text><Text code>X-UniRouter-Limit-Policy-Version</Text>：本次网关准入实际使用的共享限额策略集合指纹，可与请求日志中的策略链一起排障。</Text>
            <Text><Text code>Idempotency-Key</Text>：Chat / Embeddings 使用 24 小时元数据去重；图像任务使用项目、API Key 与请求内容绑定的持久身份。两者都不会在准入响应中重放模型输出。</Text>
          </div>
        </Surface>
        {agentCapabilitiesAvailable && (isOrgAdminRole(authz.organizationRole) || isProjectAdminReference) && <Surface title="Agent 能力边界" description="UniRouter 只授予模型网关调用权，不授予 Tool、IAM、数据库或导出权限。">
          <div style={{ display: 'grid', gap: 8 }}>
            <Text>Supervisor 使用根 API Key 显式登记工作流；每个子 Agent 必须由父 Agent 调用委派接口登记，系统不会从普通流量中猜测子 Agent。</Text>
            <Text>能力令牌最长 <Text code>5 分钟</Text>，通常在第 3 分钟附近带抖动刷新；工作流时长、深度、扇出和预算独立计算。</Text>
            <Text>委派合同中的模型、接口、成本、Token、RPM、RPS 与并发上限只能保持或缩小；共享预算的运行时扣减由后续分布式策略批次接入。根权限变化最多 15 秒被所有实例重新核对；Redis 状态不可用时能力请求拒绝准入。</Text>
          </div>
        </Surface>}
        {visibleEndpointGroups.map(group => (
          <details key={group.area} className="ur-surface" open={group.area === 'Gateway'}>
            <summary style={{ cursor: 'pointer', fontWeight: 600, padding: '16px 20px' }}>
              {apiAreaLabels[group.area] || group.area} · {group.endpoints.length} 个接口
            </summary>
            <div className="ur-surface-body">
              {group.area === 'Gateway' && <Text type="tertiary">模型网关使用项目 API Key 或 Agent 能力令牌鉴权；控制台登录态不适用于这些协议接口。</Text>}
            <div className="ur-table-shell">
              <Table
                columns={columns}
                dataSource={group.endpoints.map(([method, path, description]) => ({
                  key: `${method}:${path}`,
                  method,
                  path,
                  description,
                }))}
                pagination={false}
                rowKey="key"
                size="small"
              />
            </div>
            </div>
          </details>
        ))}
        {canLoadErrorDocs && <Surface title="错误说明" description="模型网关错误响应中的 doc_url 会指向本表锚点。">
          <div className="ur-table-shell">
            <Table
              columns={errorColumns}
              dataSource={errorDocs}
              pagination={false}
              rowKey="code"
              size="small"
            />
          </div>
        </Surface>}
      </div>
    </div>
  );
}
