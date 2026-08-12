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
