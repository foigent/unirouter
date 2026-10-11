import '../../i18n/models';
import { translate as t, useI18n } from '../../i18n';
import type { ModelExecutionProfile, ParameterControl } from '../../api/modelExecutionProfiles';

const tokens = (n: number) => n.toLocaleString('en-US');
const path = (control: ParameterControl) => [control.object, control.field].filter(Boolean).join('.');

export default function ModelParameterDescription({ profile }: { profile: ModelExecutionProfile }) {
  useI18n();
  const controls = profile.parameter_controls;
  if (!controls || controls.version !== 'unirouter.parameter-controls.v1') return null;
  const reasoning = controls.reasoning;
  return <section aria-label={t("所选模型的参数说明")} className="ur-model-parameter-description">
    <p><strong>{t("所选模型的参数说明")}</strong></p>
    <dl>
      <dt>{t("请求默认输出（Token）")}</dt>
      <dd>{tokens(profile.default_output_limit)}{t("；实际请求还受公共输出上限和上下文余量限制。")}</dd>
      {controls.output && <><dt>{t("输出参数")}</dt><dd>
        <code>{controls.output.field}</code>
        {controls.output.mutually_exclusive && t("；不可与其他输出长度字段同时设置。")}
      </dd></>}
      {reasoning && <>
        <dt>{t("思考模式")}</dt>
        <dd>{reasoning.mode === 'toggle' ? t("可按合同控制") : reasoning.mode === 'required' ? t("必须启用") : t("默认启用")}
          {reasoning.toggle && <> · <code>{path(reasoning.toggle)}</code>：{reasoning.toggle.values.join(' / ')}
            {reasoning.toggle.provider_default && t('；供应商默认 {value}', { value: reasoning.toggle.provider_default })}</>}
        </dd>
        <dt>{t("原生思考强度")}</dt>
        <dd>{reasoning.effort ? <>
          <code>{path(reasoning.effort)}</code>：{reasoning.effort.values.join(' / ')}
          {reasoning.effort.provider_default && t('；供应商默认 {value}', { value: reasoning.effort.provider_default })}
          {Object.entries(reasoning.effort.aliases || {}).length > 0 && <div>

            {t("兼容映射：")}{Object.entries(reasoning.effort.aliases!).map(([from, to]) => `${from} → ${to}`).join('，')}{t("。映射值不是额外的原生强度。")}
          </div>}
        </> : t("未声明分档强度；启用思考不等于支持 low / medium / high。")}</dd>
        <dt>{t("思考预算（Token）")}</dt>
        <dd>{reasoning.budget ? <>
          <code>{reasoning.budget.field}</code>：{tokens(reasoning.budget.minimum)}–{tokens(reasoning.budget.maximum)}
          {t('；供应商默认 {value}', { value: tokens(reasoning.budget.provider_default) })}
          {reasoning.budget.requires_active_reasoning && t("仅在思考启用时有效。")}
          {reasoning.budget.mutually_exclusive_with_effort && t("不可与思考强度同时设置。")}
        </> : t("未声明可配置预算；不能解释为 0。")}</dd>
        {Object.entries(reasoning.active_output_field_maximums || {}).map(([field, value]) => <div key={field}>
          <dt>{t("思考启用时的输出限制")}</dt><dd><code>{field}</code> ≤ {tokens(value)}</dd>
        </div>)}
      </>}
    </dl>
    <p>{t("思考预算与公共输出上限是不同控制，不能相加或互相替代。实际可用组合由请求及已启用的模型版本共同决定。")}</p>
    <p>{t("客户端菜单与供应商参数可能不同；OpenClaw 同步结果会列出可用选项、映射和暂不支持的控制。")}</p>
  </section>;
}
