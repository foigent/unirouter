export function quotePosixArg(value: string): string {
  return `'${value.replace(/'/g, `'"'"'`)}'`;
}

export function buildChatCompletionsCurl(baseUrl: string, apiKey: string, model: string): string {
  const requestBody = JSON.stringify({
    model,
    messages: [{ role: 'user', content: 'Hello' }],
  });
  return [
    `curl ${quotePosixArg(`${baseUrl}/v1/chat/completions`)}`,
    `  -H ${quotePosixArg(`Authorization: Bearer ${apiKey}`)}`,
    `  -H ${quotePosixArg('Content-Type: application/json')}`,
    `  --data ${quotePosixArg(requestBody)}`,
  ].join(' \\\n');
}
