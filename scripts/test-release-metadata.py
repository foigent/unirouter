#!/usr/bin/env python3
"""Exercise release metadata policy and the actual CI router in isolated Git repos."""

import json
import hashlib
import os
import re
from pathlib import Path
import shutil
import subprocess
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[1]
CHECKER = ROOT / 'scripts/check-release-metadata.sh'
WORKFLOW = ROOT / '.github/workflows/ci.yml'
GIT = shutil.which('git')


class ReleasePolicyTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='unirouter-release-policy-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / 'repo'
        self.repo.mkdir()
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith('GIT_') and k not in ('BASE_SHA', 'HEAD_SHA', 'BASH_ENV', 'ENV')}
        self.env.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_TERMINAL_PROMPT='0')
        self.git('init', '-q')
        self.git('config', 'user.name', 'Release policy fixture')
        self.git('config', 'user.email', 'fixture@invalid')
        self.git('config', 'commit.gpgsign', 'false')
        self.git('config', 'core.hooksPath', str(self.root / 'no-hooks'))
        self.write_metadata('1.2.3')
        self.write_toolchains()
        self.write('internal/runtime.go', 'package runtime\n')
        self.base = self.commit()

    def git(self, *args):
        return subprocess.run([GIT, *args], cwd=self.repo, env=self.env,
                              text=True, capture_output=True, check=True).stdout.strip()

    def write(self, path, data):
        target = self.repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(data)

    def write_metadata(self, version):
        self.write('VERSION', version + '\n')
        for name in ('package.json', 'package-lock.json'):
            # Match real manifests with a trailing comma after the version.
            self.write('frontend/' + name, json.dumps(
                {'version': version, 'private': True, 'engines': {'node': '24.19.0'}}, indent=2) + '\n')
        self.write('CHANGELOG.md', f'# Changelog\n\n## [{version}] - 2026-09-07\n\nFixture.\n')

    def write_toolchains(self):
        self.write('go.mod', 'module example.invalid/fixture\n\ngo 1.25.0\n\ntoolchain go1.25.12\n')
        self.write('Dockerfile', 'FROM golang:1.25.12-alpine AS builder\n')
        self.write('frontend/Dockerfile', 'FROM node:24.19.0-alpine3.23@sha256:' + 'a' * 64 + ' AS builder\n')
        self.write('.github/workflows/ci.yml',
                   "      - uses: actions/setup-go@v7\n"
                   "        with:\n"
                   "          go-version: '1.25.12'\n"
                   "      - uses: actions/setup-go@v7\n"
                   "        with:\n"
                   "          go-version: '1.25.12'\n"
                   "      - uses: actions/setup-node@v7\n"
                   "        with:\n"
                   "          node-version-file: 'frontend/package.json'\n"
                   "      - uses: actions/setup-node@v7\n"
                   "        with:\n"
                   "          node-version-file: 'frontend/package.json'\n")
        for name in ('capability-bundle.yml', 'foigent-protocol-conformance.yml'):
            self.write('.github/workflows/' + name,
                   "      - uses: actions/setup-go@v7\n"
                   "        with:\n"
                   "          go-version: '1.25.12'\n")
        restore = b'-- synthetic restore helper fixture\n'
        self.write('scripts/source-budget-restore-rebase.sql', restore.decode())
        self.write('deploy/customer/README.md',
                   '- SHA-256: `' + hashlib.sha256(restore).hexdigest() + '`\n'
                   '- Bytes: `' + str(len(restore)) + '`\n')

    def commit(self):
        self.git('add', '--all')
        self.git('commit', '-qm', 'fixture')
        return self.git('rev-parse', 'HEAD')

    def check(self, *, base=None, head=None, mode=None, failed=False, env=None, error=None):
        run_env = dict(self.env)
        run_env.update(BASE_SHA=self.base if base is None else base,
                       HEAD_SHA='HEAD' if head is None else head)
        if env:
            run_env.update(env)
        result = subprocess.run(['bash', str(CHECKER)], cwd=self.repo, env=run_env,
                                text=True, capture_output=True)
        output = result.stdout + result.stderr
        if failed:
            self.assertNotEqual(result.returncode, 0, output)
            self.assertNotIn('RELEASE_METADATA_OK', output)
            if error:
                self.assertIn(error, output)
        else:
            self.assertEqual(result.returncode, 0, output)
            if mode:
                self.assertIn('mode=' + mode, output)
        return result

    def test_known_nonrelease_paths(self):
        for path in (
            'internal/runtime_test.go', 'scripts/helper_test.go',
            'frontend/src/pages/__tests__/Page.test.tsx',
            'frontend/src/utils/example.test.ts',
            'docs/architecture/example.md', 'docs/CODEBASE-MAP.md',
            'scripts/check-release-metadata.sh', 'scripts/test-release-metadata.py',
            'docs/agent-workflows/AGENT-PROTOCOL.md',
            'capability-bundles/model.json',
        ):
            with self.subTest(path=path):
                self.write(path, 'non-runtime fixture\n')
                self.check(head=self.commit(), mode='non-release-bearing')

    def test_release_paths_fail_without_bump(self):
        for path in (
            'internal/runtime.go', 'go.mod', 'go.sum',
            'migrations/000001_test.up.sql', 'cmd/tool/main.go',
            'frontend/src/pages/Page.tsx', 'frontend/public/example.test.ts',
            'frontend/vite.config.ts', 'frontend/src/fixtures/product.json',
            'scripts/internal-target-rollout.py', 'scripts/check-customer-contract.sh',
            'scripts/test-named-runtime-helper.sh', 'deploy/customer/preflight.sh',
            'Dockerfile', 'docker-compose.test.yml',
            'docs/tech-ref/05-openapi-spec.yaml', 'docs/architecture/schema.json',
            'internal/space and\nnewline.go',
        ):
            with self.subTest(path=path):
                # Each case starts at the same fixture baseline; reset only this
                # test-created repository, never the caller's working tree.
                self.git('reset', '--hard', self.base)
                if path in ('go.mod', 'Dockerfile'):
                    comment = '// changed runtime or contract\n' if path == 'go.mod' else '# changed runtime or contract\n'
                    self.write(path, (self.repo / path).read_text() + comment)
                else:
                    self.write(path, 'changed runtime or contract\n')
                self.check(head=self.commit(), failed=True, error="must update VERSION")

    def test_declared_toolchain_drift_fails_in_current_mode(self):
        self.check(base='', mode='current')
        cases = (
            ('go.mod', 'toolchain go1.25.12', 'toolchain go1.25.13', 'Dockerfile'),
            ('go.mod', 'go 1.25.0', 'go 1.26.0', 'go.mod'),
            ('go.mod', 'toolchain go1.25.12\n', '', 'go.mod'),
            ('Dockerfile', 'golang:1.25.12-alpine', 'golang:1.25.11-alpine', 'Dockerfile'),
            ('.github/workflows/ci.yml', "go-version: '1.25.12'", "go-version: '1.25.11'", '.github/workflows/ci.yml'),
            ('.github/workflows/capability-bundle.yml', "go-version: '1.25.12'", "go-version: '1.25.11'", '.github/workflows/capability-bundle.yml'),
            ('.github/workflows/foigent-protocol-conformance.yml', "go-version: '1.25.12'", "go-version: '1.25.11'", '.github/workflows/foigent-protocol-conformance.yml'),
            ('frontend/Dockerfile', 'node:24.19.0-alpine3.23', 'node:24.18.0-alpine3.23', 'frontend/Dockerfile'),
            ('frontend/Dockerfile', '@sha256:' + 'a' * 64, '', 'frontend/Dockerfile'),
            ('frontend/Dockerfile', '@sha256:' + 'a' * 64, '@sha256:invalid', 'frontend/Dockerfile'),
            ('frontend/package.json', '"node": "24.19.0"', '"node": "24"', 'frontend/package.json'),
            ('frontend/package.json', '"node": "24.19.0"', '"node": "24.18.0"', 'frontend/Dockerfile'),
            ('.github/workflows/ci.yml', "node-version-file: 'frontend/package.json'", "node-version-file: 'other/package.json'", '.github/workflows/ci.yml'),
            ('.github/workflows/ci.yml', "node-version-file: 'frontend/package.json'\n", '', '.github/workflows/ci.yml'),
        )
        for path, old, new, error in cases:
            with self.subTest(path=path, old=old, new=new):
                self.git('reset', '--hard', self.base)
                source = (self.repo / path).read_text()
                self.assertIn(old, source)
                self.write(path, source.replace(old, new, 1))
                self.check(base='', failed=True, error=error)

    def test_node_version_file_rejects_competing_pin(self):
        self.check(base='', mode='current')
        workflow = (self.repo / '.github/workflows/ci.yml').read_text()
        self.write('.github/workflows/ci.yml', workflow.replace(
            "node-version-file: 'frontend/package.json'",
            "node-version-file: 'frontend/package.json'\n          node-version: '22'", 1))
        self.check(base='', failed=True, error='must use only')

    def test_restore_helper_identity_drift_fails_in_current_mode(self):
        self.check(base='', mode='current')
        self.write('scripts/source-budget-restore-rebase.sql', '-- changed helper\n')
        self.check(base='', failed=True, error='declared restore helper identity differs')

    def test_mixed_change_still_needs_bump(self):
        self.write('internal/runtime_test.go', 'package runtime\n')
        self.write('internal/runtime.go', 'package changed\n')
        self.check(head=self.commit(), failed=True, error='must update VERSION')

    def test_runtime_deletion_and_rename_do_not_hide_change(self):
        for destination in (None, 'docs/architecture/retired.md', 'internal/runtime_test.go'):
            with self.subTest(destination=destination):
                self.git('reset', '--hard', self.base)
                source = self.repo / 'internal/runtime.go'
                if destination:
                    target = self.repo / destination
                    target.parent.mkdir(parents=True, exist_ok=True)
                    source.rename(target)
                else:
                    source.unlink()
                self.check(head=self.commit(), failed=True, error='must update VERSION')

    def test_metadata_consistency_and_valid_bump(self):
        self.write('internal/runtime.go', 'package changed\n')
        self.write_metadata('1.2.4')
        self.check(head=self.commit(), mode='release-bearing')
        self.check(base='', mode='current')

    def test_missing_version_or_changelog_change(self):
        # A pre-existing heading cannot replace a CHANGELOG diff.
        self.write('CHANGELOG.md', '# Changelog\n\n## [1.2.3] - 2026-09-07\n\n## [1.2.4] - 2026-09-07\n')
        base_with_heading = self.commit()
        for missing in ('VERSION', 'CHANGELOG.md'):
            with self.subTest(missing=missing):
                self.git('reset', '--hard', base_with_heading)
                self.write('internal/runtime.go', 'package changed\n')
                self.write_metadata('1.2.4' if missing == 'CHANGELOG.md' else '1.2.3')
                if missing == 'CHANGELOG.md':
                    self.write(missing, self.git('show', base_with_heading + ':' + missing) + '\n')
                self.check(base=base_with_heading, head=self.commit(), failed=True,
                           error='must update ' + missing)

    def test_missing_dated_heading(self):
        self.write('internal/runtime.go', 'package changed\n')
        self.write_metadata('1.2.4')
        self.write('CHANGELOG.md', '# Changelog\n\n## [1.2.4]\n')
        self.check(head=self.commit(), failed=True, error='lacks a dated')

    def test_version_regression_or_manifest_mismatch(self):
        for version in ('1.2.2', '1.1.9', '0.9.9', 'invalid'):
            with self.subTest(version=version):
                self.git('reset', '--hard', self.base)
                self.write('internal/runtime.go', 'package changed\n')
                self.write_metadata(version)
                self.check(head=self.commit(), failed=True)
        for path in ('frontend/package.json', 'frontend/package-lock.json'):
            with self.subTest(path=path):
                self.git('reset', '--hard', self.base)
                self.write(path, '{\n  "version": "9.9.9",\n  "private": true\n}\n')
                self.check(head=self.commit(), failed=True)

    def test_unavailable_or_invalid_refs_fail(self):
        for base, head in (('bad', 'HEAD'), ('f' * 40, 'HEAD'),
                           (self.base, 'bad'), (self.base, 'f' * 40)):
            with self.subTest(base=base, head=head):
                self.check(base=base, head=head, failed=True)

    def test_git_diff_failure_does_not_pass_as_empty_change(self):
        shim = self.root / 'bin'
        shim.mkdir()
        fake_git = shim / 'git'
        fake_git.write_text('#!/usr/bin/env python3\nimport os,sys\n'
                            'if sys.argv[1:2] == ["diff"]:\n'
                            '    sys.stderr.write("simulated diff failure\\n")\n'
                            '    sys.exit(73)\n'
                            f'os.execv({GIT!r}, [{GIT!r}] + sys.argv[1:])\n')
        fake_git.chmod(0o700)
        self.check(env={'PATH': str(shim) + os.pathsep + self.env['PATH']},
                   failed=True, error='simulated diff failure')

    def route(self, *, event='pull_request', base=None):
        workflow = WORKFLOW.read_text()
        step = workflow.split('      - name: Classify changed files\n', 1)[1]
        block = step.split('        run: |\n', 1)[1].split('\n      - name:', 1)[0]
        output = self.root / 'github-output'
        output.write_text('')
        env = dict(self.env, EVENT_NAME=event, BASE_SHA=base or self.base,
                   HEAD_SHA=self.git('rev-parse', 'HEAD'), GITHUB_OUTPUT=str(output))
        result = subprocess.run(['bash', '-c', textwrap.dedent(block)],
                                cwd=self.repo, env=env, text=True, capture_output=True)
        return result, dict(line.split('=', 1) for line in output.read_text().splitlines())

    def test_actual_ci_routing_preserves_coverage(self):
        for path, expected in (
            ('docs/architecture/wording.md', ('false', 'false', 'false')),
            ('capability-bundles/model.json', ('true', 'false', 'false')),
            ('deploy/customer/preflight.sh', ('false', 'false', 'true')),
            ('internal/runtime_test.go', ('true', 'false', 'true')),
            ('frontend/src/pages/Page.test.tsx', ('false', 'true', 'true')),
            ('scripts/check-release-metadata.sh', ('true', 'true', 'true')),
            ('unknown-path.bin', ('true', 'true', 'true')),
            ('docs/tech-ref/05-openapi-spec.yaml', ('true', 'false', 'false')),
        ):
            with self.subTest(path=path):
                self.git('reset', '--hard', self.base)
                self.write(path, 'fixture\n')
                self.commit()
                result, routed = self.route()
                self.assertEqual(result.returncode, 0, result.stderr)
                for key, value in zip(('backend', 'frontend', 'docker'), expected):
                    # Older supported checkouts conservatively build images for
                    # Bundles too; the invariant here is retained Backend coverage.
                    if value is not None:
                        self.assertEqual(routed[key], value)
                    else:
                        self.assertIn(routed[key], ('true', 'false'))
        result, routed = self.route(event='workflow_dispatch')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(set(routed.values()), {'true'})
        result, _ = self.route(base='f' * 40)
        self.assertNotEqual(result.returncode, 0)

    def test_actual_ci_router_retains_deleted_side_of_rename(self):
        (self.repo / 'docs').mkdir()
        (self.repo / 'internal/runtime.go').rename(self.repo / 'docs/runtime.md')
        self.commit()
        result, routed = self.route()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(routed, {'backend': 'true', 'frontend': 'false', 'docker': 'true'})

    def test_actual_router_deletions_and_cross_surface_renames(self):
        for destination, expected in ((None, ('true', 'false', 'true')),
                                      ('frontend/retired.txt', ('true', 'true', 'true'))):
            with self.subTest(destination=destination):
                self.git('reset', '--hard', self.base)
                source = self.repo / 'internal/runtime.go'
                if destination:
                    target = self.repo / destination
                    target.parent.mkdir(parents=True, exist_ok=True)
                    source.rename(target)
                else:
                    source.unlink()
                self.commit()
                result, routed = self.route()
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(tuple(routed[k] for k in ('backend', 'frontend', 'docker')), expected)

    def test_actual_job_conditions_only_suppress_explicit_preflight_failure(self):
        workflow = WORKFLOW.read_text()
        output = re.search(r'delivery_preflight_failed: \$\{\{ (.+) \}\}', workflow).group(1)
        def evaluate(expression, values):
            # Evaluate this repository's finite boolean job expressions, not a
            # reimplementation of their routing policy or a general YAML engine.
            expression = re.sub(r'(?:steps|needs)\.[a-zA-Z0-9_.-]+',
                                lambda match: repr(values.get(match.group(), '')), expression)
            expression = expression.replace('always()', 'True').replace('&&', ' and ').replace('||', ' or ')
            return eval(' '.join(expression.split()), {'__builtins__': {}}, {})
        for first in ('success', 'failure', 'skipped', 'cancelled', ''):
            for second in ('success', 'failure', 'skipped', 'cancelled', ''):
                explicit_failure = first == 'failure' or second == 'failure'
                self.assertEqual(evaluate(output, {'steps.delivery_python.outcome': first,
                                                  'steps.delivery_preflight.outcome': second}), explicit_failure)
        for job in ('backend', 'frontend', 'pr4-browser', 'docker'):
            section = workflow.split(f'\n  {job}:\n', 1)[1]
            condition = re.search(r'    if: >-\n((?:      .+\n)+)', section).group(1)
            for flag in ('', 'false', 'true'):
                values = {'needs.changes.result': 'failure', 'needs.changes.outputs.delivery_preflight_failed': flag}
                self.assertEqual(evaluate(condition, values), flag != 'true', (job, flag))
            values = {'needs.changes.result':'success', 'needs.changes.outputs.delivery_preflight_failed':'false',
                      'needs.changes.outputs.backend':'true', 'needs.changes.outputs.frontend':'true', 'needs.changes.outputs.docker':'true'}
            self.assertTrue(evaluate(condition, values), job)


if __name__ == '__main__':
    unittest.main(verbosity=2)
