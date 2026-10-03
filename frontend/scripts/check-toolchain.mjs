import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';

const root = new URL('../', import.meta.url);
const pkg = JSON.parse(readFileSync(new URL('package.json', root), 'utf8'));
const npm = process.env.npm_execpath
  ? execFileSync(process.execPath, [process.env.npm_execpath, '--version'], { encoding: 'utf8' }).trim()
  : execFileSync('npm', ['--version'], { encoding: 'utf8' }).trim();
const docker = readFileSync(new URL('Dockerfile', root), 'utf8');
if (process.versions.node !== pkg.engines.node || npm !== pkg.engines.npm ||
    pkg.packageManager !== `npm@${pkg.engines.npm}` ||
    !docker.includes(`FROM node:${pkg.engines.node}-alpine3.23@sha256:`)) {
  throw new Error(`Frontend toolchain differs: expected Node ${pkg.engines.node} / npm ${pkg.engines.npm}, got ${process.versions.node} / ${npm}`);
}
console.log(`Frontend toolchain verified: Node ${process.versions.node} / npm ${npm}`);
