import { useEffect, useState, type ComponentProps } from 'react';
import { Form } from '@douyinfe/semi-ui';

type FormInputProps = ComponentProps<typeof Form.Input>;

export const MAX_EXACT_TOKEN_COUNT = 2_147_483_647;

export type ExactTokenCountParseResult =
  | { ok: true; value: number; normalized: string; raw: string }
  | { ok: false; reason: 'empty' | 'invalid_format' | 'out_of_range' };

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
  if (raw === '') return { ok: false, reason: 'empty' };

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
  label = '上下文长度（tokens）',
  resetKey,
  ...inputProps
}: ExactTokenCountInputProps) {
  const [raw, setRaw] = useState(String(initialValue));
  const parsed = parseExactTokenCount(raw);
  const normalized = parsed.ok
    ? parsed.value === 0
      ? '规范化：0 tokens（未声明 / 未知）'
      : `规范化：${parsed.raw} → ${parsed.normalized}`
    : '支持精确整数、整数 K 或 M；K=1000，M=1000000。';

  useEffect(() => {
    setRaw(String(initialValue));
  }, [initialValue, resetKey]);

  return (
    <Form.Input
      {...inputProps}
      field={field}
      initValue={initialValue}
      label={label}
      placeholder="128K / 1M / 131072"
      rules={[{ required: true, message: '请输入上下文长度；0 表示未声明 / 未知' }]}
      validator={(fieldValue) => parseExactTokenCount(fieldValue).ok
        ? ''
        : '请输入 0..2147483647 的整数 tokens，或使用十进制整数 K/M'}
      extraText={`${normalized}；首尾空白会被忽略，K/M 不区分大小写。`}
      onChange={(value) => setRaw(String(value ?? ''))}
    />
  );
}
