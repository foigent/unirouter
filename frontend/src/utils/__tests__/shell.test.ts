import { describe, expect, it } from 'vitest';
import { buildChatCompletionsCurl, quotePosixArg } from '../shell';

describe('shell guidance encoding', () => {
  it('quotes a dynamic value as one POSIX shell argument', () => {
    expect(quotePosixArg(`alpha'beta`)).toBe(`'alpha'"'"'beta'`);
  });

  it('serializes model data as JSON before quoting the complete request body', () => {
    const model = `model ' " \\ line\nnext`;
    const body = JSON.stringify({
      model,
      messages: [{ role: 'user', content: 'Hello' }],
    });
    const command = buildChatCompletionsCurl('https://router.example/base path', `key'fragment`, model);

    expect(command).toContain(`curl ${quotePosixArg('https://router.example/base path/v1/chat/completions')}`);
    expect(command).toContain(`-H ${quotePosixArg(`Authorization: Bearer key'fragment`)}`);
    expect(command).toContain(`--data ${quotePosixArg(body)}`);
    expect(JSON.parse(body)).toEqual({ model, messages: [{ role: 'user', content: 'Hello' }] });
  });
});
