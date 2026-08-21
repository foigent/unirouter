import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const sourceRoot = join(root, 'src');
const clientPath = join(sourceRoot, 'api', 'client.ts');
const transportPath = join(sourceRoot, 'api', 'transport.ts');
const routingPath = join(sourceRoot, 'api', 'routing.ts');
const channelsPath = join(sourceRoot, 'api', 'channels.ts');
const modelEndpointsPath = join(sourceRoot, 'api', 'modelEndpoints.ts');
const retiredChannelKeysPagePath = join(sourceRoot, 'pages', 'ChannelKeysPage.tsx');
const dormantConnectionPanelPath = join(sourceRoot, 'components', 'ConnectionModelPanel.tsx');
const maxLegacyRequests = 152;
const retiredRuntimeWriterNames = [
  'create',
  'validate',
  'activate',
  'shadow',
  'canary',
  'promote',
  'rollback',
  'reject',
  'createRelease',
  'validateRelease',
  'activateInitialRelease',
  'markReleaseShadow',
  'startReleaseCanary',
  'promoteRelease',
  'rollbackRelease',
  'rejectRelease',
];
const retiredChannelWriterNames = ['create', 'update', 'remove'];
const retiredCredentialWriterNames = ['create', 'update', 'rotate', 'remove'];
const retiredModelEndpointWriterNames = [
  'testChannelModel',
  'activateModelEndpoint',
  'startModelEndpointCanary',
  'recoverModelEndpoint',
];

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
const routing = readFileSync(routingPath, 'utf8');
const channels = readFileSync(channelsPath, 'utf8');
const modelEndpoints = readFileSync(modelEndpointsPath, 'utf8');
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
  const retiredWriterCall = retiredRuntimeWriterNames.find(name => new RegExp(`\\broutingApi\\.${name}\\s*\\(`).test(source));
  if (retiredWriterCall) {
    failures.push(`${relative(root, path)} calls retired routingApi.${retiredWriterCall}`);
  }
  const retiredChannelWriterCall = retiredChannelWriterNames.find(name => new RegExp(`\\bchannelApi\\.${name}\\s*\\(`).test(source));
  if (retiredChannelWriterCall) {
    failures.push(`${relative(root, path)} calls retired channelApi.${retiredChannelWriterCall}`);
  }
  const retiredCredentialWriterCall = retiredCredentialWriterNames.find(name => new RegExp(`\\bproviderCredentialApi\\.${name}\\s*\\(`).test(source));
  if (retiredCredentialWriterCall) {
    failures.push(`${relative(root, path)} calls retired providerCredentialApi.${retiredCredentialWriterCall}`);
  }
  const retiredModelEndpointWriterCall = retiredModelEndpointWriterNames.find(name => new RegExp(`\\bmodelEndpointApi\\.${name}\\s*\\(`).test(source));
  if (retiredModelEndpointWriterCall) {
    failures.push(`${relative(root, path)} calls retired modelEndpointApi.${retiredModelEndpointWriterCall}`);
  }
  if (/\bRuntimeChangeControlWorkspace\b/.test(source)) {
    failures.push(`${relative(root, path)} references the retired RuntimeChangeControlWorkspace`);
  }
  if (path !== dormantConnectionPanelPath && /\bConnectionModelPanel\b/.test(source)) {
    failures.push(`${relative(root, path)} revives the dormant legacy connection model panel`);
  }
}

if (existsSync(retiredChannelKeysPagePath)) {
  failures.push('pages/ChannelKeysPage.tsx revives the retired credential mutation workspace');
}

const retiredRoutingExport = retiredRuntimeWriterNames.find(name => new RegExp(`\\b${name}\\s*:`).test(routing));
if (retiredRoutingExport || /\/runtime-changes(?:\/|['"`])/.test(routing)) {
  failures.push('api/routing.ts exposes the retired runtime-change writer surface');
}

const retiredChannelExport = retiredChannelWriterNames.find(name => new RegExp(`\\b${name}\\s*:`).test(channels));
const retiredCredentialExport = retiredCredentialWriterNames.find(name => new RegExp(`\\b${name}\\s*:`).test(channels));
if (retiredChannelExport || retiredCredentialExport) {
  failures.push('api/channels.ts exposes a retired channel or credential writer');
}

const retiredModelEndpointExport = retiredModelEndpointWriterNames.find(name => new RegExp(`\\b${name}\\s*:`).test(modelEndpoints));
if (retiredModelEndpointExport) {
  failures.push(`api/modelEndpoints.ts exposes retired modelEndpointApi.${retiredModelEndpointExport}`);
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
