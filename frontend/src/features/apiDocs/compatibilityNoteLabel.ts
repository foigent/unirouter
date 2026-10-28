import '../../i18n/developer';
import { translate as t } from '../../i18n';
import type { MessageKey } from '../../i18n/catalog';

const notes: Record<string, MessageKey> = {
  "Set api_key and base_url to the selected UniRouter Gateway Endpoint.": "将 api_key 和 base_url 配置为所选的 UniRouter 网关端点。",
  "Use the SDK Chat Completions model adapter; the default Responses path is not a runtime claim.": "使用 SDK 的 Chat Completions 模型适配器；默认 Responses 路径不代表已支持运行。",
  "Endpoint Profile exists as a contract only; runtime use is blocked until independently implemented and verified.": "端点 Profile 目前仅有合同定义；独立实现并验证前不可运行。",
  "Configure base_url, api_key and an explicitly approved model alias.": "配置 base_url、api_key 以及明确批准的模型别名。",
  "Use an OpenAI-compatible model client with the UniRouter Base URL.": "使用兼容 OpenAI 的模型客户端和 UniRouter Base URL。",
  "Register UniRouter as an OpenAI-compatible provider and keep secrets outside exported profiles.": "将 UniRouter 注册为兼容 OpenAI 的供应商，密钥不得写入导出的配置。",
  "Use the custom OpenAI-compatible model entry and Chat Completions path.": "使用自定义的兼容 OpenAI 模型入口与 Chat Completions 路径。",
  "Requires a separately deployed event adapter with idempotent acknowledgement; not part of the Gateway process.": "需要独立部署、支持幂等确认的事件适配器；不在网关进程内运行。",
  "Requires an exact customer Aily environment and separately verified adapter contract.": "需要明确的客户 Aily 环境，以及单独验证的适配器合同。"
};

export function compatibilityNoteLabel(note: string) {
  return Object.prototype.hasOwnProperty.call(notes, note) ? t(notes[note]) : note;
}
