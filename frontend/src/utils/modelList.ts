const MODEL_LIST_SEPARATOR = /[,，;；\r\n]+/;

export function parseModelList(value: unknown): string[] {
  const rawValues = Array.isArray(value) ? value : [value];
  const seen = new Set<string>();
  const models: string[] = [];

  for (const rawValue of rawValues) {
    for (const candidate of String(rawValue || '').split(MODEL_LIST_SEPARATOR)) {
      const model = candidate.trim();
      if (!model || seen.has(model)) continue;
      seen.add(model);
      models.push(model);
    }
  }

  return models;
}

export function normalizeModelList(value: unknown): string {
  return parseModelList(value).join(',');
}

function compareUTF8(left: string, right: string): number {
  const encoder = new TextEncoder();
  const leftBytes = encoder.encode(left);
  const rightBytes = encoder.encode(right);
  const length = Math.min(leftBytes.length, rightBytes.length);
  for (let index = 0; index < length; index += 1) {
    if (leftBytes[index] !== rightBytes[index]) return leftBytes[index] - rightBytes[index];
  }
  return leftBytes.length - rightBytes.length;
}

// The access service uses Go's sort.Strings before hashing the semantic
// operation. Keep client retry identity byte-for-byte aligned with that order.
export function parseSortedModelList(value: unknown): string[] {
  return parseModelList(value).sort(compareUTF8);
}
