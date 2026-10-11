// Runtime-only registry. Domain modules register synchronously as dependencies
// of their lazy page; this module must not import React or any domain catalog.
const english: Record<string, string> = Object.create(null);

export function registerMessages(messages: Readonly<Record<string, string>>) {
  const entries = Object.entries(messages);
  // Validate the complete batch before mutation. Order cannot choose a winner.
  for (const [key, value] of entries) {
    if (Object.prototype.hasOwnProperty.call(english, key) && english[key] !== value) {
      throw new Error(`Conflicting UI translation: ${key}`);
    }
  }
  for (const [key, value] of entries) english[key] = value;
}

export function englishMessage(key: string): string | undefined {
  return Object.prototype.hasOwnProperty.call(english, key) ? english[key] : undefined;
}
