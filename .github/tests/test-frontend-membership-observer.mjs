// Offline tests of the actual immutable binding and original-input guards.
// Builtin fs calls are intercepted; no /app files, Docker or build is created.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import fs from 'node:fs';
import { syncBuiltinESMExports } from 'node:module';
import test from 'node:test';
import { membershipObserver } from './frontend-membership-observer.mjs';
const digest = (body) => createHash('sha256').update(body).digest('hex');

test('requires the exact typed data binding without defaults', () => {
  for (const binding of [undefined, {}, { source_commit: 'a'.repeat(40), package_sha256: 'b'.repeat(64), lock_sha256: 'c'.repeat(64), extra: true },
    { source_commit: 'unknown', package_sha256: 'b'.repeat(64), lock_sha256: 'c'.repeat(64) }]) {
    assert.throws(() => membershipObserver(binding), /exact observer input binding/);
  }
});

test('same actual observer enforces two current inputs and captures immutable binding values', () => {
  const names = ['existsSync', 'lstatSync', 'mkdirSync', 'readFileSync', 'realpathSync'];
  const originals = Object.fromEntries(names.map((name) => [name, fs[name]]));
  const reached = new Error('post-original-input-boundary');
  try {
    for (const [version, character] of [['1.15.0', 'a'], ['1.15.1', 'b']]) {
      const packageBody = Buffer.from(JSON.stringify({ version }));
      const lockBody = Buffer.from(JSON.stringify({ version, lockfileVersion: 3 }));
      for (const changed of [null, 'package', 'lock']) {
        const binding = { source_commit: character.repeat(40), package_sha256: digest(packageBody), lock_sha256: digest(lockBody) };
        const observer = membershipObserver(binding);
        binding.package_sha256 = '0'.repeat(64); binding.lock_sha256 = '0'.repeat(64);
        let crossed = false;
        fs.existsSync = () => false;
        fs.realpathSync = (path) => path;
        fs.mkdirSync = () => undefined;
        const bodies = {
          '/app/.ur-source-evidence/original-package.json': changed === 'package' ? Buffer.from('changed') : packageBody,
          '/app/.ur-source-evidence/original-package-lock.json': changed === 'lock' ? Buffer.from('changed') : lockBody,
        };
        fs.lstatSync = (path) => ({ isFile: () => true, isSymbolicLink: () => false, size: bodies[path]?.length ?? 0 });
        fs.readFileSync = (path) => { if (path in bodies) return bodies[path]; crossed = true; throw reached; };
        syncBuiltinESMExports();
        observer.configResolved({ root: '/app', build: { outDir: 'dist', watch: false } });
        assert.throws(() => observer.writeBundle.handler.call({}, { dir: '/app/dist' }, {}),
          changed ? /original source input identity mismatch/ : (error) => error === reached);
        assert.equal(crossed, changed === null);
      }
    }
  } finally {
    Object.assign(fs, originals); syncBuiltinESMExports();
  }
});
