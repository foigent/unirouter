import { useEffect, useState } from 'react';
import { Banner, Typography, Table, Tag } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { PageHeader, Surface } from '../ui';
import { isBillingRole, isOrgAdminRole, isOrgViewerRole, isPlatformAdminRole, isProjectAdminRole, isProjectConsoleRole, normalizeRole } from '../navigation';
import { useAuthStore } from '../store/auth';

const { Text } = Typography;

const endpointGroups = [
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
      ['POST', '/v1/embeddings', '兼容 OpenAI 协议的向量接口'],
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
    area: '开发者工作台',
    endpoints: [
      ['GET', '/api/v1/projects', '当前账号可访问项目'],
      ['GET', '/api/v1/projects/{id}', '项目详情'],
      ['PUT', '/api/v1/projects/{id}', '更新项目'],
      ['GET', '/api/v1/projects/{id}/model-sets', '项目模型集合绑定'],
      ['PUT', '/api/v1/projects/{id}/model-sets', '更新项目模型集合绑定'],
      ['GET', '/api/v1/projects/{id}/members', '查看项目成员与同组织可加入用户'],
      ['PUT', '/api/v1/projects/{id}/members/{user_id}', '加入或同步项目成员；成员角色必须与账号项目角色一致'],
      ['DELETE', '/api/v1/projects/{id}/members/{user_id}', '移除项目成员；启用账号必须保留至少一个项目'],
      ['GET', '/api/v1/projects/{id}/budget', '查看项目预算（账务管理员仅有预算权限）'],
      ['PUT', '/api/v1/projects/{id}/budget', '仅更新项目预算金额与周期并原子记录审计'],
      ['GET', '/api/v1/keys?project_id={id}', '项目访问凭证列表'],
      ['GET', '/api/v1/keys/{id}', '访问凭证详情'],
      ['POST', '/api/v1/keys', '创建项目访问凭证'],
      ['PUT', '/api/v1/keys/{id}', '更新访问凭证'],
      ['POST', '/api/v1/keys/{id}/disable', '停用访问凭证；developer 仅限本人拥有且仍有项目成员关系的 active Key'],
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
    area: 'Admin',
    endpoints: [
      ['GET', '/api/v1/dashboard/stats', 'Dashboard 聚合数据'],
      ['GET', '/api/v1/channels', '上游通道列表'],
      ['POST', '/api/v1/channels', '创建上游通道'],
      ['GET', '/api/v1/channels/{id}', '上游通道详情'],
      ['PUT', '/api/v1/channels/{id}', '更新上游通道'],
      ['DELETE', '/api/v1/channels/{id}', '删除上游通道'],
      ['POST', '/api/v1/channels/{id}/test', '测试上游通道'],
      ['GET', '/api/v1/provider-packs', '列出离线 fixture-verified Provider Pack、能力和精确摘要'],
      ['GET', '/api/v1/routing/preview', '只读预览模型的路由候选、排除原因与快照状态'],
      ['GET', '/api/v1/runtime-changes', '读取通道准入、优先级和权重的确定性发布队列'],
      ['POST', '/api/v1/runtime-changes', '创建绑定通道 epoch、Provider Pack 和模拟证据的发布草稿'],
      ['GET', '/api/v1/runtime-changes/slot', '读取一个模型的稳定与灰度发布槽'],
      ['GET', '/api/v1/runtime-changes/{ref}', '读取发布候选、Hash、Pack Pin、模拟报告和生命周期'],
      ['POST', '/api/v1/runtime-changes/{ref}/validate', '重建同钟快照并验证候选；breaking 直接阻断'],
      ['POST', '/api/v1/runtime-changes/{ref}/shadow', '进入零流量影子阶段'],
      ['POST', '/api/v1/runtime-changes/{ref}/canary', '按服务端请求标识启动最高 50% 的确定性灰度'],
      ['POST', '/api/v1/runtime-changes/{ref}/promote', '将当前灰度原子提升为稳定发布'],
      ['POST', '/api/v1/runtime-changes/{ref}/rollback', '移除灰度或恢复父稳定发布；不复活紧急禁用通道'],
      ['GET', '/api/v1/channel-keys', '上游凭证列表'],
      ['POST', '/api/v1/channel-keys', '创建上游凭证'],
      ['PUT', '/api/v1/channel-keys/{id}', '更新上游凭证'],
      ['DELETE', '/api/v1/channel-keys/{id}', '删除上游凭证'],
      ['GET', '/api/v1/keys', '访问凭证列表'],
      ['GET', '/api/v1/organizations', '当前角色可见的组织选项；平台查看全部，组织角色仅查看所属组织'],
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
      ['GET', '/api/v1/model-registry', '本组织模型注册表；平台管理员可显式选择组织'],
      ['POST', '/api/v1/model-registry', '在本组织创建模型注册记录；跨组织请求返回 403'],
      ['PUT', '/api/v1/model-registry/{id}', '更新本组织记录；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['GET', '/api/v1/model-catalog', '组织模型目录；project_id 决定组织，项目角色必须提供 project_id'],
      ['GET', '/api/v1/model-aliases', '本组织模型别名；平台管理员可显式选择组织'],
      ['POST', '/api/v1/model-aliases', '在本组织创建模型别名；跨组织请求返回 403'],
      ['PUT', '/api/v1/model-aliases/{id}', '更新本组织别名；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['GET', '/api/v1/model-sets', '本组织模型集合；项目管理员只读候选集合，平台管理员可显式选择组织'],
      ['POST', '/api/v1/model-sets', '原子创建本组织模型集合及其全部初始条目'],
      ['GET', '/api/v1/model-sets/{id}/items', '读取本组织模型集合条目；越权组织参数返回 403，越权或不存在的 ID 返回 404'],
      ['PUT', '/api/v1/model-sets/{id}/items', '原子替换本组织模型集合条目；不会暴露部分结果'],
      ['GET', '/api/v1/org-model-prices', '查看本组织计费倍率；平台可显式选择组织'],
      ['POST', '/api/v1/org-model-prices', '创建或覆盖组织计费倍率（仅平台管理员）'],
      ['PUT', '/api/v1/org-model-prices/{id}', '更新组织计费倍率（仅平台管理员）'],
      ['DELETE', '/api/v1/org-model-prices/{id}', '删除组织计费倍率（仅平台管理员）'],
      ['GET', '/api/v1/reconciliation', '估算/实际用量及供应商 Prompt Cache 分项对账'],
      ['POST', '/api/v1/provider-cost/reconciliations', '平台管理员处理未决供应商成本；请求不接受金额，collect 仅重放权威用量与冻结客户价'],
      ['GET', '/api/v1/provider-cost/pending', '平台管理员按明确组织查看最多 100 条未决供应商成本元数据'],
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
      ['GET', '/api/v1/enterprise-finance/rate-cards', '读取各经济层最新企业 Rate Card 版本与精确组件'],
      ['POST', '/api/v1/enterprise-finance/rate-cards', '创建企业 Rate Card 草稿；不改变现有请求结算'],
      ['POST', '/api/v1/enterprise-finance/rate-cards/{rate_card_ref}/approve', '用 expected_version 与 maker-checker 追加批准版本'],
      ['POST', '/api/v1/enterprise-finance/rate-cards/{rate_card_ref}/activate', '在有效期和 meter 无冲突时激活版本；仍不接入请求路径'],
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

const organizationModelGovernanceWrites = new Set([
  'POST /api/v1/model-registry',
  'PUT /api/v1/model-registry/{id}',
  'POST /api/v1/model-aliases',
  'PUT /api/v1/model-aliases/{id}',
  'POST /api/v1/model-sets',
  'PUT /api/v1/model-sets/{id}/items',
]);

function endpointVisibleForRole(area: string, method: string, path: string, role?: string) {
  if (area === 'Auth' || area === 'Platform' || area === 'Gateway') return true;
  if (area === 'Async Media') {
    if (path.startsWith('/v1/')) return true;
    if (isBillingRole(role) || isOrgViewerRole(role)) return false;
    if (normalizeRole(role) === 'viewer') return method === 'GET';
    return isPlatformAdminRole(role) || isOrgAdminRole(role) || isProjectConsoleRole(role);
  }
  if (isPlatformAdminRole(role)) return true;

  if (area === '开发者工作台') {
    const isMemberPath = path.includes('/members');
    const isBudgetPath = path.endsWith('/budget');
    if (isBillingRole(role)) {
      return (method === 'GET' && path === '/api/v1/projects') || isBudgetPath;
    }
    if (isMemberPath || isBudgetPath) return isOrgAdminRole(role) || isProjectAdminRole(role);
    if (isOrgAdminRole(role) || isProjectAdminRole(role)) return true;
    if (normalizeRole(role) === 'developer') {
      return method === 'GET' || (method === 'POST' && (
        path === '/api/v1/keys' ||
        path === '/api/v1/keys/{id}/disable' ||
        path === '/v1/chat/completions'
      ));
    }
    if (normalizeRole(role) === 'viewer') return method === 'GET';
    return false;
  }
  if (area === '产品与运行状态') {
    if (path.endsWith('/error-docs')) return isPlatformAdminRole(role) || isOrgAdminRole(role) || isBillingRole(role) || isProjectConsoleRole(role);
    if (path.endsWith('/usage/analytics')) return isOrgAdminRole(role) || isBillingRole(role) || isProjectConsoleRole(role);
    if (path.endsWith('/internal-status')) return isOrgAdminRole(role);
    if (path.endsWith('/runtime/status')) return isOrgAdminRole(role);
    return false;
  }
  if (area === 'Notifications') {
    if (isBillingRole(role)) return !path.includes('/notification-webhooks');
    return isOrgAdminRole(role) || isProjectAdminRole(role);
  }
  if (area === 'Model Governance') {
    if (isOrgAdminRole(role)) {
      if (method === 'GET') {
        return !path.includes('/provider-cost/') && !path.endsWith('/reconciliation');
      }
      return organizationModelGovernanceWrites.has(`${method} ${path}`);
    }
    if (isBillingRole(role)) {
      return method === 'GET' && (
        path.includes('/model-prices') || path.includes('/model-names') ||
        path.includes('/org-model-prices') || path.endsWith('/reconciliation') ||
        path.endsWith('/provider-cogs/offline-contract')
      ) || (method === 'POST' && path.endsWith('/provider-cogs/offline-preview'));
    }
    if (isProjectAdminRole(role)) return method === 'GET' && path === '/api/v1/model-sets';
    return false;
  }
  if (area === 'Admin') {
    if (isOrgViewerRole(role)) return method === 'GET' && path.endsWith('/dashboard/stats');
    if (isOrgAdminRole(role)) {
      return [
        '/dashboard/stats', '/keys', '/limit-policies', '/guardrail-policies',
        '/projects', '/logs', '/users', '/audit-logs',
      ].some(fragment => path.includes(fragment)) && !path.includes('/wallet');
    }
    if (isBillingRole(role)) {
      return ['/dashboard/stats', '/logs', '/wallet', '/grants'].some(fragment => path.includes(fragment));
    }
    return false;
  }
  return !isOrgViewerRole(role);
}

export default function ApiDocsPage() {
  const role = useAuthStore((state) => state.user?.role);
  const [errorDocs, setErrorDocs] = useState<any[]>([]);
  const [clientProfiles, setClientProfiles] = useState<any[]>([]);
  const [gatewayEndpoints, setGatewayEndpoints] = useState<any[]>([]);
  const canLoadErrorDocs = isPlatformAdminRole(role) || isOrgAdminRole(role) || isBillingRole(role) || isProjectConsoleRole(role);
  const visibleEndpointGroups = endpointGroups
    .map(group => ({
      ...group,
      endpoints: group.endpoints.filter(([method, path]) => endpointVisibleForRole(group.area, method, path, role)),
    }))
    .filter(group => group.endpoints.length > 0);

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
    if (!isOrgAdminRole(role)) {
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
  }, [role]);

  const errorColumns = [
    { title: 'Code', dataIndex: 'code', width: 250, render: (value: string) => <Text id={`error-${value}`} code>{value}</Text> },
    { title: 'HTTP', dataIndex: 'http_status', width: 90 },
    { title: '说明', dataIndex: 'summary' },
    { title: '处理建议', dataIndex: 'resolution' },
  ];
  const compatibilityColumns = [
    { title: '客户端', dataIndex: 'display_name', width: 260 },
    { title: '协议路径', dataIndex: 'supported_endpoints', render: (value: string[]) => (value || []).map(path => <Tag key={path}>{path}</Tag>) },
    { title: '运行时', dataIndex: 'runtime_state', width: 150, render: (value: string) => <Tag color={value === 'runtime_supported' ? 'green' : value === 'protocol_compatible' ? 'blue' : 'orange'}>{value}</Tag> },
    { title: '证据等级', dataIndex: 'evidence_level', width: 150, render: (value: string) => <Tag color={value === 'verified' ? 'green' : value === 'protocol_compatible' ? 'blue' : 'red'}>{value}</Tag> },
    { title: 'POC', dataIndex: 'recommended_for_poc', width: 80, render: (value: boolean) => value ? '推荐' : '不推荐' },
    { title: '配置边界', dataIndex: 'configuration_note' },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="系统与集成"
        title="API 与兼容性"
        description="同时查看真实 Gateway API、Client Profile、证据等级与配置边界；协议兼容不等于已完成真实客户端认证。"
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {isOrgAdminRole(role) && <Surface title="Agent 客户端兼容矩阵" description="等级从代码合同返回；只有通过独立客户端或端到端证据的能力才标记 verified。">
          <Banner
            type={gatewayEndpoints.some(endpoint => endpoint.status === 'active' && endpoint.observed_status === 'ready') ? 'success' : 'warning'}
            description={gatewayEndpoints.some(endpoint => endpoint.status === 'active' && endpoint.observed_status === 'ready')
              ? '存在已启用且独立验证通过的 Gateway Endpoint。'
              : '当前没有“已启用且独立验证通过”的 Gateway Endpoint；配置可以生成，但不能宣称现场可用。'}
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
        <Surface title="Agent 能力边界" description="UniRouter 只授予模型网关调用权，不授予 Tool、IAM、数据库或导出权限。">
          <div style={{ display: 'grid', gap: 8 }}>
            <Text>Supervisor 使用根 API Key 显式登记工作流；每个子 Agent 必须由父 Agent 调用委派接口登记，系统不会从普通流量中猜测子 Agent。</Text>
            <Text>能力令牌最长 <Text code>5 分钟</Text>，通常在第 3 分钟附近带抖动刷新；工作流时长、深度、扇出和预算独立计算。</Text>
            <Text>委派合同中的模型、接口、成本、Token、RPM、RPS 与并发上限只能保持或缩小；共享预算的运行时扣减由后续分布式策略批次接入。根权限变化最多 15 秒被所有实例重新核对；Redis 状态不可用时能力请求拒绝准入。</Text>
          </div>
        </Surface>
        {visibleEndpointGroups.map(group => (
          <Surface
            key={group.area}
            title={group.area}
            description={group.area === 'Gateway'
              ? 'Gateway 使用项目 API Key 或 Agent 能力令牌鉴权；控制台 JWT 不适用于这些协议接口。'
              : undefined}
          >
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
          </Surface>
        ))}
        {canLoadErrorDocs && <Surface title="Error Docs" description="Gateway 错误响应中的 doc_url 指向本表锚点。">
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
