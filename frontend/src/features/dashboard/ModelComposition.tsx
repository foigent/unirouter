import { Surface, EmptyState, formatMoney } from '../../ui';
import { useI18n } from '../../i18n';
import type { ModelBreakdown } from './modelBreakdown';

export default function ModelComposition({ data, metric, selected, onSelect, fallbackTotal }: {
  data: ModelBreakdown; metric: 'requests' | 'cost'; selected: string | null; onSelect: (model: string) => void;
  fallbackTotal?: bigint;
}) {
  const { t, locale } = useI18n();
  const title = t(metric === 'cost' ? '模型费用构成' : '模型调用构成');
  const format = (value: bigint) => metric === 'cost' ? formatMoney(value) : value.toLocaleString(locale);
  let offset = 0;
  return <Surface title={title} className="ur-composition">
    <div className="ur-composition-total">{data.state === 'unavailable' ? fallbackTotal === undefined ? '—' : format(fallbackTotal) : format(data.total)}</div>
    {data.state === 'unavailable' ? <EmptyState title={t(metric === 'cost' ? '费用构成暂不可用' : '模型构成暂不可用')} description={t('总量来自窗口汇总；构成不可用。')} minHeight={170} />
      : data.state === 'signed' ? <EmptyState title={t('当前金额含负数，请查看明细。')} minHeight={170} />
      : <>
        {data.state === 'empty' ? <EmptyState title={t(metric === 'cost' ? '暂无费用' : '暂无请求')} minHeight={130} />
          : <div className="ur-composition-body">
            <svg viewBox="0 0 120 120" className="ur-composition-ring" role="img" aria-label={title}>
              <title>{title}</title>
              <desc>{data.items.map(item => `${item.model ?? t('其他模型')}: ${format(item.value)}`).join('; ')}</desc>
              <g transform="rotate(-90 60 60)">{data.items.map(item => {
                const start = offset; offset += item.share;
                return <circle key={item.key} cx="60" cy="60" r="46" fill="none" stroke={item.color} strokeWidth="13"
                  pathLength="1" strokeDasharray={`${item.share} ${1 - item.share}`} strokeDashoffset={-start} />;
              })}</g>
              <text x="60" y="58" textAnchor="middle" className="ur-ring-number">{data.items.length > 5 ? '5+' : data.items.length}</text>
              <text x="60" y="75" textAnchor="middle" className="ur-ring-label">{t('模型')}</text>
            </svg>
            <ul className="ur-composition-legend">{data.items.map(item => <li key={item.key}>
              <button type="button" disabled={item.model === null} aria-pressed={item.model !== null && selected === item.model}
                onClick={() => item.model !== null && onSelect(item.model)} className="ur-composition-entry">
                <span className="ur-model-dot" style={{ background: item.color }} />
                <span className="ur-model-label">{item.model ?? t('其他模型')}</span>
                <span className="ur-model-amount">{format(item.value)}</span><small>{(item.share * 100).toLocaleString(locale, { maximumFractionDigits: 1 })}%</small>
              </button>
            </li>)}</ul>
          </div>}
      </>}
    {data.state !== 'unavailable' && <p className="ur-composition-note">{t(metric === 'cost' ? '逐模型聚合 · CNY' : '点击模型查看详情；趋势保持全部模型范围。')}</p>}
  </Surface>;
}
