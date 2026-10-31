import '../i18n/developer';
import { translate as t, useI18n, translateLabel } from '../i18n';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Form, Input, Modal, Select, SideSheet, Space, Table, TextArea, Toast, Tooltip, Typography } from '@douyinfe/semi-ui';
import {
  IconAlertTriangle,
  IconBell,
  IconBolt,
  IconCoinMoney,
  IconCopy,
  IconDelete,
  IconFile,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconServer,
} from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import {
  authzContextForUser,
  canCreateProjectKey,
  canDisableProjectKey,
  canInvokeProject,
  canonicalProjectMembershipRole,
  hasLicensedFeature,
  isOrgAdminRole,
  isProjectAdminMembership,
  licenseRestrictionMessage,
  organizationRoleForUser,
} from '../navigation';
import { EmptyState, MetricTile, MoneyText, PageHeader, parseMicros, StatusTag, Surface } from '../ui';
import { roleLabel as formatRoleLabel } from '../utils/displayLabels';
import { loadAllPages } from '../utils/loadAllPages';
import { normalizeModelList, parseModelList } from '../utils/modelList';
import { keyQuotaText } from '../utils/quota';
import { projectDisplayName, projectOption } from '../utils/projectDisplay';
import ProjectApiQuickstart from '../features/apiDocs/ProjectApiQuickstart';

const { Text } = Typography;

interface Project {
  id: number;
  org_id: number;
  name: string;
  slug: string;
  status: string;
  budget_cents?: number;
  budget_period?: string;
  key_count?: number;
  current_user_membership_role?: string;
}

interface ApiKey {
  id: number;
  project_id: number;
  owner_user_id?: number | null;
  name: string;
  key_prefix: string;
  status: string;
  allowed_ips: string;
  allowed_models: string;
  rpm_limit: number;
  rps_limit?: number;
  qps_limit: number;
  quota?: {
    rpm_limit?: number | null;
    rps_limit?: number | null;
    tpm_limit?: number | null;
    budget_limit_cents?: number | null;
  };
  created_at: string;
}

interface CatalogModel {
  id: number;
  model_name: string;
  request_model?: string;
  display_name?: string;
  provider_family?: string;
  lifecycle: string;
  context_length?: number;
  output_limit?: number;
  capability_tags?: string | string[];
  data_residency?: string;
  pricing_model_name?: string;
  migration_recommendation?: string;
  aliases?: string | string[];
}

interface CallLog {
  id: number;
  project_id: number;
  request_id: string;
  model: string;
  requested_model?: string;
  provider: string;
  channel: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_micros: string;
  status_code: number;
  latency_ms: number;
  ttft_ms?: number;
  upstream_status_code?: number;
  upstream_error?: string;
  usage_source?: string;
  is_stream?: boolean;
  created_at: string;
}

interface UsageAnalyticsRow {
  key: string;
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_micros: string;
  avg_latency_ms: number;
  estimated_ratio: number;
}

interface UsageAnalyticsSummary {
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  total_tokens: number;
  cost_micros: string;
  avg_latency_ms: number;
  estimated_ratio: number;
}

function splitCSV(value?: string | string[] | null) {
  return parseModelList(value);
}

function compactNumber(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return String(value);
}

function fallbackCopyText(text: string) {
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', 'true');
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  document.body.appendChild(textarea);
  textarea.select();
  try {
    return document.execCommand('copy');
  } finally {
    document.body.removeChild(textarea);
  }
}

export default function DeveloperConsolePage() {
  const { locale } = useI18n();
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const organizationRole = organizationRoleForUser(user);
  const currentUserID = useAuthStore((s) => s.user?.id);
  const licenseAccess = useAuthStore((s) => s.user?.license_access);
  const gatewayLicensed = hasLicensedFeature(licenseAccess, 'gateway');
  const modelGovernanceLicensed = hasLicensedFeature(licenseAccess, 'model_governance');
  const notificationsLicensed = hasLicensedFeature(licenseAccess, 'notifications');
  const gatewayRestriction = licenseAccess && !gatewayLicensed
    ? licenseRestrictionMessage(licenseAccess, 'gateway')
    : '';
  const modelGovernanceRestriction = licenseAccess && !modelGovernanceLicensed
    ? licenseRestrictionMessage(licenseAccess, 'model_governance')
    : '';
  const notificationsRestriction = licenseAccess && !notificationsLicensed
    ? licenseRestrictionMessage(licenseAccess, 'notifications')
    : '';
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProjectId, setSelectedProjectId] = useState<number>();
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [models, setModels] = useState<CatalogModel[]>([]);
  const [keyPage, setKeyPage] = useState(1);
  const [keyPageSize, setKeyPageSize] = useState(20);
  const [modelPage, setModelPage] = useState(1);
  const [modelPageSize, setModelPageSize] = useState(20);
  const [logs, setLogs] = useState<CallLog[]>([]);
  const [usageSummary, setUsageSummary] = useState<UsageAnalyticsSummary | null>(null);
  const [usageByModel, setUsageByModel] = useState<UsageAnalyticsRow[]>([]);
  const [initialLoading, setInitialLoading] = useState(true);
  const [projectsLoadError, setProjectsLoadError] = useState('');
  const [dataLoading, setDataLoading] = useState(true);
  const [projectDataError, setProjectDataError] = useState('');
  const [usageLoadError, setUsageLoadError] = useState<{ detail: string } | null>(null);
  const [showKeySheet, setShowKeySheet] = useState(false);
  const [formApi, setFormApi] = useState<any>(null);
  const [createdKey, setCreatedKey] = useState<{ key: string; key_prefix: string; name: string } | null>(null);
  const [lookupRequestId, setLookupRequestId] = useState('');
  const [lookupLoading, setLookupLoading] = useState(false);
  const [lookupResult, setLookupResult] = useState<CallLog | null>(null);
  const [playgroundKey, setPlaygroundKey] = useState('');
  const [playgroundModel, setPlaygroundModel] = useState('');
  const [playgroundPrompt, setPlaygroundPrompt] = useState(t("请用一句话返回当前项目的健康检查结果。"));
  const [playgroundMode, setPlaygroundMode] = useState('non_stream');
  const [playgroundMaxTokens, setPlaygroundMaxTokens] = useState('');
  const [playgroundThinking, setPlaygroundThinking] = useState('');
  const [playgroundReasoningEffort, setPlaygroundReasoningEffort] = useState('');
  const [playgroundScenario, setPlaygroundScenario] = useState('text');
  const [playgroundLoading, setPlaygroundLoading] = useState(false);
  const [playgroundResult, setPlaygroundResult] = useState<{
    status: number;
    content: string;
    request_id?: string;
    cost_micros?: string;
    total_tokens?: number;
    latency_ms?: number;
    headers_ms?: number;
    usage?: unknown;
    finish_reason?: string;
    usage_source?: string;
    billing_status?: string;
    doc_url?: string;
    error?: string;
  } | null>(null);
  const projectDataRequestVersion = useRef(0);

  const selectedProject = projects.find(project => project.id === selectedProjectId);
  const selectedProjectMembershipRole = canonicalProjectMembershipRole(selectedProject?.current_user_membership_role);
  const selectedProjectAuthz = authzContextForUser(user, selectedProjectMembershipRole, true);
  const projectRoleLabel = formatRoleLabel(selectedProjectMembershipRole || organizationRole, t("未分配项目角色"));
  const mayCreateKey = canCreateProjectKey(selectedProjectAuthz);
  const mayDisableKey = canDisableProjectKey(selectedProjectAuthz);
  const developerOwnKeyOnly = selectedProjectMembershipRole === 'project_user';
  const mayRunPlayground = canInvokeProject(selectedProjectAuthz);
  const projectReadOnly = selectedProjectMembershipRole === 'project_viewer';
  const keySurfaceDescription = !gatewayLicensed
    ? t("{v0} 仍可管理凭证，但模型调用会被后端拒绝。", { v0: String(gatewayRestriction) })
    : mayCreateKey
      ? t("创建后只显示一次完整凭证；列表仅显示前缀。")
      : t("只读查看当前项目的凭证前缀和使用范围；创建或停用请联系项目管理员。");
  const emptyKeyDescription = mayCreateKey
    ? t("创建项目凭证后即可从内部应用调用网关。")
    : t("当前项目暂无可见凭证；如需调用权限，请联系项目管理员。");
  const workbenchDescription = projectReadOnly
    ? t("查看当前项目的个人用量、已启用模型和最近调用，并按 Request ID 排查请求。")
    : isProjectAdminMembership(selectedProjectMembershipRole)
      ? t("创建和停用项目访问凭证、查看已启用模型、调试调用并排查当前项目请求。")
      : t("创建项目访问凭证、停用本人拥有的凭证、查看已启用模型、调试调用并排查当前项目请求。");
  const noProjectDescription = t("请联系组织管理员或项目管理员把你的账号加入项目后，再使用项目工作台。");

  const modelOptions = useMemo(() => {
    return Array.from(new Set(models
      .map(model => String(model.request_model || model.model_name || '').trim())
      .filter(Boolean))).sort();
  }, [models, locale]);

  useEffect(() => {
    // Choose an initial catalog model once; a later refresh must not silently
    // replace a selected model that is no longer available.
    if (modelGovernanceLicensed && !playgroundModel && modelOptions.length) {
      setPlaygroundModel(modelOptions[0]);
    }
  }, [modelGovernanceLicensed, modelOptions, playgroundModel]);

  const handlePlaygroundModelChange = (value: unknown) => {
    const nextModel = String(value || '');
    if (nextModel !== playgroundModel) {
      setPlaygroundThinking('');
      setPlaygroundReasoningEffort('');
      setPlaygroundScenario('text');
    }
    setPlaygroundModel(nextModel);
  };
  const effectivePlaygroundModel = modelGovernanceLicensed
    ? (modelOptions.includes(playgroundModel) ? playgroundModel : '')
    : playgroundModel.trim();
  const maxTokensText = playgroundMaxTokens.trim();
  const maxTokensInvalid = Boolean(maxTokensText) &&
    (!/^[1-9][0-9]*$/.test(maxTokensText) || !Number.isSafeInteger(Number(maxTokensText)));
  const sampleKey = createdKey?.key || '<YOUR_PROJECT_API_KEY>';
  const baseUrl = typeof window !== 'undefined' ? window.location.origin : 'https://your-unirouter.example';

  const usageSnapshot = useMemo(() => {
    if (usageSummary) {
      return {
        requestCount: usageSummary.total_requests || 0,
        totalTokens: usageSummary.total_tokens || 0,
        totalCostMicros: usageSummary.cost_micros || '0',
        errorRate: usageSummary.total_requests ? (usageSummary.failed_requests || 0) / usageSummary.total_requests * 100 : 0,
        avgLatency: usageSummary.avg_latency_ms || 0,
        sourceLabel: t("我的 Key 聚合"),
      };
    }
    const requestCount = logs.length;
    const totalTokens = logs.reduce((sum, log) => sum + (log.total_tokens || 0), 0);
    const totalCostMicros = logs.reduce((sum, log) => sum + parseMicros(log.cost_micros || '0'), 0n);
    const failed = logs.filter(log => log.status_code >= 400 || log.status_code === 0).length;
    const avgLatency = logs.length
      ? logs.reduce((sum, log) => sum + (log.latency_ms || 0), 0) / logs.length
      : 0;
    return {
      requestCount,
      totalTokens,
      totalCostMicros,
      errorRate: logs.length ? failed / logs.length * 100 : 0,
      avgLatency,
      sourceLabel: t("最近 20 条合计"),
    };
  }, [logs, usageSummary, locale]);

  const clearProjectData = useCallback(() => {
    setKeys([]);
    setModels([]);
    setLogs([]);
    setUsageSummary(null);
    setUsageByModel([]);
    setKeyPage(1);
    setModelPage(1);
  }, []);

  const loadProjects = useCallback(async () => {
    setInitialLoading(true);
    setProjectsLoadError('');
    try {
      const items = await loadAllPages<Project>((page, pageSize) => (
        api.getProjects(page, pageSize, { status: 'active' })
      ));
      const workspaceProjects = isOrgAdminRole(organizationRole)
        ? items
        : items.filter(project => canonicalProjectMembershipRole(project.current_user_membership_role));
      setProjects(workspaceProjects);
      setSelectedProjectId(current => {
        if (current && workspaceProjects.some(project => project.id === current)) return current;
        return workspaceProjects[0]?.id;
      });
    } catch (err: any) {
      projectDataRequestVersion.current += 1;
      clearProjectData();
      setProjects([]);
      setSelectedProjectId(undefined);
      setProjectsLoadError('无法读取当前账号可访问的项目。请检查网络或服务状态后重试。');
      Toast.error(t("项目加载失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    } finally {
      setInitialLoading(false);
    }
  }, [clearProjectData, organizationRole]);

  const loadProjectData = useCallback(async () => {
    if (!selectedProjectId) {
      setDataLoading(false);
      return;
    }
    const projectId = selectedProjectId;
    const requestVersion = ++projectDataRequestVersion.current;
    clearProjectData();
    setProjectDataError('');
    setUsageLoadError(null);
    setDataLoading(true);
    try {
      let usageFailure: { detail: string } | null = null;
      const [keyItems, modelItems, logData, usageData] = await Promise.all([
        loadAllPages<ApiKey>((page, pageSize) => api.getKeys(page, pageSize, { project_id: projectId })),
        modelGovernanceLicensed
          ? loadAllPages<CatalogModel>((page, pageSize) => api.getModelCatalog(page, pageSize, {
            project_id: projectId,
            routable_only: true,
          }))
          : Promise.resolve([] as CatalogModel[]),
        api.getLogs(1, 20, { project_id: projectId }),
        api.getUsageAnalytics({ project_id: projectId, scope: 'my', group_by: 'alias' }).catch((err: any) => {
          usageFailure = { detail: err?.message || '' };
          return null;
        }),
      ]);
      if (projectDataRequestVersion.current !== requestVersion) return;
      setKeys(keyItems);
      setModels(modelItems);
      setLogs((logData?.data || []) as CallLog[]);
      setUsageSummary((usageData?.summary || null) as UsageAnalyticsSummary | null);
      setUsageByModel((usageData?.data || []) as UsageAnalyticsRow[]);
      setUsageLoadError(usageFailure);
    } catch (err: any) {
      if (projectDataRequestVersion.current !== requestVersion) return;
      setProjectDataError('无法读取当前项目的凭证、模型或调用数据。请检查网络或服务状态后重试。');
      Toast.error(t("项目数据加载失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    } finally {
      if (projectDataRequestVersion.current === requestVersion) setDataLoading(false);
    }
  }, [clearProjectData, modelGovernanceLicensed, selectedProjectId]);

  const handleProjectChange = (value: unknown) => {
    const nextProjectId = Number(value);
    if (!Number.isFinite(nextProjectId) || nextProjectId === selectedProjectId) return;
    projectDataRequestVersion.current += 1;
    clearProjectData();
    setProjectDataError('');
    setUsageLoadError(null);
    setLookupRequestId('');
    setLookupResult(null);
    setPlaygroundKey('');
    setPlaygroundModel('');
    setPlaygroundThinking('');
    setPlaygroundReasoningEffort('');
    setPlaygroundScenario('text');
    setPlaygroundMaxTokens('');
    setPlaygroundResult(null);
    setCreatedKey(null);
    setDataLoading(true);
    setSelectedProjectId(nextProjectId);
  };

  useEffect(() => { loadProjects(); }, [loadProjects]);
  useEffect(() => { loadProjectData(); }, [loadProjectData]);
  useEffect(() => {
    if (!playgroundModel && modelOptions[0]) setPlaygroundModel(modelOptions[0]);
  }, [modelOptions, playgroundModel]);

  const openCreateKey = () => {
    setShowKeySheet(true);
    setTimeout(() => formApi?.reset(), 0);
  };

  const closeCreateKey = () => {
    setShowKeySheet(false);
    formApi?.reset();
  };

  const handleCreateKey = async (values: Record<string, any>) => {
    if (!selectedProject) return;
    const allowedModels = normalizeModelList(values.allowed_models);
    try {
      const result = await api.createKey({
        org_id: selectedProject.org_id,
        project_id: selectedProject.id,
        name: values.name,
        allowed_ips: values.allowed_ips || '',
        allowed_models: allowedModels,
        rpm_limit: Number(values.rpm_limit || 60),
        rps_limit: Number(values.rps_limit || 10),
      });
      setCreatedKey({
        key: result.key,
        key_prefix: result.key_prefix,
        name: values.name,
      });
      setPlaygroundKey(result.key);
      Toast.success(t("项目 Key 已创建"));
      closeCreateKey();
      loadProjectData();
    } catch (err: any) {
      Toast.error(t("创建失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    }
  };

  const handleDisableKey = (record: ApiKey) => {
    Modal.confirm({
      okText: t("确认操作"),
      cancelText: t("取消"),
      title: t("确认停用项目 Key"),
      content: t("停用 {v0} 后，使用该 Key 的项目调用会被拒绝。", { v0: String(record.name) }),
      onOk: async () => {
        try {
          await api.disableKey(record.id);
          Toast.success(t("项目 Key 已停用"));
          loadProjectData();
        } catch (err: any) {
          Toast.error(t("停用失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
        }
      },
    });
  };

  const copyCreatedKey = async () => {
    if (!createdKey?.key) return;
    try {
      if (navigator.clipboard?.writeText && window.isSecureContext) {
        await navigator.clipboard.writeText(createdKey.key);
      } else if (!fallbackCopyText(createdKey.key)) {
        throw new Error('fallback copy failed');
      }
      Toast.success(t("已复制完整 Key"));
    } catch {
      Toast.error(t("复制失败，请手动复制"));
    }
  };

  const handleLookup = async () => {
    const requestID = lookupRequestId.trim();
    if (!selectedProjectId || !requestID) return;
    setLookupLoading(true);
    setLookupResult(null);
    try {
      const list = await api.getLogs(1, 1, { project_id: selectedProjectId, request_id: requestID });
      const first = list?.data?.[0] as CallLog | undefined;
      if (!first) {
        Toast.warning(t("未找到该 Request ID"));
        return;
      }
      const detail = await api.getLogDetail(first.id);
      setLookupResult(detail as CallLog);
    } catch (err: any) {
      Toast.error(t("查询失败{v0}", { v0: String(err?.message ? `：${err.message}` : '') }));
    } finally {
      setLookupLoading(false);
    }
  };

  const handlePlaygroundRun = async () => {
    const key = playgroundKey.trim();
    const prompt = playgroundPrompt.trim();
    if (!key || !prompt || !effectivePlaygroundModel || maxTokensInvalid) {
      Toast.warning(t("请填写项目访问凭证、模型和提示词"));
      return;
    }
    setPlaygroundLoading(true);
    setPlaygroundResult(null);
    const started = performance.now();
    let responseStatus = 0;
    let headersMS: number | undefined;
    const elapsed = () => Math.max(0, Math.round(performance.now() - started));
    try {
      const stream = playgroundMode === 'stream';
      const functionTool = {
        type: 'function',
        function: {
          name: 'unirouter_ui_echo',
          description: 'Return a supplied test value through a function call.',
          parameters: {
            type: 'object',
            properties: { value: { type: 'string' } },
            required: ['value'],
            additionalProperties: false,
          },
        },
      };
      const toolChoice = playgroundScenario === 'function_auto' ? 'auto'
        : playgroundScenario === 'function_required' ? 'required'
          : playgroundScenario === 'function_named'
            ? { type: 'function', function: { name: 'unirouter_ui_echo' } }
            : undefined;
      const functionToolsRequested = playgroundScenario === 'function_default' || toolChoice !== undefined;
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${key}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          model: effectivePlaygroundModel,
          stream,
          messages: [{ role: 'user', content: prompt }],
          ...(maxTokensText ? { max_tokens: Number(maxTokensText) } : {}),
          ...(playgroundThinking ? { thinking: { type: playgroundThinking } } : {}),
          ...(playgroundReasoningEffort ? { reasoning_effort: playgroundReasoningEffort } : {}),
          ...(functionToolsRequested ? { tools: [functionTool] } : {}),
          ...(toolChoice ? { tool_choice: toolChoice } : {}),
          ...(playgroundScenario === 'json_object' ? { response_format: { type: 'json_object' } } : {}),
        }),
      });
      responseStatus = response.status;
      headersMS = elapsed();

      if (stream && response.ok && response.body) {
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let content = '';
        let finalMeta: any = null;
        let finishReason: string | undefined;
        let streamError: any = null;
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop() || '';
          for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed.startsWith('data:')) continue;
            const data = trimmed.slice(5).trim();
            if (!data || data === '[DONE]') continue;
            try {
              const chunk = JSON.parse(data);
              content += chunk?.choices?.[0]?.delta?.content || '';
              if (typeof chunk?.choices?.[0]?.finish_reason === 'string') {
                finishReason = chunk.choices[0].finish_reason;
              }
              if (chunk?._uniroute) finalMeta = { ...(finalMeta || {}), ...chunk._uniroute };
              if (chunk?.usage) finalMeta = { ...(finalMeta || {}), usage: chunk.usage };
              if (chunk?.error) streamError = chunk.error;
            } catch {
              // Ignore malformed SSE lines from intermediaries; the gateway error path is handled below.
            }
          }
        }
        if (streamError) {
          setPlaygroundResult({
            status: response.status,
            content: '',
            request_id: streamError?.request_id || finalMeta?.request_id,
            latency_ms: elapsed(),
            headers_ms: headersMS,
            billing_status: streamError?.billing_status || finalMeta?.billing_status,
            error: [streamError?.code, streamError?.message].filter(Boolean).join('：') || t("流式响应失败"),
          });
          return;
        }
        setPlaygroundResult({
          status: response.status,
          content: content || '(empty stream)',
          request_id: finalMeta?.request_id,
          cost_micros: finalMeta?.cost?.charged_cost_micros,
          total_tokens: finalMeta?.usage?.total_tokens,
          latency_ms: elapsed(),
          headers_ms: headersMS,
          usage: finalMeta?.usage,
          finish_reason: finishReason,
          usage_source: finalMeta?.usage_source,
        });
        return;
      }

      const payload = await response.json().catch(() => {
        throw new Error(t("响应不是合法 JSON；不能作为成功的模型输出。"));
      });
      if (!response.ok) {
        const error = payload?.error;
        setPlaygroundResult({
          status: response.status,
          content: '',
          latency_ms: elapsed(),
          headers_ms: headersMS,
          request_id: response.headers.get('X-Request-ID') || undefined,
          error: (typeof error === 'string' ? error : error?.message) || `HTTP ${response.status}`,
          doc_url: error?.doc_url,
        });
        return;
      }
      const responseMessage = payload?.choices?.[0]?.message;
      const responseHasText = typeof responseMessage?.content === 'string';
      const responseHasToolCalls = Array.isArray(responseMessage?.tool_calls) && responseMessage.tool_calls.length > 0;
      if (!payload || payload.error || !Array.isArray(payload.choices) || (!responseHasText && !responseHasToolCalls)) {
        throw new Error(t("响应不符合 Chat 文本或工具调用合同；不能作为成功的模型输出。"));
      }
      const uniroute = payload?._uniroute || {};
      setPlaygroundResult({
        status: response.status,
        content: responseHasToolCalls
          ? JSON.stringify(responseMessage.tool_calls, null, 2)
          : responseMessage.content || t("(空文本响应)"),
        request_id: uniroute?.request_id || payload?.id,
        cost_micros: uniroute?.cost?.charged_cost_micros,
        total_tokens: payload?.usage?.total_tokens,
        latency_ms: elapsed(),
        headers_ms: headersMS,
        usage: payload.usage,
        finish_reason: payload.choices[0].finish_reason,
        usage_source: uniroute?.usage_source || payload?.usage_source,
      });
    } catch (err: any) {
      setPlaygroundResult({
        status: responseStatus,
        content: '',
        latency_ms: elapsed(),
        headers_ms: headersMS,
        error: err?.message || t("调试请求失败"),
      });
    } finally {
      setPlaygroundLoading(false);
    }
  };

  const keyColumns = [
    { title: t("名称"), dataIndex: 'name', key: 'name' },
    { title: t("Key 前缀"), dataIndex: 'key_prefix', key: 'key_prefix', render: (value: string) => <Text code>{value}***</Text> },
    { title: t("允许模型"), dataIndex: 'allowed_models', key: 'allowed_models', render: (value: string) => value || t("继承项目模型集") },
    { title: t("限额"), key: 'limits', render: (_: unknown, record: ApiKey) => keyQuotaText(record) },
    { title: t("状态"), dataIndex: 'status', key: 'status', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    {
      title: t("操作"),
      key: 'actions',
      width: 96,
      render: (_: unknown, record: ApiKey) => {
        const canDisableRecord = mayDisableKey &&
          record.status !== 'disabled' &&
          (!developerOwnKeyOnly || (record.status === 'active' && record.owner_user_id === currentUserID));
        return (
          canDisableRecord
            ? (
              <Tooltip content={t("停用项目 Key")}>
                <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDisableKey(record)} aria-label={t("停用项目 Key")} />
              </Tooltip>
            )
            : <Text type="tertiary">-</Text>
        );
      },
    },
  ];

  const modelColumns = [
    {
      title: t("模型 / 别名"),
      dataIndex: 'model_name',
      key: 'model_name',
      render: (value: string, record: CatalogModel) => (
        <div className="ur-dev-model-name">
          <Text strong>{record.display_name || value}</Text>
          <Text type="tertiary" size="small">{value}</Text>
          {record.lifecycle === 'deprecated' && record.migration_recommendation && (
            <div className="ur-dev-migration">
              <IconAlertTriangle />
              <Text type="warning" size="small">{record.migration_recommendation}</Text>
            </div>
          )}
        </div>
      ),
    },
    { title: t("供应商族"), dataIndex: 'provider_family', key: 'provider_family', render: (value: string) => value || '-' },
    { title: t("生命周期"), dataIndex: 'lifecycle', key: 'lifecycle', render: (value: string) => <StatusTag status={value}>{value}</StatusTag> },
    { title: t("上下文"), dataIndex: 'context_length', key: 'context_length', render: (value: number) => value ? compactNumber(value) : '-' },
    { title: t("能力"), dataIndex: 'capability_tags', key: 'capability_tags', render: (value: string) => splitCSV(value).slice(0, 4).join(', ') || '-' },
    { title: t("数据驻留"), dataIndex: 'data_residency', key: 'data_residency', render: (value: string) => value || '-' },
  ];

  const logColumns = [
    { title: 'Request ID', dataIndex: 'request_id', key: 'request_id', render: (value: string) => <Text code copyable={{ content: value }}>{value}</Text> },
    { title: t("模型"), key: 'model', render: (_: unknown, record: CallLog) => record.requested_model || record.model },
    { title: t("状态"), dataIndex: 'status_code', key: 'status_code', render: (value: number) => <StatusTag status={value}>{value}</StatusTag> },
    { title: 'Token', dataIndex: 'total_tokens', key: 'total_tokens', render: (value: number) => compactNumber(value || 0) },
    { title: t("费用"), dataIndex: 'cost_micros', key: 'cost_micros', render: (value: string) => <MoneyText micros={value || '0'} precision={6} tone="cost" /> },
    { title: t("延迟"), dataIndex: 'latency_ms', key: 'latency_ms', render: (value: number) => `${value || 0}ms` },
  ];

  const usageModelColumns = [
    { title: t("模型 / 别名"), dataIndex: 'key', key: 'key', render: (value: string) => <Text code>{value || '(unknown)'}</Text> },
    { title: t("请求数"), dataIndex: 'total_requests', key: 'total_requests', render: (value: number) => compactNumber(value || 0) },
    { title: t("成功 / 失败"), key: 'status_mix', render: (_: unknown, row: UsageAnalyticsRow) => `${row.success_requests || 0} / ${row.failed_requests || 0}` },
    { title: 'Token', dataIndex: 'total_tokens', key: 'total_tokens', render: (value: number) => compactNumber(value || 0) },
    { title: t("费用"), dataIndex: 'cost_micros', key: 'cost_micros', render: (value: string) => <MoneyText micros={value || '0'} precision={6} tone="cost" /> },
    { title: t("平均延迟"), dataIndex: 'avg_latency_ms', key: 'avg_latency_ms', render: (value: number) => `${Math.round(value || 0)}ms` },
  ];

  if (initialLoading) {
    return (
      <div className="ur-page ur-developer-workspace">
        <Surface>
          <div className="ur-dev-loading"><IconRefresh />  {t("正在加载项目上下文")}</div>
        </Surface>
      </div>
    );
  }

  if (projectsLoadError) {
    return (
      <div className="ur-page ur-developer-workspace">
        <PageHeader
          eyebrow={t("项目空间")}
          title={t("项目工作台")}
          description={t("读取项目上下文后才能展示当前账号的凭证、模型和用量。")}
        />
        <Surface>
          <EmptyState
            title={t("项目列表加载失败")}
            description={translateLabel(projectsLoadError)}
            actions={<Button type="primary" icon={<IconRefresh />} onClick={loadProjects}>{t("重新加载项目")}</Button>}
            minHeight={320}
          />
        </Surface>
      </div>
    );
  }

  if (projects.length === 0) {
    return (
      <div className="ur-page ur-developer-workspace">
        <PageHeader
          eyebrow={t("项目空间")}
          title={t("项目工作台")}
          description={t("当前账号没有可访问项目。")}
        />
        <EmptyState
          title={t("没有可访问项目")}
          description={noProjectDescription}
          minHeight={320}
        />
      </div>
    );
  }

  const projectHeader = (
    <>
      <PageHeader
        eyebrow={t("项目空间")}
        title={t("项目工作台")}
        description={workbenchDescription}
        actions={(
          <Space>
            <Select
              value={selectedProjectId}
              onChange={handleProjectChange}
              optionList={projects.map(projectOption)}
              style={{ width: 280 }}
              aria-label={t("选择项目")}
            />
            {(isOrgAdminRole(organizationRole) || isProjectAdminMembership(selectedProjectMembershipRole)) && selectedProjectId && (
              <Tooltip content={notificationsLicensed ? t("打开当前项目通知") : notificationsRestriction}>
                <span>
                  <Button
                    icon={<IconBell />}
                    aria-label={notificationsLicensed ? t("打开当前项目通知") : t("项目通知（商业授权不可用）")}
                    disabled={!notificationsLicensed}
                    onClick={() => navigate(`/notifications?project_id=${selectedProjectId}`)}
                  >
                    {notificationsLicensed ? t("项目通知") : t("项目通知（商业授权不可用）")}
                  </Button>
                </span>
              </Tooltip>
            )}
            <Button icon={<IconRefresh />} onClick={loadProjectData} loading={dataLoading}>{t("刷新")}</Button>
          </Space>
        )}
      />

      <section className="ur-dev-context">
        <div>
          <Text className="ur-page-eyebrow">{t("当前项目")}</Text>
          <strong>{projectDisplayName(selectedProject)}</strong>
          <Text type="tertiary">/{selectedProject?.slug}</Text>
        </div>
        <StatusTag status={selectedProject?.status || 'active'}>{selectedProject?.status || 'active'}</StatusTag>
        <StatusTag status={projectRoleLabel}>{projectRoleLabel}</StatusTag>
        <Text type="tertiary">{t("项目标识：")}{selectedProject?.slug || '-'}</Text>
      </section>
    </>
  );

  const workbenchTaskNav = (
    <nav className="ur-dev-task-nav" aria-label={t("项目工作台任务入口")}>
      <Text strong>{t("快速定位")}</Text>
      <div className="ur-dev-task-links">
        <a href="#project-usage">{t("概览与用量")}</a>
        <a href="#project-access">{t("项目凭证")}</a>
        <a href="#project-api">{t("API 快速接入")}</a>
        {mayRunPlayground && <a href="#project-debug">{gatewayLicensed ? t("调试与排障") : t("调试与排障（授权未包含）")}</a>}
        <a href="#project-models">{modelGovernanceLicensed ? t("已启用模型") : t("已启用模型（商业授权不可用）")}</a>
      </div>
    </nav>
  );

  if (dataLoading) {
    return (
      <div className="ur-page ur-page-wide ur-developer-workspace">
        {projectHeader}
        <Surface>
          <div className="ur-dev-loading"><IconRefresh />  {t("正在加载当前项目数据")}</div>
        </Surface>
      </div>
    );
  }

  if (projectDataError) {
    return (
      <div className="ur-page ur-page-wide ur-developer-workspace">
        {projectHeader}
        <Surface>
          <EmptyState
            title={t("项目数据加载失败")}
            description={translateLabel(projectDataError)}
            actions={<Button type="primary" icon={<IconRefresh />} onClick={loadProjectData}>{t("重新加载当前项目")}</Button>}
            minHeight={320}
          />
        </Surface>
      </div>
    );
  }

  return (
    <div className="ur-page ur-page-wide ur-developer-workspace">
      {projectHeader}
      {workbenchTaskNav}

      <section id="project-usage" className="ur-dev-section-stack">
        <div className="ur-kpi-grid">
          <MetricTile label={t("请求数")} value={compactNumber(usageSnapshot.requestCount)} icon={<IconFile />} tone="blue" meta={usageSnapshot.sourceLabel} />
          <MetricTile label="Token" value={compactNumber(usageSnapshot.totalTokens)} icon={<IconServer />} tone="violet" meta={usageSnapshot.sourceLabel} />
          <MetricTile label={t("费用")} value={<MoneyText micros={usageSnapshot.totalCostMicros} precision={6} strong tone="cost" />} icon={<IconCoinMoney />} tone="green" meta={usageSnapshot.sourceLabel} />
          <MetricTile label={t("错误率")} value={`${usageSnapshot.errorRate.toFixed(1)}%`} icon={<IconAlertTriangle />} tone={usageSnapshot.errorRate > 5 ? 'red' : 'amber'} meta={t("平均延迟 {v0}ms", { v0: String(usageSnapshot.avgLatency.toFixed(0)) })} />
        </div>

        <Surface title={t("我的模型用量")} description={t("按当前账号名下 API Key 聚合当前项目的模型用量和费用；历史未归属 Owner 的调用不会被计入。")}>
          {usageLoadError ? (
            <EmptyState
              title={t("个人用量加载失败")}
              description={t("{v0}。上方指标暂按最近 20 条调用计算，并非完整用量汇总。", { v0: t("无法读取当前项目的个人用量汇总{v0}", { v0: usageLoadError.detail ? `: ${usageLoadError.detail}` : '' }) })}
              actions={<Button icon={<IconRefresh />} onClick={loadProjectData}>{t("重新加载当前项目")}</Button>}
              minHeight={180}
            />
          ) : (
            <div className="ur-table-shell">
              <Table
                columns={usageModelColumns}
                dataSource={usageByModel}
                rowKey="key"
                loading={dataLoading}
                pagination={false}
                empty={<EmptyState title={t("暂无我的用量")} description={t("使用当前账号名下的项目 Key 调用后，会在这里按模型显示请求、Token 和费用。")} minHeight={180} />}
                size="small"
              />
            </div>
          )}
        </Surface>
      </section>

      <div id="project-access" className="ur-dev-main-grid ur-dev-section-anchor">
        <Surface
          title={t("项目访问凭证")}
          description={keySurfaceDescription}
          actions={mayCreateKey && <Button icon={<IconPlus />} type="primary" onClick={openCreateKey}>{t("创建项目凭证")}</Button>}
        >
          <div className="ur-table-shell">
            <Table
              columns={keyColumns}
              dataSource={keys}
              rowKey="id"
              loading={dataLoading}
              pagination={{
                currentPage: keyPage,
                pageSize: keyPageSize,
                total: keys.length,
                pageSizeOpts: [10, 20, 50, 100],
                showSizeChanger: true,
                showTotal: true,
                onPageChange: (page: number) => setKeyPage(page),
                onPageSizeChange: (pageSize: number) => { setKeyPageSize(pageSize); setKeyPage(1); },
              }}
              empty={<EmptyState title={t("暂无项目凭证")} description={emptyKeyDescription} minHeight={180} />}
              size="small"
            />
          </div>
        </Surface>

        <div id="project-api" className="ur-dev-section-anchor">
          <ProjectApiQuickstart
            baseUrl={baseUrl}
            modelOptions={modelOptions}
            apiKey={sampleKey}
            canInvoke={mayRunPlayground}
            gatewayAvailable={gatewayLicensed}
            unavailableReason={gatewayRestriction}
            modelWarning={!modelGovernanceLicensed
              ? t("{v0} 请使用管理员提供的现有模型或别名替换示例占位符。", { v0: String(modelGovernanceRestriction) })
              : undefined}
            troubleshootingHref="/docs#developer-errors"
          />
        </div>
      </div>

      {mayRunPlayground && (
        <div id="project-debug" className="ur-dev-section-anchor">
          {gatewayLicensed ? (
            <Surface
              title={t("请求调试台")}
              description={t("使用项目访问凭证直接调用兼容 OpenAI 协议的网关；提示词和响应仅保留在当前页面，不写入历史。")}
            >
              <div className="ur-playground-grid">
                <div className="ur-playground-controls">
                  <div className="ur-inline-controls">
                    <Input
                      value={playgroundKey}
                      onChange={setPlaygroundKey}
                      type="password"
                      placeholder={t("粘贴项目访问凭证")}
                      style={{ width: 260 }}
                    />
                    {modelGovernanceLicensed ? (
                      <Select
                        aria-labelledby="playground-model-label"
                        value={effectivePlaygroundModel}
                        optionList={modelOptions.map(model => ({ label: model, value: model }))}
                        onChange={handlePlaygroundModelChange}
                        style={{ width: 220 }}
                        placeholder={t("选择已批准模型或别名")}
                      />
                    ) : (
                      <Input
                        value={playgroundModel}
                        onChange={handlePlaygroundModelChange}
                        style={{ width: 220 }}
                        placeholder={t("输入现有模型或别名")}
                      />
                    )}
                    <Select
                      value={playgroundMode}
                      optionList={[
                        { label: t("非流式"), value: 'non_stream' },
                        { label: t("流式"), value: 'stream' },
                      ]}
                      onChange={(value) => setPlaygroundMode(String(value || 'non_stream'))}
                      disabled={playgroundScenario !== 'text'}
                      style={{ width: 120 }}
                    />
                    <Button type="primary" icon={<IconBolt />} loading={playgroundLoading}
                      disabled={!effectivePlaygroundModel || dataLoading || Boolean(projectDataError) || maxTokensInvalid}
                      onClick={handlePlaygroundRun}>{t("运行")}</Button>
                  </div>
                  <Text id="playground-model-label" type="tertiary">{t("调试模型")}</Text>
                  {modelGovernanceLicensed && !modelOptions.length && <Text type="warning">{t("项目没有可选的稳定模型，不能运行调试请求。")}</Text>}
                  {modelGovernanceLicensed && modelOptions.length > 0 && playgroundModel && !effectivePlaygroundModel &&
                    <Text type="warning">{t("之前选择的模型已不在当前目录，请重新选择；不会自动切换到其他模型。")}</Text>}
                  <div className="ur-inline-controls" style={{ marginTop: 12 }}>
                    <label>
                      <Text>{t("max_tokens（可选）")}</Text>
                      <Input aria-label={t("max_tokens（可选）")} value={playgroundMaxTokens}
                        onChange={setPlaygroundMaxTokens} placeholder={t("例如 128；留空不发送")} style={{ width: 230 }} />
                    </label>
                    <div>
                      <Text id="playground-thinking-label">{t("thinking.type（可选）")}</Text>
                      <Select aria-labelledby="playground-thinking-label" value={playgroundThinking}
                        onChange={value => {
                          const next = String(value || '');
                          setPlaygroundThinking(next);
                          if (next === 'disabled') setPlaygroundReasoningEffort('');
                        }}
                        optionList={[
                          { label: t("不指定（不发送）"), value: '' },
                          { label: t("disabled（关闭）"), value: 'disabled' },
                          { label: t("enabled（开启）"), value: 'enabled' },
                        ]} style={{ width: 200 }} />
                    </div>
                    <div>
                      <Text id="playground-reasoning-effort-label">{t("reasoning_effort（可选）")}</Text>
                      <Select aria-labelledby="playground-reasoning-effort-label" value={playgroundReasoningEffort}
                        onChange={value => {
                          const next = String(value || '');
                          setPlaygroundReasoningEffort(next);
                          if (next) setPlaygroundThinking('');
                        }}
                        optionList={[
                          { label: t("不指定（不发送）"), value: '' },
                          { label: 'low', value: 'low' },
                          { label: 'medium', value: 'medium' },
                          { label: 'high', value: 'high' },
                          { label: 'xhigh', value: 'xhigh' },
                          { label: 'max', value: 'max' },
                        ]} style={{ width: 210 }} />
                    </div>
                    <div>
                      <Text id="playground-scenario-label">{t("能力场景")}</Text>
                      <Select aria-labelledby="playground-scenario-label" value={playgroundScenario}
                        onChange={value => {
                          const next = String(value || 'text');
                          setPlaygroundScenario(next);
                          if (next !== 'text') setPlaygroundMode('non_stream');
                        }}
                        optionList={[
                          { label: t("普通文本"), value: 'text' },
                          { label: 'Function Tool · Provider default', value: 'function_default' },
                          { label: 'Function Tool · auto', value: 'function_auto' },
                          { label: 'Function Tool · required', value: 'function_required' },
                          { label: 'Function Tool · named', value: 'function_named' },
                          { label: 'JSON Object', value: 'json_object' },
                        ]} style={{ width: 220 }} />
                    </div>
                  </div>
                  <Text type="tertiary">{t("这些字段仅在显式填写时发送；thinking 与 reasoning_effort 是供应商或 surface 扩展，并非所有模型都支持。值域和输出上限仍由精确 Profile 校验；留空不是零输出，也不是供应商最大输出。")}</Text>
                  {maxTokensInvalid && <Text type="danger">{t("max_tokens 必须是正整数。")}</Text>}
                  {playgroundScenario === 'function_default' && <Text type="tertiary">

                    {t("仅发送 tools，不发送 tool_choice；是否采用默认 auto 仍由服务端精确 Profile 校验，不代表强制调用工具。")}
                  </Text>}
                  {!modelGovernanceLicensed && <Text type="warning">{modelGovernanceRestriction}  {t("调试仍可使用管理员提供的现有模型或别名。")}</Text>}
                  <TextArea
                    value={playgroundPrompt}
                    onChange={setPlaygroundPrompt}
                    rows={5}
                    placeholder={t("输入本次测试提示词")}
                    style={{ width: '100%', marginTop: 12 }}
                  />
                </div>
                <div className="ur-playground-result">
                  {playgroundResult ? (
                    <>
                      <div className="ur-chip-row">
                        <StatusTag status={playgroundResult.error ? 'failed' : playgroundResult.status}>
                          {playgroundResult.error ? 'response_error' : playgroundResult.status || 'network_error'}
                        </StatusTag>
                        {playgroundResult.error && playgroundResult.status > 0 && <Text>HTTP {playgroundResult.status}</Text>}
                        {playgroundResult.request_id && <Text code>{playgroundResult.request_id}</Text>}
                        {playgroundResult.total_tokens !== undefined && <StatusTag status="tokens">{playgroundResult.total_tokens} tokens</StatusTag>}
                        {playgroundResult.cost_micros !== undefined && <MoneyText micros={playgroundResult.cost_micros} precision={6} tone="cost" />}
                        {playgroundResult.latency_ms !== undefined && <Text type="tertiary">{t("客户端完整耗时")} {playgroundResult.latency_ms}ms</Text>}
                        {playgroundResult.headers_ms !== undefined && <Text type="tertiary">{t("响应头耗时")} {playgroundResult.headers_ms}ms</Text>}
                        {playgroundResult.billing_status && <Text type="tertiary">billing_status：{playgroundResult.billing_status}</Text>}
                      </div>
                      {playgroundResult.error ? (
                        <div className="ur-playground-error">
                          <Text type="danger">{playgroundResult.error}</Text>
                          {playgroundResult.doc_url && <a href={playgroundResult.doc_url}>{t("查看错误文档")}</a>}
                        </div>
                      ) : (
                        <pre className="ur-dev-code">{playgroundResult.content}</pre>
                      )}
                      {playgroundResult.finish_reason && <Text>finish_reason：{playgroundResult.finish_reason}</Text>}
                      {playgroundResult.finish_reason === 'length' && <Text type="warning">{t("输出达到上限，响应可能未完成。")}</Text>}
                      {playgroundResult.usage !== undefined && <details>
                        <summary>{t("usage（响应原值）")}</summary>
                        <pre className="ur-dev-code">{JSON.stringify(playgroundResult.usage, null, 2)}</pre>
                      </details>}
                    </>
                  ) : (
                    <EmptyState title={t("等待运行")} description={t("填写项目 Key、现有模型或别名后运行一次测试。")} minHeight={180} />
                  )}
                </div>
              </div>
            </Surface>
          ) : (
            <Surface title={t("请求调试台")} description={t("此区块需要模型网关授权。")}>
              <EmptyState title={t("调试调用暂不可用")} description={gatewayRestriction} minHeight={180} />
            </Surface>
          )}
        </div>
      )}

      <div id="project-models" className="ur-dev-section-anchor">
        <Surface title={t("已启用模型")} description={t("只展示当前项目可直接调用的稳定模型 ID 和已明确配置的别名。")}>
          {modelGovernanceLicensed ? (
            <div className="ur-table-shell">
              <Table
                columns={modelColumns}
                dataSource={models}
                rowKey="id"
                loading={dataLoading}
                pagination={{
                  currentPage: modelPage,
                  pageSize: modelPageSize,
                  total: models.length,
                  pageSizeOpts: [10, 20, 50, 100],
                  showSizeChanger: true,
                  showTotal: true,
                  onPageChange: (page: number) => setModelPage(page),
                  onPageSizeChange: (pageSize: number) => { setModelPageSize(pageSize); setModelPage(1); },
                }}
                empty={<EmptyState title={t("暂无已启用模型")} description={t("请先在“模型连接”完成该模型的真实检测与启用。")} minHeight={180} />}
                size="small"
              />
            </div>
          ) : (
            <EmptyState title={t("模型目录暂不可用")} description={modelGovernanceRestriction} minHeight={180} />
          )}
        </Surface>
      </div>

      <div className="ur-dev-main-grid">
        <Surface title={t("最近调用")} description={t("项目最近请求，用于快速确认状态、token、费用和延迟。")}>
          <div className="ur-table-shell">
            <Table
              columns={logColumns}
              dataSource={logs}
              rowKey="id"
              loading={dataLoading}
              pagination={false}
              empty={<EmptyState title={t("暂无调用记录")} description={t("项目完成调用后会显示最近请求。")} minHeight={180} />}
              size="small"
            />
          </div>
        </Surface>

        <Surface title={t("请求定位")} description={t("按 Request ID 定位项目内请求，不展示上游凭证、完整访问凭证、提示词或响应。")}>
          <div className="ur-dev-lookup">
            <Space align="start">
              <Input
                value={lookupRequestId}
                onChange={setLookupRequestId}
                placeholder="req-..."
                showClear
                style={{ width: 300 }}
                onEnterPress={handleLookup}
              />
              <Button icon={<IconSearch />} type="primary" loading={lookupLoading} onClick={handleLookup}>{t("查询")}</Button>
            </Space>
            {lookupResult ? (
              <div className="ur-dev-lookup-grid">
                <div><Text type="tertiary">{t("状态")}</Text><StatusTag status={lookupResult.status_code}>{lookupResult.status_code}</StatusTag></div>
                <div><Text type="tertiary">{t("模型")}</Text><strong>{lookupResult.requested_model || lookupResult.model}</strong></div>
                {isOrgAdminRole(organizationRole) && (
                  <div><Text type="tertiary">{t("路由模型")}</Text><strong>{lookupResult.model}</strong></div>
                )}
                <div><Text type="tertiary">{t("供应商")}</Text><strong>{lookupResult.provider || '-'}</strong></div>
                <div><Text type="tertiary">{t("通道")}</Text><strong>{lookupResult.channel || '-'}</strong></div>
                <div><Text type="tertiary">Token</Text><strong>{compactNumber(lookupResult.total_tokens || 0)}</strong></div>
                <div><Text type="tertiary">{t("费用")}</Text><MoneyText micros={lookupResult.cost_micros || '0'} precision={6} strong tone="cost" /></div>
                <div><Text type="tertiary">{t("延迟")}</Text><strong>{lookupResult.latency_ms || 0}ms</strong></div>
                {lookupResult.upstream_error && (
                  <div className="ur-dev-lookup-wide">
                    <Text type="tertiary">{t("上游错误")}</Text>
                    <Text>{lookupResult.upstream_error}</Text>
                  </div>
                )}
              </div>
            ) : (
              <EmptyState title={t("等待查询")} description={t("输入项目内 Request ID 后显示排障摘要。")} minHeight={180} />
            )}
          </div>
        </Surface>
      </div>

      <SideSheet
        title={t("创建项目访问凭证")}
        visible={showKeySheet}
        onCancel={closeCreateKey}
        width={500}
      >
        <Form getFormApi={(apiRef: any) => setFormApi(apiRef)} onSubmit={handleCreateKey} initValues={{ rpm_limit: 60, rps_limit: 10 }}>
          <Form.Input field="name" label={t("凭证名称")} placeholder="frontend-prod" rules={[{ required: true, message: t("请输入凭证名称") }]} />
          {modelGovernanceLicensed ? (
            <Form.Select
              field="allowed_models"
              aria-label={t("允许模型或别名")}
              label={t("允许模型 / 别名")}
              placeholder={t("留空表示继承项目模型集")}
              multiple
              optionList={modelOptions.map(model => ({ label: model, value: model }))}
              style={{ width: '100%' }}
            />
          ) : (
            <Text type="warning">{modelGovernanceRestriction}  {t("新凭证将继承项目现有模型范围。")}</Text>
          )}
          <Form.Input field="allowed_ips" label={t("允许 IP")} placeholder={t("留空表示不限制，多个 IP 用逗号分隔")} />
          <Form.InputNumber field="rpm_limit" label={t("RPM 限速")} min={1} style={{ width: '100%' }} />
          <Form.InputNumber field="rps_limit" label={t("RPS 限速")} min={1} style={{ width: '100%' }} />
          <Text type="tertiary" size="small">{t("TPM 暂未配置为 Key 级限额；当前仅展示口径，不启用 Scheduler V2。")}</Text>
          <div className="ur-form-actions">
            <Button onClick={closeCreateKey}>{t("取消")}</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{t("创建")}</Button>
          </div>
        </Form>
      </SideSheet>

      <Modal
        title={t("请立即保存完整项目 Key")}
        visible={!!createdKey}
        onCancel={() => setCreatedKey(null)}
        footer={(
          <Space>
            <Button icon={<IconCopy />} onClick={copyCreatedKey}>{t("复制 Key")}</Button>
            <Button type="primary" onClick={() => setCreatedKey(null)}>{t("我已保存")}</Button>
          </Space>
        )}
      >
        <Text type="warning">

          {t("完整 Key 只在创建后显示一次。关闭后系统只保留哈希，无法再次查看。")}
        </Text>
        <div className="ur-secret-reveal">
          <Text strong>{createdKey?.name}</Text>
          <code>{createdKey?.key}</code>
          <Text type="tertiary">{t("前缀：")}{createdKey?.key_prefix}***</Text>
        </div>
      </Modal>
    </div>
  );
}
