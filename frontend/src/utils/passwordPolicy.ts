export const LOCAL_PASSWORD_MIN_CODE_POINTS = 12;
export const LOCAL_PASSWORD_MAX_UTF8_BYTES = 72;

export function localPasswordCodePointCount(value: string) {
  return Array.from(value).length;
}

export function localPasswordUTF8ByteCount(value: string) {
  return new TextEncoder().encode(value).length;
}
