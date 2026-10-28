#!/usr/bin/env python3
"""Offline transport/adversarial preparation checks; no server or Docker use."""
import contextlib
import copy
import gzip
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT / "deploy/community/bootstrap.py"
if not MODULE.exists():
    MODULE = Path(__file__).with_name("bootstrap.py")
spec = importlib.util.spec_from_file_location("bootstrap", MODULE)
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return json.dumps(value, separators=(",", ":")).encode()


def archive_bytes(manifest, payload, changes=None, tar_format=tarfile.USTAR_FORMAT):
    entries = [("manifest.json", encoded(manifest), {}), ("manifest.sig", b'{}', {})]
    entries += [(name, data, {}) for name, data in payload.items()]
    if changes:
        entries = changes(entries)
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode="w", format=tar_format) as archive:
        for name, data, attributes in entries:
            entry = tarfile.TarInfo(name)
            entry.size = len(data)
            for key, value in attributes.items():
                setattr(entry, key, value)
            archive.addfile(entry, io.BytesIO(data))
    return gzip.compress(out.getvalue())


class BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="ur-bootstrap-test-")
        self.root = Path(self.tmp.name).resolve()
        self.output = self.root / "prepared"
        self.payload = {b.INSTALLER: b"# synthetic installer; never executed\n", "payload/example.txt": b"hello\n"}
        self.release = {"version": "1.11.0", "commit": "a" * 40, "release_sequence": 1,
                        "key_id": "bootstrap-test", "target_schema": b.SCHEMA}
        self.manifest = {"schema_version": "unirouter.upgrade-manifest.v2", "expires_at": "never", **self.release, "target_os": "linux", "target_arch": "amd64",
                         "artifacts": [{"path": name, "size": len(data), "sha256": sha(data)}
                                       for name, data in sorted(self.payload.items())]}
        self.assets = {"verifier": b"synthetic verifier fixture", "public_keys": b'{"bootstrap-test":"synthetic"}',
                       "package": archive_bytes(self.manifest, self.payload)}
        self.system = mock.patch.object(b.platform, "system", return_value="Linux")
        self.arch = mock.patch.object(b.platform, "machine", return_value="x86_64")
        self.system.start()
        self.arch.start()

    def tearDown(self):
        self.arch.stop()
        self.system.stop()
        self.tmp.cleanup()

    def bootstrap(self, edit=None):
        value = {"schema_version":"unirouter.community-bootstrap.v1", "platform":{"os":"linux","arch":"amd64"},
                 "release":copy.deepcopy(self.release), "assets":{name:{"url":"https://example.invalid/"+name,
                  "size":len(data), "sha256":sha(data)} for name,data in self.assets.items()}}
        if edit:
            edit(value)
        raw = encoded(value)
        return b.Bootstrap(raw, sha(raw))

    def fetch(self, asset, target):
        b.write_new(target, self.assets[asset["url"].rsplit("/", 1)[-1]])

    def verify(self, args):
        self.assertNotIn("--at", args)
        self.assertFalse((self.output / b.INSTALLER).exists())
        return self.verified_fields()

    def verified_fields(self):
        return {"verified":"true", "package_sha256":sha(self.assets["package"]),
                "manifest_sha256":sha(encoded(self.manifest)), "target":"linux/amd64",
                **{key:str(value) for key,value in self.release.items() if key != "key_id"}}

    def prepare(self, anchor=None):
        return b.prepare(anchor or self.bootstrap(), self.output, self.fetch, self.verify)

    def snapshot(self):
        return {str(p.relative_to(self.root)):(p.lstat().st_mode, p.read_bytes() if p.is_file() and not p.is_symlink() else None)
                for p in self.root.rglob("*")}

    def test_prepare_binds_verifier_before_payload_and_never_runs_installer(self):
        with mock.patch.object(b, "delegate") as delegate:
            receipt = self.prepare()
            delegate.assert_not_called()
        self.assertEqual(receipt["manifest_sha256"], sha(encoded(self.manifest)))
        self.assertEqual((self.output / "payload/example.txt").read_bytes(), b"hello\n")
        for item in [self.output, *self.output.rglob("*")]:
            mode = stat.S_IMODE(item.stat().st_mode)
            self.assertEqual(mode, 0o700 if item.is_dir() or item.name == "uniroute-upgrade" else 0o600)

    def test_reuse_checks_are_read_only_and_reverify_current_time(self):
        anchor = self.bootstrap()
        self.prepare(anchor)
        before = self.snapshot()
        calls = []
        def verify(args):
            calls.append(args)
            self.assertNotIn("--at", args)
            return self.verified_fields()
        b.check_prepared(anchor, self.output, verify)
        self.assertEqual(len(calls), 1)
        self.assertEqual(before, self.snapshot())

    def test_existing_and_partial_destination_refused_without_fetch_or_execution(self):
        self.output.mkdir(mode=0o700)
        (self.output / "keep").write_bytes(b"owner data")
        before = self.snapshot()
        with mock.patch.object(b, "download") as fetch, mock.patch.object(b, "verifier_output") as verify:
            with self.assertRaises(b.BootstrapError):
                b.prepare(self.bootstrap(), self.output, fetch, verify)
            fetch.assert_not_called()
            verify.assert_not_called()
        self.assertEqual(before, self.snapshot())

    def test_wrong_anchor_duplicate_unknown_and_oversized_bootstrap(self):
        anchor = self.bootstrap()
        bad = [(anchor.raw, "0" * 64), (b'{"a":1,"a":2}', sha(b'{"a":1,"a":2}')),
               (b" " * (b.MAX_BOOTSTRAP + 1), sha(b" " * (b.MAX_BOOTSTRAP + 1)))]
        for raw, digest in bad:
            with self.subTest(raw_size=len(raw)), self.assertRaises((b.BootstrapError, ValueError)):
                b.Bootstrap(raw, digest)
        for mutate in (lambda v:v.update(extra=True), lambda v:v["platform"].update(arch="arm64"),
                       lambda v:v["release"].update(release_sequence=True),
                       lambda v:v["assets"]["package"].update(size=b.MAX_PAYLOAD+1)):
            with self.assertRaises(b.BootstrapError):
                self.bootstrap(mutate)
        self.assertFalse(self.output.exists())

    def test_wrong_host_has_no_writes(self):
        with mock.patch.object(b.platform,"system",return_value="Darwin"), self.assertRaises(b.BootstrapError):
            self.prepare()
        self.assertFalse(self.output.exists())

    def test_wrong_asset_never_executes_verifier(self):
        for name in b.ASSETS:
            with self.subTest(asset=name):
                self.output = self.root / name
                anchor = self.bootstrap(lambda v:v["assets"][name].update(sha256="0"*64))
                verifier = mock.Mock()
                with self.assertRaises(b.BootstrapError):
                    b.prepare(anchor, self.output, self.fetch, verifier)
                verifier.assert_not_called()
                self.assertFalse((self.output / b.RECEIPT).exists())
                self.assertFalse((self.output / b.INSTALLER).exists())

    def test_failed_signature_manifest_binding_or_release_identity_never_extracts(self):
        cases = ("signature", "manifest", "package", "version", "key_id")
        for name in cases:
            with self.subTest(case=name):
                self.output = self.root / name
                anchor = self.bootstrap(lambda v:v["release"].update(key_id="different") if name=="key_id" else None)
                fields = self.verified_fields()
                if name == "manifest": fields["manifest_sha256"] = "0"*64
                if name == "package": fields["package_sha256"] = "0"*64
                if name == "version": fields["version"] = "1.10.0"
                def verify(args):
                    if name == "signature": raise b.BootstrapError("Synthetic wrong key or expired package.")
                    return fields
                with self.assertRaises(b.BootstrapError):
                    b.prepare(anchor, self.output, self.fetch, verify)
                self.assertFalse((self.output / b.INSTALLER).exists())
                self.assertFalse((self.output / b.RECEIPT).exists())

    def test_archive_attacks_rejected_before_verifier_execution(self):
        cases = {
            "traversal": lambda entries:entries+[('../escape',b'x',{})],
            "absolute": lambda entries:entries+[('/absolute',b'x',{})],
            "duplicate": lambda entries:entries+[entries[-1]],
            "undeclared": lambda entries:entries+[('payload/extra',b'x',{})],
            "missing": lambda entries:entries[:-1],
            "symlink": lambda entries:entries[:2]+[(b.INSTALLER,b'',{'type':tarfile.SYMTYPE,'linkname':'/tmp/escape'})],
            "hardlink": lambda entries:entries[:2]+[(b.INSTALLER,b'',{'type':tarfile.LNKTYPE,'linkname':'/tmp/escape'})],
            "fifo": lambda entries:entries[:2]+[(b.INSTALLER,b'',{'type':tarfile.FIFOTYPE})],
            "hash": lambda entries:entries[:-1]+[(entries[-1][0],b'wrong\n',{})],
            "size": lambda entries:entries[:-1]+[(entries[-1][0],b'wrong-size',{})],
        }
        for name, edit in cases.items():
            with self.subTest(case=name):
                self.output = self.root / name
                self.assets['package'] = archive_bytes(self.manifest,self.payload,edit)
                verify = mock.Mock()
                with self.assertRaises(b.BootstrapError):
                    b.prepare(self.bootstrap(), self.output, self.fetch, verify)
                verify.assert_not_called()
                self.assertFalse((self.output / b.RECEIPT).exists())
                self.assertFalse((self.root / 'escape').exists())

    def test_pax_gnu_and_excessive_uncompressed_padding_rejected(self):
        cases = {
            'pax': archive_bytes(self.manifest,self.payload,
                  lambda e:e[:2]+[(b.INSTALLER,self.payload[b.INSTALLER],{'pax_headers':{'comment':'extension'}})]+e[3:], tarfile.PAX_FORMAT),
            'gnu': archive_bytes(self.manifest,self.payload,tar_format=tarfile.GNU_FORMAT),
            'padding': gzip.compress(gzip.decompress(self.assets['package'])+bytes(b.MAX_TAR_PADDING+1)),
            'truncated': self.assets['package'][:-6],
        }
        for name, raw in cases.items():
            with self.subTest(case=name):
                file = self.root / (name+'.tgz')
                b.write_new(file, raw)
                with self.assertRaises((b.BootstrapError, EOFError)):
                    b.scan_archive(file)

    def test_manifest_path_collision_and_payload_limit_refused(self):
        manifest = copy.deepcopy(self.manifest)
        manifest['artifacts'].append({'path':'payload/deploy','size':0,'sha256':sha(b'')})
        with self.assertRaises(b.BootstrapError): b.manifest_files(encoded(manifest))
        manifest['artifacts'][-1]['path'] = 'payload/large'
        manifest['artifacts'][-1]['size'] = b.MAX_PAYLOAD
        with self.assertRaises(b.BootstrapError): b.manifest_files(encoded(manifest))

    def test_reuse_detects_payload_tamper_or_extra_file_without_writes(self):
        anchor = self.bootstrap()
        self.prepare(anchor)
        (self.output/'payload/example.txt').write_bytes(b'tampered')
        before = self.snapshot()
        with self.assertRaises(b.BootstrapError):
            b.check_prepared(anchor,self.output,lambda _:self.verified_fields())
        self.assertEqual(before,self.snapshot())
        (self.output/'payload/example.txt').write_bytes(b'hello\n')
        (self.output/'payload/extra').write_bytes(b'untrusted')
        before = self.snapshot()
        with self.assertRaises(b.BootstrapError):
            b.check_prepared(anchor,self.output,lambda _:self.verified_fields())
        self.assertEqual(before,self.snapshot())

    def test_symlink_world_writable_and_different_owner_directories_refused(self):
        real = self.root/'real'
        real.mkdir(mode=0o700)
        self.output.symlink_to(real,target_is_directory=True)
        with self.assertRaises(b.BootstrapError): b.protected_directory(self.output,exists=True)
        real.chmod(0o777)
        with self.assertRaises(b.BootstrapError): b.protected_directory(real,exists=True)
        real.chmod(0o700)
        original = Path.lstat
        def foreign(path):
            info = original(path)
            if path == real:
                values = list(info)
                values[4] = os.geteuid()+12345
                return os.stat_result(values)
            return info
        with mock.patch.object(Path,'lstat',foreign), self.assertRaises(b.BootstrapError):
            b.protected_directory(real,exists=True)

    def test_https_download_size_hash_redirect_and_no_overwrite(self):
        raw = b'public fixture'
        asset = {'url':'https://example.invalid/file','size':len(raw),'sha256':sha(raw)}
        class Response(io.BytesIO):
            status = 200
            headers = {}
            def geturl(self): return asset['url']
        with mock.patch.object(b.urllib.request,'build_opener') as opener:
            opener.return_value.open.return_value = Response(raw)
            b.download(asset,self.root/'download')
            self.assertEqual((self.root/'download').read_bytes(),raw)
            opener.return_value.open.return_value = Response(raw)
            with self.assertRaises(FileExistsError): b.download(asset,self.root/'download')
            for name, body in (('large',raw+b'x'),('short',raw[:-1]),('wrong',b'x'*len(raw))):
                opener.return_value.open.return_value = Response(body)
                with self.assertRaises(b.BootstrapError): b.download(asset,self.root/name)
        for url in ('http://example.invalid/x','https://user:secret@example.invalid/x','https://example.invalid:444/x'):
            with self.assertRaises(b.BootstrapError):
                b.HTTPSRedirects().redirect_request(urllib.request.Request(asset['url']),None,302,'',{},url)

    def test_child_environment_excludes_python_docker_and_secret_overrides(self):
        with mock.patch.dict(os.environ, {'PYTHONPATH':'untrusted','DOCKER_HOST':'untrusted','TOKEN':'secret'}):
            environment = b.child_environment()
            self.assertNotIn('PYTHONPATH',environment)
            self.assertNotIn('DOCKER_HOST',environment)
            self.assertNotIn('TOKEN',environment)

    def test_verifier_failure_does_not_log_child_output(self):
        result = subprocess.CompletedProcess([],1,b'synthetic-secret',b'synthetic-secret')
        with mock.patch.object(b.subprocess,'run',return_value=result):
            with self.assertRaises(b.BootstrapError) as error:
                b.verifier_output(['/trusted/verifier'])
            self.assertNotIn('synthetic-secret',str(error.exception))

    def test_partial_reuse_refuses_without_writes(self):
        anchor = self.bootstrap()
        self.output.mkdir(mode=0o700)
        (self.output / 'bootstrap.json').write_bytes(anchor.raw)
        before = self.snapshot()
        verify = mock.Mock()
        with self.assertRaises(b.BootstrapError):
            b.check_prepared(anchor, self.output, verify)
        verify.assert_not_called()
        self.assertEqual(before, self.snapshot())

    def test_cli_check_resume_and_existing_install_only_reuse_verified_files(self):
        anchor = self.bootstrap()
        path = self.root / 'bootstrap.json'
        path.write_bytes(anchor.raw)
        self.output.mkdir(mode=0o700)
        before = self.snapshot()
        for action in ('check', 'resume', 'install'):
            with (mock.patch.object(b.os, 'geteuid', return_value=0),
                  mock.patch.object(b, 'check_prepared') as check,
                  mock.patch.object(b, 'prepare') as prepare,
                  mock.patch.object(b, 'delegate', return_value=0) as delegate):
                args = [action, '--bootstrap', str(path), '--bootstrap-sha256', anchor.sha,
                        '--directory', str(self.output), '--name', 'example', '--port', '4321']
                self.assertEqual(b.main(args), 0)
                self.assertEqual(check.call_count, 1)
                prepare.assert_not_called()
                self.assertEqual(delegate.call_args.args, (action, self.output, Path('/opt/unirouter'),
                                                          'example', 4321, Path('/var/run/docker.sock')))
                delegate.reset_mock()
                check.side_effect = b.BootstrapError('Synthetic invalid preparation.')
                with contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(b.main(args), 1)
                prepare.assert_not_called()
                delegate.assert_not_called()
        self.assertEqual(before, self.snapshot())

    def test_install_delegates_signed_script_without_shell_or_python_imports(self):
        with mock.patch.object(b.subprocess, 'run', return_value=subprocess.CompletedProcess([],0)) as run:
            self.assertEqual(b.delegate('install',self.output,Path('/opt/example'),'example',4321,Path('/var/run/docker.sock')),0)
            args, kwargs = run.call_args
            self.assertEqual(args[0][:5],[sys.executable,'-I','-B',str(self.output/b.INSTALLER),'install'])
            self.assertNotIn('shell',kwargs)


    def test_upgrade_delegates_one_signed_program_with_fixed_argv(self):
        self.output.mkdir(mode=0o700)
        program = self.output / b.UPGRADER
        program.parent.mkdir(parents=True)
        program.write_text("synthetic signed payload")
        for action in b.UPGRADE_ACTIONS:
            with self.subTest(action=action), mock.patch.object(b.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)) as run:
                self.assertEqual(0, b.delegate_upgrade(action, self.output, Path('/opt/ur'), Path('/retained/source.tar.gz'), 'ur_edge_pair', Path('/opt/ur-edge'), Path('/original/historical-exits.json')))
                argv = run.call_args.args[0]
                self.assertEqual([sys.executable, '-I', '-B', str(program), action], argv[:5])
                self.assertIn('--source-package', argv)
                self.assertNotIn('--name', argv)
                self.assertNotIn('--port', argv)
                self.assertEqual('ur_edge_pair', argv[argv.index('--layout') + 1])
                self.assertEqual('/original/historical-exits.json', argv[argv.index('--historical-exits') + 1])
                self.assertNotIn('shell', run.call_args.kwargs)
        program.unlink()
        with mock.patch.object(b.subprocess, 'run') as run:
            with self.assertRaises(b.BootstrapError): b.delegate_upgrade('upgrade', self.output, Path('/opt/ur'), None, 'community_single', None)
            run.assert_not_called()

if __name__ == '__main__':
    unittest.main()
