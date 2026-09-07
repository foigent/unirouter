import type { ModelExecutionProfile, ParameterControl } from '../../api/modelExecutionProfiles';

const tokens = (n: number) => n.toLocaleString('en-US');
const path = (control: ParameterControl) => [control.object, control.field].filter(Boolean).join('.');

export default function ModelParameterDescription({ profile }: { profile: ModelExecutionProfile }) {
  const controls = profile.parameter_controls;
  if (!controls || controls.version !== 'unirouter.parameter-controls.v1') return null;
  const reasoning = controls.reasoning;
  return <section aria-label="所选模型的参数说明" className="ur-model-parameter-description">
    <p><strong>所选模型的参数说明</strong></p>
    <dl>
      <dt>请求默认输出（Token）</dt>
      <dd>{tokens(profile.default_output_limit)}；实际请求还受公共输出上限和上下文余量限制。</dd>
      {controls.output && <><dt>输出参数</dt><dd>
        <code>{controls.output.field}</code>
        {controls.output.mutually_exclusive && '；不可与其他输出长度字段同时设置。'}
      </dd></>}
      {reasoning && <>
        <dt>思考模式</dt>
        <dd>{reasoning.mode === 'toggle' ? '可按合同控制' : reasoning.mode === 'required' ? '必须启用' : '默认启用'}
          {reasoning.toggle && <> · <code>{path(reasoning.toggle)}</code>：{reasoning.toggle.values.join(' / ')}
            {reasoning.toggle.provider_default && `；供应商默认 ${reasoning.toggle.provider_default}`}</>}
        </dd>
        <dt>原生思考强度</dt>
        <dd>{reasoning.effort ? <>
          <code>{path(reasoning.effort)}</code>：{reasoning.effort.values.join(' / ')}
          {reasoning.effort.provider_default && `；供应商默认 ${reasoning.effort.provider_default}`}
          {Object.entries(reasoning.effort.aliases || {}).length > 0 && <div>
            兼容映射：{Object.entries(reasoning.effort.aliases!).map(([from, to]) => `${from} → ${to}`).join('，')}。映射值不是额外的原生强度。
          </div>}
        </> : '未声明分档强度；启用思考不等于支持 low / medium / high。'}</dd>
        <dt>思考预算（Token）</dt>
        <dd>{reasoning.budget ? <>
          <code>{reasoning.budget.field}</code>：{tokens(reasoning.budget.minimum)}–{tokens(reasoning.budget.maximum)}
          ；供应商默认 {tokens(reasoning.budget.provider_default)}。
          {reasoning.budget.requires_active_reasoning && '仅在思考启用时有效。'}
          {reasoning.budget.mutually_exclusive_with_effort && '不可与思考强度同时设置。'}
        </> : '未声明可配置预算；不能解释为 0。'}</dd>
        {Object.entries(reasoning.active_output_field_maximums || {}).map(([field, value]) => <div key={field}>
          <dt>思考启用时的输出限制</dt><dd><code>{field}</code> ≤ {tokens(value)}</dd>
        </div>)}
      </>}
    </dl>
    <p>思考预算与公共输出上限是不同控制，不能相加或互相替代。实际可用组合由请求及已启用的模型版本共同决定。</p>
    <p>客户端菜单与供应商参数可能不同；OpenClaw 同步结果会列出可用选项、映射和暂不支持的控制。</p>
  </section>;
}
