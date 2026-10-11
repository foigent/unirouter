import '../../i18n/developer';
import { translate as t, useI18n, translateLabel } from '../../i18n';
import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Select, Tag, Typography } from '@douyinfe/semi-ui';
import { EmptyState, Surface } from '../../ui';
import { buildChatCompletionsCurl } from '../../utils/shell';

const { Text } = Typography;

interface ProjectApiQuickstartProps {
  baseUrl: string;
  modelOptions: string[];
  apiKey?: string;
  canInvoke: boolean;
  gatewayAvailable: boolean;
  unavailableReason?: string;
  modelWarning?: string;
  title?: string;
  description?: string;
  troubleshootingHref?: string;
}

const coreEndpoints = [
  ['GET', '/v1/models', '查看当前项目可见模型'],
  ['POST', '/v1/chat/completions', '发送对话请求'],
  ['POST', '/v1/embeddings', '仅在项目有已启用向量模型时生成向量'],
] as const;

export default function ProjectApiQuickstart({
  baseUrl,
  modelOptions,
  apiKey = '<YOUR_PROJECT_API_KEY>',
  canInvoke,
  gatewayAvailable,
  unavailableReason,
  modelWarning,
  title = t("API 快速接入"),
  description = t("使用当前项目的访问凭证和已启用模型调用 OpenAI 兼容接口。"),
  troubleshootingHref,
}: ProjectApiQuickstartProps) {
  useI18n();
  const fallbackModel = modelOptions[0] || '<APPROVED_MODEL_OR_ALIAS>';
  const [selectedModel, setSelectedModel] = useState(fallbackModel);

  useEffect(() => {
    if (!modelOptions.includes(selectedModel)) setSelectedModel(fallbackModel);
  }, [fallbackModel, modelOptions, selectedModel]);

  const curl = useMemo(
    () => buildChatCompletionsCurl(baseUrl, apiKey, selectedModel),
    [apiKey, baseUrl, selectedModel],
  );

  return (
    <Surface title={title} description={description}>
      {!gatewayAvailable ? (
        <EmptyState title={t("模型接入暂不可用")} description={unavailableReason || t("当前模型网关不可用。")} minHeight={180} />
      ) : !canInvoke ? (
        <EmptyState
          title={t("当前项目角色为只读")}
          description={t("你可以查看项目模型和请求记录，但不能创建或使用项目访问凭证；需要调用权限时请联系项目管理员。")}
          minHeight={180}
        />
      ) : (
        <div className="ur-dev-docs">
          <div>
            <Text className="ur-filter-label">{t("OpenAI 兼容 Base URL")}</Text>
            <div><Text code>{baseUrl}/v1</Text></div>
          </div>
          <div>
            <Text className="ur-filter-label">{t("模型 / 别名")}</Text>
            {modelOptions.length > 0 ? (
              <Select
                aria-label={t("快速接入模型")}
                value={selectedModel}
                optionList={modelOptions.map(model => ({ label: model, value: model }))}
                onChange={value => setSelectedModel(String(value || fallbackModel))}
                style={{ width: 'min(100%, 360px)' }}
              />
            ) : (
              <div><Text code>{fallbackModel}</Text></div>
            )}
          </div>
          {modelWarning && <Text type="warning">{modelWarning}</Text>}
          <div>
            <Text className="ur-filter-label">{t("常用兼容接口")}</Text>
            <div>
              <Text type="tertiary">{t("具体可用性取决于所选模型已验证的能力；系统不会按模型名称推断。")}</Text>
            </div>
            <div className="ur-api-endpoint-list">
              {coreEndpoints.map(([method, path, label]) => (
                <div key={`${method}:${path}`}>
                  <Tag color={method === 'GET' ? 'blue' : 'green'}>{method}</Tag>
                  <Text code>{path}</Text>
                  <Text type="tertiary">{translateLabel(label)}</Text>
                </div>
              ))}
            </div>
          </div>
          <Text>

            {t("在请求头使用")} <Text code>{`Authorization: Bearer <${t("项目 API Key")}>`}</Text>{t("；控制台登录会话不能替代项目 API Key。")}
          </Text>
          <pre className="ur-dev-code">{curl}</pre>
          <details className="ur-inline-details">
            <summary>{t("高级请求标识")}</summary>
            <div className="ur-dev-docs">
              <Text><Text code>X-Request-ID</Text>  {t("由 UniRouter 返回，用于串联请求日志、计费与审计。")}</Text>
              <Text><Text code>X-UniRouter-Client-Request-ID</Text>  {t("可由调用方提供，仅用于关联查询。")}</Text>
              <Text><Text code>Idempotency-Key</Text>  {t("用于需要安全重试的请求；同一键必须保持请求内容一致。")}</Text>
            </div>
          </details>
          {troubleshootingHref && <Link to={troubleshootingHref}>{t("查看错误排查与错误码")}</Link>}
        </div>
      )}
    </Surface>
  );
}
