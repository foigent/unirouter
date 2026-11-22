import { afterEach, expect, it, vi } from 'vitest';
import { copyText } from '../clipboard';

const originalExecCommand = Object.getOwnPropertyDescriptor(document, 'execCommand');

function mockLegacyCopy(callback: () => boolean) {
  const command = vi.fn(callback);
  Object.defineProperty(document, 'execCommand', { configurable: true, value: command });
  return command;
}

afterEach(() => {
  vi.unstubAllGlobals();
  if (originalExecCommand) {
    Object.defineProperty(document, 'execCommand', originalExecCommand);
  } else {
    Reflect.deleteProperty(document, 'execCommand');
  }
});

it('passes the exact text to the secure clipboard', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal('isSecureContext', true);
  vi.stubGlobal('navigator', { clipboard: { writeText } });

  await copyText('  request-123  ');
  expect(writeText).toHaveBeenCalledWith('  request-123  ');
});

it('propagates secure clipboard denial without invoking the fallback', async () => {
  const denied = new Error('Clipboard access denied');
  vi.stubGlobal('isSecureContext', true);
  vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(denied) } });
  const command = mockLegacyCopy(() => true);

  await expect(copyText('request-123')).rejects.toBe(denied);
  expect(command).not.toHaveBeenCalled();
});

it('copies from a temporary textarea when the secure clipboard is unavailable', async () => {
  vi.stubGlobal('isSecureContext', false);
  const command = mockLegacyCopy(() => {
    expect(document.querySelector('textarea')).toHaveValue('request-123');
    return true;
  });

  await copyText('request-123');
  expect(command).toHaveBeenCalledWith('copy');
  expect(document.querySelector('textarea')).toBeNull();
});

it('removes the temporary textarea when the fallback throws', async () => {
  vi.stubGlobal('isSecureContext', false);
  const denied = new Error('Copy unavailable');
  mockLegacyCopy(() => { throw denied; });

  await expect(copyText('request-123')).rejects.toBe(denied);
  expect(document.querySelector('textarea')).toBeNull();
});
