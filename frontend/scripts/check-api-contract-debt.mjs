import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const sourceRoot = join(root, 'src');
const clientPath = join(sourceRoot, 'api', 'client.ts');
const transportPath = join(sourceRoot, 'api', 'transport.ts');
const maxLegacyRequests = 152;

function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return entry.name === '__tests__' ? [] : sourceFiles(path);
    return statSync(path).isFile() && /\.(?:ts|tsx)$/.test(entry.name) ? [path] : [];
  });
}

const failures = [];
const transport = readFileSync(transportPath, 'utf8');
const client = readFileSync(clientPath, 'utf8');
const apiSources = sourceFiles(join(sourceRoot, 'api'));
const productSources = sourceFiles(sourceRoot);

if (/request\s*<\s*T\s*=/.test(transport)) {
  failures.push('transport.request must require an explicit response contract');
}

for (const path of productSources) {
  const source = readFileSync(path, 'utf8');
  if (/\bapi\.request\s*\(/.test(source)) {
    failures.push(`${relative(root, path)} exposes or consumes the aggregate generic request escape hatch`);
  }
}

for (const path of apiSources) {
  const source = readFileSync(path, 'utf8');
  const withoutCompatibilityMarker = source.replace('type LegacyApiResponse = any;', '');
  if (/\bany\b/.test(withoutCompatibilityMarker)) {
    failures.push(`${relative(root, path)} contains an untracked any API contract`);
  }
}

const compatibilityMarkers = client.match(/type LegacyApiResponse = any;/g)?.length || 0;
if (compatibilityMarkers !== 1) {
  failures.push(`aggregate client must contain exactly one explicit compatibility marker; found ${compatibilityMarkers}`);
}

const legacyRequests = client.match(/\blegacyRequest\s*\(/g)?.length || 0;
if (legacyRequests > maxLegacyRequests) {
  failures.push(`legacy aggregate request count grew from ${maxLegacyRequests} to ${legacyRequests}`);
}

if (failures.length) {
  failures.forEach(failure => console.error(`API contract guard: ${failure}`));
  process.exit(1);
}

console.log(`API contract guard: ${legacyRequests}/${maxLegacyRequests} legacy aggregate calls; typed domains cannot regress to any.`);
