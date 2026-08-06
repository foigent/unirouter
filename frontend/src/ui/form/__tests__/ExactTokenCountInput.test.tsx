import { Button, Form } from '@douyinfe/semi-ui';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import ExactTokenCountInput, { MAX_EXACT_TOKEN_COUNT, parseExactTokenCount } from '../ExactTokenCountInput';

describe('parseExactTokenCount', () => {
  it.each([
    ['128K', 128_000],
    ['128k', 128_000],
    ['1M', 1_000_000],
    ['1m', 1_000_000],
    [' 1M ', 1_000_000],
    ['131072', 131_072],
    ['0', 0],
    [String(MAX_EXACT_TOKEN_COUNT), MAX_EXACT_TOKEN_COUNT],
  ])('parses %s as exact decimal-SI tokens', (input, expected) => {
    expect(parseExactTokenCount(input)).toMatchObject({ ok: true, value: expected });
  });

  it.each([
    '',
    '2147483648',
    '1.5',
    '1.5K',
    '1.5M',
    '128Ki',
    '1Mi',
    '1e3',
    '128,000',
    '-1',
    '+1',
    '128 K',
  ])('rejects ambiguous or out-of-range input %s', (input) => {
    expect(parseExactTokenCount(input).ok).toBe(false);
  });

  it('keeps decimal K distinct from a direct binary-style integer', () => {
    expect(parseExactTokenCount('128K')).toMatchObject({ ok: true, value: 128_000 });
    expect(parseExactTokenCount('131072')).toMatchObject({ ok: true, value: 131_072 });
  });
});

describe('ExactTokenCountInput', () => {
  it('shows the exact normalized token value before submit', async () => {
    const user = userEvent.setup();
    render(
      <Form>
        <ExactTokenCountInput field="context_length" />
      </Form>,
    );

    const input = screen.getByPlaceholderText('128K / 1M / 131072');
    await user.clear(input);
    await user.type(input, '128K');

    expect(screen.getByText(/规范化：128K → 128,000 tokens/)).toBeTruthy();
  });

  it('blocks an invalid shorthand at the form boundary', async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn();
    render(
      <Form onSubmit={onSubmit}>
        <ExactTokenCountInput field="context_length" />
        <Button htmlType="submit">保存</Button>
      </Form>,
    );

    const input = screen.getByPlaceholderText('128K / 1M / 131072');
    await user.clear(input);
    await user.type(input, '1.5K');
    await user.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() => expect(screen.getByText(/请输入 0\.\.2147483647/)).toBeTruthy());
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
