import { useEffect, useState, type ComponentProps } from 'react';
import { Form } from '@douyinfe/semi-ui';
import { useI18n } from '../../i18n';

type FormInputProps = ComponentProps<typeof Form.Input>;

export const MAX_EXACT_TOKEN_COUNT = 2_147_483_647;

export type ExactTokenCountParseResult =
  | { ok: true; value: number; normalized: string; raw: string }
  | { ok: false; reason: 'invalid_format' | 'out_of_range' };

export function formatExactTokenCount(value: number): string {
  return String(value).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
}

/**
 * Parses the context-length UI shorthand. Leading/trailing whitespace is
 * ignored and K/M are case-insensitive. Internal whitespace and every other
 * suffix or numeric notation are rejected.
 */
export function parseExactTokenCount(input: unknown): ExactTokenCountParseResult {
  const raw = typeof input === 'number' && Number.isSafeInteger(input)
    ? String(input)
    : String(input ?? '').trim();
  if (raw === '') return { ok: true, value: 0, normalized: '未声明', raw };

  const match = /^([0-9]+)([kKmM]?)$/.exec(raw);
  if (!match) return { ok: false, reason: 'invalid_format' };

  const multiplier = match[2].toLowerCase() === 'k'
    ? 1_000n
    : match[2].toLowerCase() === 'm'
      ? 1_000_000n
      : 1n;
  const exact = BigInt(match[1]) * multiplier;
  if (exact > BigInt(MAX_EXACT_TOKEN_COUNT)) {
    return { ok: false, reason: 'out_of_range' };
  }

  const value = Number(exact);
  return { ok: true, value, normalized: `${formatExactTokenCount(value)} tokens`, raw };
}

export type ExactTokenCountInputProps = Omit<
  FormInputProps,
  'extraText' | 'field' | 'initValue' | 'label' | 'onChange' | 'rules' | 'validator'
> & {
  field: string;
  initialValue?: number;
  label?: string;
  resetKey?: unknown;
};

export default function ExactTokenCountInput({
  field,
  initialValue = 0,
  label,
  resetKey,
  ...inputProps
}: ExactTokenCountInputProps) {
  const { t } = useI18n();
  const [raw, setRaw] = useState(initialValue > 0 ? String(initialValue) : '');
  const parsed = parseExactTokenCount(raw);
  const normalized = parsed.ok
    ? parsed.value === 0
      ? t('规范化：0 tokens（未声明 / 未知）')
      : t('规范化：{raw} → {normalized}', { raw: parsed.raw, normalized: parsed.normalized })
    : t('支持精确整数、整数 K 或 M；K=1000，M=1000000。');

  useEffect(() => {
    setRaw(initialValue > 0 ? String(initialValue) : '');
  }, [initialValue, resetKey]);

  return (
    <Form.Input
      {...inputProps}
      field={field}
      initValue={initialValue > 0 ? initialValue : ''}
      label={label ?? t('模型上下文窗口上限（Token，可选）')}
      placeholder="128K / 1M / 131072"
      validator={(fieldValue) => parseExactTokenCount(fieldValue).ok
        ? ''
        : t('请输入 0..2147483647 的整数 tokens，或使用十进制整数 K/M')}
      extraText={t('{normalized}；留空表示未知，不影响端点检测或路由。该字段用于目录说明和客户端规划，不替代供应商实际限制。首尾空白会被忽略，K/M 不区分大小写。', { normalized })}
      onChange={(value) => setRaw(String(value ?? ''))}
    />
  );
}
