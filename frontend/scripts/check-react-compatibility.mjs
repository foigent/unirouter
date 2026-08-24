import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const sourceRoot = path.join(frontendRoot, 'src');
const packageJSON = JSON.parse(fs.readFileSync(path.join(frontendRoot, 'package.json'), 'utf8'));
const packageLock = JSON.parse(fs.readFileSync(path.join(frontendRoot, 'package-lock.json'), 'utf8'));
const mainSource = fs.readFileSync(path.join(sourceRoot, 'main.tsx'), 'utf8');
const failures = [];

function dependencyMajor(specifier) {
  const match = String(specifier || '').match(/\d+/);
  return match ? Number(match[0]) : Number.NaN;
}

function sourceFiles(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const absolute = path.join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(absolute);
    return /\.(?:ts|tsx)$/.test(entry.name) ? [absolute] : [];
  });
}

const reactMajor = dependencyMajor(packageJSON.dependencies?.react);
const reactDOMMajor = dependencyMajor(packageJSON.dependencies?.['react-dom']);
if (!Number.isInteger(reactMajor) || reactMajor !== reactDOMMajor) {
  failures.push('react and react-dom must declare the same parseable major version');
}
if (reactMajor >= 19) {
  failures.push(
    'React 19 upgrade is blocked while FE-REACT18-SEMI-FINDDOMNODE-001 is active; clear the runtime warning and retire the waiver first',
  );
}

const ownedFindDOMNode = sourceFiles(sourceRoot).filter((filename) =>
  fs.readFileSync(filename, 'utf8').includes('findDOMNode'),
);
if (ownedFindDOMNode.length > 0) {
  failures.push(`UniRouter-owned source must not call findDOMNode: ${ownedFindDOMNode.map((filename) => path.relative(frontendRoot, filename)).join(', ')}`);
}

if (!mainSource.includes('<React.StrictMode>')) {
  failures.push('React.StrictMode must remain enabled');
}
if (!mainSource.includes('<BrowserRouter future={ROUTER_FUTURE_FLAGS}>')) {
  failures.push('BrowserRouter must use the reviewed React Router future contract');
}

const semiVersion = packageLock.packages?.['node_modules/@douyinfe/semi-ui']?.version;
if (semiVersion !== '2.100.0') {
  failures.push(
    `FE-REACT18-SEMI-FINDDOMNODE-001 was verified only for @douyinfe/semi-ui 2.100.0; found ${semiVersion || 'unknown'}, so the waiver must be revalidated`,
  );
}

if (failures.length > 0) {
  failures.forEach((failure) => console.error(`React compatibility guard: ${failure}`));
  process.exit(1);
}

console.log(
  `React compatibility guard: React ${reactMajor}; v7 router flags enabled; StrictMode retained; owned findDOMNode calls 0; FE-REACT18-SEMI-FINDDOMNODE-001 active for Semi ${semiVersion}.`,
);
