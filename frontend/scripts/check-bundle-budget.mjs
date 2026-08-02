import { readdir, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const assetsDirectory = fileURLToPath(new URL('../dist/assets/', import.meta.url));
const maxJavaScriptBytes = 450 * 1024;
const entries = await readdir(assetsDirectory);
const javascript = [];

for (const name of entries) {
  if (!name.endsWith('.js')) continue;
  const metadata = await stat(join(assetsDirectory, name));
  javascript.push({ name, bytes: metadata.size });
}

if (javascript.length === 0) {
  throw new Error('bundle budget: no generated JavaScript assets found');
}

javascript.sort((left, right) => right.bytes - left.bytes || left.name.localeCompare(right.name));
const largest = javascript[0];
if (largest.bytes > maxJavaScriptBytes) {
  throw new Error(
    `bundle budget exceeded: ${largest.name} is ${largest.bytes} bytes; limit is ${maxJavaScriptBytes} bytes`,
  );
}

console.log(
  `bundle budget PASS: ${javascript.length} JavaScript assets; largest ${largest.name} ${largest.bytes} bytes; limit ${maxJavaScriptBytes} bytes`,
);
