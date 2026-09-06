#!/usr/bin/env python3
"""Offline component-build contract checks; no real Docker or network calls."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).with_name('internal-target-build-images.sh').read_text()
COMMIT = 'a' * 40


class ComponentBuildTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='unirouter-component-build-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'bin').mkdir()
        (self.root / 'scripts').mkdir()
        (self.root / 'VERSION').write_text('1.2.58\n')
        # The production lock is Linux-host scoped; redirect only its pathname.
        self.assertEqual(SOURCE.count('/run/lock/unirouter-docker-lifecycle.lock'), 1)
        (self.root / 'scripts/build.sh').write_text(SOURCE.replace(
            '/run/lock/unirouter-docker-lifecycle.lock', str(self.root / 'lock')))
        self.env = {**os.environ, 'PATH': str(self.root / 'bin') + ':' + os.environ['PATH'],
                    'TEST_ROOT': str(self.root), 'RUNTIME_VERSION': '1.2.58', 'RUNTIME_COMMIT': COMMIT}
        for key in ('UNIROUTER_BACKEND_IMAGE', 'UNIROUTER_FRONTEND_IMAGE', 'UNIROUTER_FRONTEND_REUSE_FROM'):
            self.env.pop(key, None)
        self.script('bin/git', '''#!/bin/sh
case "$1 $2" in
  'rev-parse --show-toplevel') echo "$TEST_ROOT";;
  'rev-parse HEAD') echo "$RUNTIME_COMMIT";;
  rev-parse*) echo bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb;;
  status*) printf '%s' "${TEST_DIRTY:-}";;
  *) exit 91;;
esac
''')
        self.script('bin/flock', '#!/bin/sh\nexit 0\n')
        self.script('scripts/docker-storage-preflight.sh', '#!/bin/sh\nexit "${TEST_STORAGE_EXIT:-0}"\n')
        self.script('bin/docker', '''#!/usr/bin/env python3
import json, os, pathlib, sys
r = pathlib.Path(os.environ['TEST_ROOT']); a = sys.argv[1:]
with (r/'calls').open('a') as f: f.write(json.dumps(a)+'\\n')
if a == ['info']: sys.exit(0)
if a[0] == 'build':
    tag=a[a.index('--tag')+1]; (r/('built-'+tag.split('/')[1].split(':')[0])).touch(); sys.exit(0)
if a[:2] == ['image','inspect']:
    tag=a[-1]; component=tag.split('/')[1].split(':')[0]
    exists=(r/('built-'+component)).exists() or (component=='frontend' and os.environ.get('TEST_EXISTING'))
    if not exists: sys.exit(1)
    if '--format' in a:
        fmt=a[a.index('--format')+1]
        if 'io.unirouter.component' in fmt: print(component)
        elif 'image.revision' in fmt: print('wrong' if os.environ.get('TEST_BAD_TAG') else os.environ['RUNTIME_COMMIT'])
        elif 'image.version' in fmt: print(os.environ['RUNTIME_VERSION'])
        elif '.Id' in fmt: print('sha256:'+component)
        else: sys.exit(92)
    sys.exit(0)
sys.exit(93)
''')

    def script(self, path, value):
        p = self.root / path
        p.write_text(value)
        p.chmod(0o755)

    def run_build(self, *args, **env):
        result = subprocess.run(['bash', str(self.root / 'scripts/build.sh'), *args],
                                cwd=self.root, env={**self.env, **env}, capture_output=True, text=True)
        log = self.root / 'calls'
        calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
        return result, calls

    def test_frontend_only_never_touches_backend(self):
        result, calls = self.run_build('--frontend-only')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum(c[0] == 'build' for c in calls), 1)
        self.assertFalse(any('backend' in ' '.join(c) or 'GOPROXY' in ' '.join(c) for c in calls))
        self.assertIn('SELECTED UNIROUTER_FRONTEND_IMAGE=', result.stdout)
        self.assertNotIn('SELECTED UNIROUTER_BACKEND_IMAGE=', result.stdout)

    def test_default_builds_both_components(self):
        result, calls = self.run_build()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum(c[0] == 'build' for c in calls), 2)
        self.assertIn('SELECTED UNIROUTER_BACKEND_IMAGE=', result.stdout)
        self.assertIn('SELECTED UNIROUTER_FRONTEND_IMAGE=', result.stdout)

    def test_matching_immutable_frontend_is_reused(self):
        result, calls = self.run_build('--frontend-only', TEST_EXISTING='1')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(c[0] == 'build' for c in calls))

    def test_preconditions_reject_without_build(self):
        for args, env in [(('--frontend-only',), {'TEST_EXISTING': '1', 'TEST_BAD_TAG': '1'}),
                          (('--frontend-only',), {'TEST_DIRTY': ' M frontend/file'}),
                          (('--frontend-only',), {'TEST_STORAGE_EXIT': '1'}),
                          (('--unknown',), {}), (('--frontend-only', 'extra'), {})]:
            with self.subTest(args=args, env=env):
                result, calls = self.run_build(*args, **env)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any(c[0] == 'build' for c in calls))


if __name__ == '__main__':
    unittest.main()
