import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Form } from '@douyinfe/semi-ui';
import { describe, expect, it } from 'vitest';
import PasswordField from '../PasswordField';

function renderField(visibilityResetKey: string = 'initial') {
  return render(
    <Form>
      <PasswordField
        field="password"
        label="密码"
        placeholder="请输入密码"
        visibilityResetKey={visibilityResetKey}
      />
    </Form>,
  );
}

describe('PasswordField', () => {
  it('keeps passwords hidden by default and toggles visibility explicitly', async () => {
    const user = userEvent.setup();
    renderField();

    const input = screen.getByPlaceholderText('请输入密码');
    expect(input).toHaveAttribute('type', 'password');

    await user.click(screen.getByRole('button', { name: '显示密码' }));
    expect(input).toHaveAttribute('type', 'text');

    await user.click(screen.getByRole('button', { name: '隐藏密码' }));
    expect(input).toHaveAttribute('type', 'password');
  });

  it('resets to hidden when the reset key changes', async () => {
    const user = userEvent.setup();
    const { rerender } = renderField('first');

    const input = screen.getByPlaceholderText('请输入密码');
    await user.click(screen.getByRole('button', { name: '显示密码' }));
    expect(input).toHaveAttribute('type', 'text');

    rerender(
      <Form>
        <PasswordField
          field="password"
          label="密码"
          placeholder="请输入密码"
          visibilityResetKey="second"
        />
      </Form>,
    );

    await waitFor(() => expect(input).toHaveAttribute('type', 'password'));
  });
});
