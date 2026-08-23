import { Banner, Button, Input, Select, Space, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import {
  emptyPriceOverride,
  requestPriceMeters,
  requestUnitPriceMeters,
  type PriceBandOverrideDraft,
} from '../utils/rateCardPriceBands';

export default function RateCardPriceBandsEditor({
  ariaPrefix,
  overrides,
  enabledUnitMeters = [],
  onChange,
}: {
  ariaPrefix: string;
  overrides: PriceBandOverrideDraft[];
  enabledUnitMeters?: string[];
  onChange: (next: PriceBandOverrideDraft[]) => void;
}) {
  const update = (index: number, key: keyof PriceBandOverrideDraft, value: string | number[]) => onChange(
    overrides.map((item, itemIndex) => itemIndex === index ? { ...item, [key]: value } : item),
  );
  return <Space vertical align="start" style={{ width: '100%' }}>
    <Banner
      type="info"
      description="上方四类 Token 价格及已启用的单位价格是默认值；这里只填写与默认价不同的 UTC 时段。系统会自动补齐其余时间，并拒绝重叠、跨午夜或缺项。"
    />
    {overrides.map((override, index) => <div key={`${ariaPrefix}-${index}`} style={{ width: '100%', padding: 12, border: '1px solid var(--semi-color-border)', borderRadius: 8 }}>
      <Space vertical align="start" style={{ width: '100%' }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }}>
          <Typography.Text strong>特殊价段 {index + 1}</Typography.Text>
          <Button aria-label={`${ariaPrefix}删除特殊价段${index + 1}`} icon={<IconDelete />} type="danger" theme="borderless" onClick={() => onChange(overrides.filter((_, itemIndex) => itemIndex !== index))}>删除</Button>
        </Space>
        <Space style={{ width: '100%' }}>
          <Input aria-label={`${ariaPrefix}特殊价段${index + 1}开始`} type="time" value={override.start_utc} onChange={value => update(index, 'start_utc', value)} />
          <Input aria-label={`${ariaPrefix}特殊价段${index + 1}结束`} type="time" value={override.end_utc} onChange={value => update(index, 'end_utc', value)} />
        </Space>
        <Select
          aria-label={`${ariaPrefix}特殊价段${index + 1}适用 UTC 星期`}
          multiple
          value={override.weekdays_utc || []}
          onChange={value => update(index, 'weekdays_utc', Array.isArray(value) ? value.map(Number) : [])}
          optionList={[
            { value: 1, label: '周一' }, { value: 2, label: '周二' }, { value: 3, label: '周三' },
            { value: 4, label: '周四' }, { value: 5, label: '周五' }, { value: 6, label: '周六' },
            { value: 0, label: '周日' },
          ]}
          placeholder="留空表示每天"
          style={{ width: '100%' }}
        />
        {requestPriceMeters.map(item => <Input
          key={`${ariaPrefix}-${index}-${item.key}`}
          aria-label={`${ariaPrefix}特殊价段${index + 1}${item.label}每百万价格`}
          value={override[item.key]}
          onChange={value => update(index, item.key, value)}
          placeholder={`${item.label}：CNY / 1,000,000 tokens`}
        />)}
        {requestUnitPriceMeters.filter(item => enabledUnitMeters.includes(item.meter)).map(item => <Input
          key={`${ariaPrefix}-${index}-${item.key}`}
          aria-label={`${ariaPrefix}特殊价段${index + 1}${item.label}每次价格`}
          value={override[item.key] || ''}
          onChange={value => update(index, item.key, value)}
          placeholder={`${item.label}：CNY / ${item.unitLabel}`}
        />)}
      </Space>
    </div>)}
    <Space wrap>
      <Button icon={<IconPlus />} onClick={() => onChange([...overrides, emptyPriceOverride()])}>添加特殊价段</Button>
      <Button onClick={() => onChange([
        { ...emptyPriceOverride('01:00', '04:00'), weekdays_utc: [1, 2, 3, 4, 5] },
        { ...emptyPriceOverride('06:00', '10:00'), weekdays_utc: [1, 2, 3, 4, 5] },
      ])}>载入 DeepSeek 文档示例时段（UTC 工作日）</Button>
    </Space>
    <Typography.Text type="tertiary">
      UTC 星期留空表示每天。内置模板记录的 DeepSeek 峰时段为周一至周五北京时间 09:00–12:00、14:00–18:00，即 UTC 01:00–04:00、06:00–10:00；按钮只载入示例时段，不代表实时官方价格或当前规则。提交前必须核对来源页面并填写来源证据。缓存命中由供应商回传的 token 字段判断，UniRouter 不根据延迟或文本猜测。
    </Typography.Text>
  </Space>;
}
