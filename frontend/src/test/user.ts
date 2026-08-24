import userEvent from '@testing-library/user-event';

export async function pasteValue(
  user: ReturnType<typeof userEvent.setup>,
  input: Element,
  value: string,
) {
  await user.click(input);
  await user.paste(value);
}
