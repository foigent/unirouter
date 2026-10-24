// Evidence only. This plugin observes the new build; it does not transform it.
import { createHash } from 'node:crypto';
import {
  existsSync, lstatSync, mkdirSync, readFileSync, readdirSync,
  realpathSync, writeFileSync,
} from 'node:fs';
import { basename, isAbsolute, join, relative, resolve } from 'node:path';

const sourceCommit = '21572d9e73ca901583a8b526c9af7cd0319d59f1';
const sha = (body) => createHash('sha256').update(body).digest('hex');
const requireFact = (value, reason) => { if (!value) throw new Error(reason); };
const maximum = { modules: 50000, watchFiles: 50000, packages: 2048,
  distFiles: 4096, distBytes: 64*1024**2, jsonBytes: 16*1024**2,
  inputBytes: 256*1024**2, edges: 200000,
  noticeFiles: 8192, noticeOne: 1024**2, noticeTotal: 8*1024**2 };

export function membershipObserver() {
  let config;
  let calls = 0;
  return {
    name: 'ur-source-evidence-observer',
    apply: 'build', enforce: 'post',
    configResolved(value) {
      config = value;
      requireFact(resolve(value.root) === '/app' && !value.build.watch,
        'fixed non-watch build root required');
      requireFact(resolve(value.root, value.build.outDir) === '/app/dist',
        'fixed dist directory required');
    },
    writeBundle: {
      order: 'post', sequential: true,
      handler(options, bundle) {
        requireFact(++calls === 1 && options.dir && resolve(options.dir) === '/app/dist',
          'one production output only');
        const root = realpathSync(config.root);
        const output = '/app/.ur-source-evidence/output';
        requireFact(!existsSync(output), 'preserve existing evidence output');
        mkdirSync(output, { mode: 0o700 });
        mkdirSync(join(output, 'npm-notices'), { mode: 0o700 });
        const norm = (id) => {
          requireFact(typeof id === 'string' && id.length <= 8192, 'bounded module id required');
          if (id.startsWith(root+'/')) return id.slice(root.length+1);
          requireFact(!isAbsolute(id), 'external absolute module id is outside source scope');
          return id; // Preserve virtual identifiers as data; never turn them into paths.
        };
        const physical = (id) => {
          if (!id.startsWith(root+'/') || id.includes('\0')) return null;
          const path = id.split('?')[0];
          if (!existsSync(path) || !lstatSync(path).isFile()) return null;
          const actual = realpathSync(path);
          requireFact(actual.startsWith(root+'/'), 'input escapes source root');
          return actual;
        };
        const bounded = (path, cap) => {
          const info = lstatSync(path);
          requireFact(info.isFile() && !info.isSymbolicLink() && info.size <= cap,
            'bounded regular input required');
          return readFileSync(path);
        };
        const originalPackageBody = bounded(join(root, '.ur-source-evidence/original-package.json'), 1024**2);
        const lockBody = bounded(join(root, '.ur-source-evidence/original-package-lock.json'), 1024**2);
        requireFact(sha(lockBody) === '20becd6a150cf57a8d5925a507f0600528a27d028df92d9a878351453810c0d1'
          && sha(originalPackageBody) === '7f5fdf0a6c3882f913263546c3940b8f19514a198e1fba5e488be5b1648724ea',
          'original source input identity mismatch');
        const postInstallLockBody = bounded(join(root, 'package-lock.json'), 1024**2);
        const postInstallPackageBody = bounded(join(root, 'package.json'), 1024**2);
        const lock = JSON.parse(lockBody);
        const hiddenPath = join(root, 'node_modules/.package-lock.json');
        const hiddenBody = bounded(hiddenPath, 1024**2);
        const hidden = JSON.parse(hiddenBody);
        const packages = new Map();
        let edgeCount = 0;
        const packageFor = (id, reason) => {
          const path = physical(id);
          if (!path) return null;
          const parts = relative(root, path).split('/');
          const index = parts.lastIndexOf('node_modules');
          if (index < 0 || !parts[index+1]) return null;
          const count = parts[index+1].startsWith('@') ? 3 : 2;
          const location = parts.slice(0, index+count).join('/');
          let record = packages.get(location);
          if (!record) {
            requireFact(packages.size < maximum.packages, 'package count limit');
            const metadata = JSON.parse(bounded(join(root, location, 'package.json'), 1024**2));
            const sourceRow = lock.packages?.[location];
            const installedRow = hidden.packages?.[location];
            record = { location,
              name: typeof metadata.name === 'string' ? metadata.name : null,
              version: typeof metadata.version === 'string' ? metadata.version : null,
              declared_license: typeof metadata.license === 'string' ? metadata.license : null,
              license_metadata_kind: metadata.license == null ? 'missing'
                : typeof metadata.license === 'string' ? 'string' : 'unsupported',
              unsupported_license_sha256: metadata.license != null && typeof metadata.license !== 'string'
                ? sha(JSON.stringify(metadata.license)) : null,
              source_lock_version: typeof sourceRow?.version === 'string' ? sourceRow.version : null,
              installed_lock_version: typeof installedRow?.version === 'string' ? installedRow.version : null,
              exact_lock_version_match: typeof metadata.version === 'string' && metadata.version.length > 0
                && metadata.version === sourceRow?.version && metadata.version === installedRow?.version,
              reasons: [], notices: [] };
            packages.set(location, record);
          }
          if (!record.reasons.includes(reason)) record.reasons.push(reason);
          return location;
        };
        const ids = [...this.getModuleIds()].sort();
        const watches = [...this.getWatchFiles()].sort();
        requireFact(ids.length <= maximum.modules && watches.length <= maximum.watchFiles,
          'graph/input count limit');
        const graph = ids.map((id) => {
          const info = this.getModuleInfo(id);
          edgeCount += (info?.importedIds.length ?? 0) + (info?.dynamicallyImportedIds.length ?? 0);
          requireFact(edgeCount <= maximum.edges, 'graph edge count limit');
          return { id: norm(id), package: packageFor(id, 'rollup_graph'),
            info_present: !!info, included: info?.isIncluded ?? null,
            external: info?.isExternal ?? null,
            imports: (info?.importedIds ?? []).map(norm),
            dynamic_imports: (info?.dynamicallyImportedIds ?? []).map(norm) };
        });
        let inputBytes = 0;
        const watchInputs = watches.map((id) => {
          const path = physical(id);
          const body = path ? bounded(path, 16*1024**2) : null;
          inputBytes += body?.length ?? 0;
          requireFact(inputBytes <= maximum.inputBytes, 'watched input byte limit');
          return { id: norm(id), package: packageFor(id, 'watch_input'),
            sha256: body ? sha(body) : null };
        });
        const emitted = Object.values(bundle).map((item) => item.type === 'chunk' ? {
          type: 'chunk', file: item.fileName, imports: item.imports,
          dynamic_imports: item.dynamicImports, referenced_files: item.referencedFiles,
          modules: Object.entries(item.modules).map(([id, module]) => ({
            id: norm(id), package: packageFor(id, 'emitted_chunk'),
            rendered_length: module.renderedLength, original_length: module.originalLength,
            rendered_exports_count: module.renderedExports.length,
            removed_exports_count: module.removedExports.length,
          })),
        } : { type: 'asset', file: item.fileName,
          original_files: item.originalFileNames.map(norm) });
        for (const tool of ['vite', 'rollup', '@vitejs/plugin-react']) {
          packageFor(join(root, 'node_modules', tool, 'package.json'), 'build_tool_notice_candidate');
        }
        let noticeBytes = 0;
        let noticeCount = 0;
        for (const record of [...packages.values()].sort((a,b) => a.location.localeCompare(b.location))) {
          for (const name of readdirSync(join(root, record.location)).sort()) {
            if (!/^(?:licen[cs]e|copying|notice|copyright)(?:[._-][a-z0-9_-]+)?(?:\.(?:txt|md|markdown|rst))?$/i.test(name)) continue;
            if (/\.(?:js|ts|tsx|jsx|json|map)$/i.test(name)) continue;
            const path = join(root, record.location, name);
            if (!lstatSync(path).isFile() || lstatSync(path).isSymbolicLink()) continue;
            const body = bounded(path, maximum.noticeOne);
            noticeBytes += body.length;
            requireFact(++noticeCount <= maximum.noticeFiles && noticeBytes <= maximum.noticeTotal,
              'notice count/byte limit');
            const file = `npm-notices/${String(noticeCount).padStart(4,'0')}-${basename(name)}`;
            writeFileSync(join(output, file), body, { flag: 'wx', mode: 0o600 });
            record.notices.push({ file, source: record.location+'/'+name,
              bytes: body.length, sha256: sha(body) });
          }
        }
        const walk = (directory, capFiles, capBytes) => {
          const rows = [];
          let bytes = 0;
          const visit = (path) => {
            for (const name of readdirSync(path).sort()) {
              const file = join(path, name);
              const info = lstatSync(file);
              requireFact(!info.isSymbolicLink(), 'asset link requires separate disposition');
              if (info.isDirectory()) visit(file);
              else {
                requireFact(info.isFile(), 'nonregular asset');
                bytes += info.size;
                requireFact(rows.length < capFiles && bytes <= capBytes, 'asset count/byte limit');
                rows.push({ file: relative(directory, file), bytes: info.size,
                  sha256: sha(readFileSync(file)) });
              }
            }
          };
          visit(directory);
          return rows;
        };
        const dist = walk('/app/dist', maximum.distFiles, maximum.distBytes);
        requireFact(!config.publicDir || resolve(config.publicDir).startsWith(root+'/'),
          'public source directory escapes root');
        const publicFiles = config.publicDir && existsSync(config.publicDir)
          ? walk(config.publicDir, maximum.distFiles, maximum.distBytes) : [];
        const emittedNames = new Set(emitted.map((row) => row.file));
        const publicMatches = publicFiles.filter((row) => dist.some((out) =>
          row.file === out.file && row.sha256 === out.sha256 && row.bytes === out.bytes));
        const publicNames = new Set(publicMatches.map((row) => row.file));
        const result = {
          status: 'actual_build_graph_and_output_observations_not_license_clearance',
          source_commit: sourceCommit,
          actual_vite_version: JSON.parse(readFileSync('/app/node_modules/vite/package.json')).version,
          actual_rollup_version: JSON.parse(readFileSync('/app/node_modules/rollup/package.json')).version,
          original_source_lock_sha256: sha(lockBody), original_package_json_sha256: sha(originalPackageBody),
          post_install_package_lock_sha256: sha(postInstallLockBody),
          post_install_package_json_sha256: sha(postInstallPackageBody),
          installed_hidden_lock_sha256: sha(hiddenBody),
          plugin_order: config.plugins.map((plugin) => plugin.name),
          graph, watch_inputs: watchInputs, emitted, output_files: dist,
          public_copies: publicMatches,
          unexplained_output_files: dist.filter((row) => !emittedNames.has(row.file) && !publicNames.has(row.file)),
          packages: [...packages.values()],
          limits: maximum,
          limits_of_claim: ['A watched or parsed file is not necessarily emitted code.',
            'Virtual modules, transformed CSS/assets and package notice completeness require disposition.',
            'This is the newly observed build, not proof of old image bytes or of a future build.'],
        };
        const body = Buffer.from(JSON.stringify(result, null, 2)+'\n');
        requireFact(body.length <= maximum.jsonBytes, 'membership report byte limit');
        writeFileSync(join(output, 'frontend-membership.json'), body, { flag: 'wx', mode: 0o600 });
      },
    },
  };
}
