#!/usr/bin/env python3
"""Local transport/adversarial checks; ephemeral loopback HTTP only, no Docker."""
import contextlib
import copy
import gzip
import hashlib
import http.server
import importlib.util
import io
import json
import os
from pathlib import Path
import random
import stat
import subprocess
import sys
import tarfile
import tempfile
import threading
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

    @contextlib.contextmanager
    def http_transport(self, mode="normal", interrupt_once=False):
        """Real HTTP body/headers; only routing and reported HTTPS URL are adapted.

        This tests transfer semantics/bytes without creating TLS credentials.
        Production HTTPS URL and redirect-policy tests remain independent.
        """
        assets, records = self.assets, []
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                name = self.path.lstrip("/")
                raw, requested = assets[name], self.headers.get("Range")
                offset = int(requested.removeprefix("bytes=").removesuffix("-")) if requested else 0
                ranged = requested is not None and mode != "ignored-range"
                body = raw[offset:] if ranged else raw
                self.send_response(206 if ranged else 200)
                length = len(body) + (1 if mode == "wrong-length" else 0)
                self.send_header("Content-Length", str(length))
                if mode == "duplicate-length":
                    self.send_header("Content-Length", str(length))
                if ranged and mode != "missing-range":
                    start = offset + (1 if mode == "wrong-start" else 0)
                    total = len(raw) + (1 if mode == "wrong-total" else 0)
                    end = len(raw) - (2 if mode == "wrong-end" else 1)
                    value = f"bytes {start}-{end}/{total}"
                    self.send_header("Content-Range", value)
                    if mode == "duplicate-range":
                        self.send_header("Content-Range", value)
                if mode == "encoded":
                    self.send_header("Content-Encoding", "gzip")
                self.end_headers()
                if interrupt_once and name == "package" and not any(r["name"] == name for r in records):
                    body = body[:len(body)//3]
                record = {"name":name, "range":requested, "body_bytes_written":0}
                records.append(record)
                try:
                    self.wfile.write(body)
                    self.wfile.flush()
                    record["body_bytes_written"] = len(body)
                except (BrokenPipeError, ConnectionResetError):
                    pass
                self.close_connection = True

        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval":0.01}, daemon=True)
        real_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        class Adapter:
            def open(self, request, timeout):
                local = urllib.request.Request(f"http://127.0.0.1:{server.server_port}" +
                                               urllib.parse.urlsplit(request.full_url).path,
                                               headers=dict(request.header_items()))
                response = real_opener.open(local, timeout=timeout)
                response.geturl = lambda: request.full_url
                return response
        thread.start()
        try:
            with mock.patch.object(b.urllib.request, "build_opener", return_value=Adapter()):
                yield records
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)
            self.assertFalse(thread.is_alive())

    def download_stage(self, anchor):
        self.output.mkdir(mode=0o700)
        b.write_new(self.output / "bootstrap.json", anchor.raw)

    def test_actual_http_interruption_range_continuation_and_zero_network_completed_reuse(self):
        # Cross multiple downloader reads; compressed repetition would hide this.
        self.payload["payload/example.bin"] = random.Random(0).randbytes(3 * 1024 * 1024 + 8192)
        self.manifest["artifacts"] = [{"path":name, "size":len(data), "sha256":sha(data)}
                                      for name, data in sorted(self.payload.items())]
        self.assets["package"] = archive_bytes(self.manifest, self.payload)
        anchor, verify = self.bootstrap(), mock.Mock(side_effect=self.verify)
        with self.http_transport(interrupt_once=True) as records:
            with self.assertRaises(b.BootstrapError):
                b.prepare(anchor, self.output, b.download, verify)
            prefix = (self.output / "release.tar.gz").read_bytes()
            self.assertEqual(prefix, self.assets["package"][:len(prefix)])
            self.assertGreater(len(prefix), 0)
            verify.assert_not_called()
            before = self.snapshot()
            with self.assertRaises(b.BootstrapError):
                b.check_prepared(anchor, self.output, verify)
            self.assertEqual(before, self.snapshot())
            verify.assert_not_called()
            b.prepare(anchor, self.output, b.download, verify)
            self.assertEqual(verify.call_count, 1)
            self.assertEqual([r["range"] for r in records if r["name"] == "package"],
                             [None, f"bytes={len(prefix)}-"])
            self.assertEqual(sum(r["body_bytes_written"] for r in records), sum(map(len, self.assets.values())))
            before, requests = self.snapshot(), len(records)
            with mock.patch.object(b, "delegate") as delegate:
                b.prepare(anchor, self.output, b.download, lambda _:self.verified_fields())
                delegate.assert_not_called()
            self.assertEqual(len(records), requests)
            self.assertEqual(before, self.snapshot())
            package_requests = [r for r in records if r["name"] == "package"]
            print(json.dumps({"local_HTTP_transport":"interruption_then_standard_Range",
                              "initial_package_body_bytes":package_requests[0]["body_bytes_written"],
                              "continued_package_body_bytes":package_requests[1]["body_bytes_written"],
                              "complete_package_bytes":len(self.assets["package"]),
                              "all_asset_body_bytes_written":sum(r["body_bytes_written"] for r in records),
                              "completed_prepare_reuse_new_requests":len(records)-requests}), flush=True)

    def test_actual_http_bad_range_headers_never_append(self):
        asset = self.bootstrap().assets["package"]
        prefix = self.assets["package"][:37]
        for mode in ("ignored-range", "missing-range", "wrong-start", "wrong-end", "wrong-total", "duplicate-range", "wrong-length",
                     "duplicate-length", "encoded"):
            with self.subTest(mode=mode):
                target = self.root / mode
                b.write_new(target, prefix)
                with self.http_transport(mode=mode), self.assertRaises(b.BootstrapError):
                    b.download(asset, target)
                self.assertEqual(target.read_bytes(), prefix)

    def test_bad_prefix_is_retained_and_never_verified_then_refused_without_network(self):
        anchor, verify = self.bootstrap(), mock.Mock()
        self.download_stage(anchor)
        prefix = b"wrong prefix"
        b.write_new(self.output / "release.tar.gz", prefix)
        with self.http_transport() as records:
            with self.assertRaises(b.BootstrapError):
                b.prepare(anchor, self.output, b.download, verify)
            verify.assert_not_called()
            bad = (self.output / "release.tar.gz").read_bytes()
            self.assertEqual(bad, prefix + self.assets["package"][len(prefix):])
            before, requests = self.snapshot(), len(records)
            with self.assertRaises(b.BootstrapError):
                b.prepare(anchor, self.output, b.download, verify)
            self.assertEqual(requests, len(records))
            self.assertEqual(before, self.snapshot())
            self.assertFalse((self.output / b.INSTALLER).exists())
            self.assertFalse((self.output / b.RECEIPT).exists())

    def test_all_complete_assets_reuse_without_network_but_reverify_before_receipt(self):
        anchor = self.bootstrap()
        self.download_stage(anchor)
        for name, (filename, _) in b.ASSETS.items():
            b.write_new(self.output / filename, self.assets[name])
        verify = mock.Mock(side_effect=self.verify)
        with self.http_transport() as records:
            b.prepare(anchor, self.output, b.download, verify)
            self.assertEqual(records, [])
        self.assertEqual(verify.call_count, 1)
        self.assertTrue((self.output / b.RECEIPT).is_file())

    def test_same_length_corrupt_later_asset_refuses_before_any_network(self):
        anchor, verify = self.bootstrap(), mock.Mock()
        self.download_stage(anchor)
        b.write_new(self.output / "release.tar.gz", b"x" * len(self.assets["package"]))
        before = self.snapshot()
        with self.http_transport() as records, self.assertRaises(b.BootstrapError):
            b.prepare(anchor, self.output, b.download, verify)
        self.assertEqual(records, [])
        verify.assert_not_called()
        self.assertEqual(before, self.snapshot())

    def test_partial_assets_reject_links_special_files_permissions_and_oversize(self):
        anchor = self.bootstrap()
        source = self.root / "original"
        b.write_new(source, b"partial")
        for kind in ("symlink", "hardlink", "fifo", "directory", "writable", "oversized"):
            with self.subTest(kind=kind):
                self.output = self.root / kind
                self.download_stage(anchor)
                target = self.output / "uniroute-upgrade"
                if kind == "symlink": target.symlink_to(source)
                elif kind == "hardlink": os.link(source, target)
                elif kind == "fifo": os.mkfifo(target, 0o600)
                elif kind == "directory": target.mkdir(mode=0o700)
                else:
                    b.write_new(target, b"partial" if kind == "writable" else b"x"*(len(self.assets["verifier"])+1))
                    if kind == "writable": target.chmod(0o666)
                fetch, verify = mock.Mock(), mock.Mock()
                with self.assertRaises((b.BootstrapError, OSError)):
                    b.prepare(anchor, self.output, fetch, verify)
                fetch.assert_not_called()
                verify.assert_not_called()

    def test_mixed_bootstrap_extraction_remnants_and_concurrent_prepare_do_not_fetch(self):
        anchor = self.bootstrap()
        self.download_stage(anchor)
        fetch, verify = mock.Mock(), mock.Mock()
        other = self.bootstrap(lambda value:value["release"].update(version="1.11.1"))
        with self.assertRaises(b.BootstrapError): b.prepare(other, self.output, fetch, verify)
        with b.preparation_lock(self.output), self.assertRaises(b.BootstrapError):
            b.prepare(anchor, self.output, fetch, verify)
        (self.output / "payload").mkdir(mode=0o700)
        with self.assertRaises(b.BootstrapError): b.prepare(anchor, self.output, fetch, verify)
        fetch.assert_not_called()
        verify.assert_not_called()

    def test_foreign_partial_file_owner_is_refused_before_network(self):
        anchor = self.bootstrap()
        self.download_stage(anchor)
        target = self.output / "release.tar.gz"
        b.write_new(target, self.assets["package"][:37])
        inode, original_fstat = target.stat().st_ino, os.fstat
        def foreign(fd):
            value = original_fstat(fd)
            if value.st_ino == inode:
                changed = list(value)
                changed[4] = os.geteuid() + 12345
                return os.stat_result(changed)
            return value
        fetch, verify = mock.Mock(), mock.Mock()
        with mock.patch.object(b.os, "fstat", side_effect=foreign), self.assertRaises(b.BootstrapError):
            b.prepare(anchor, self.output, fetch, verify)
        fetch.assert_not_called()
        verify.assert_not_called()

    def test_replaced_partial_descriptor_is_not_appended(self):
        raw, prefix = self.assets["package"], self.assets["package"][:37]
        target = self.root / "partial"
        b.write_new(target, prefix)
        class Response(io.BytesIO):
            status = 206
            headers = {"Content-Length":str(len(raw)-len(prefix)),
                       "Content-Range":f"bytes {len(prefix)}-{len(raw)-1}/{len(raw)}"}
            def geturl(self): return "https://example.invalid/package"
        retained = self.root / "retained"
        def replace(*args, **kwargs):
            target.rename(retained)
            b.write_new(target, b"z" * len(prefix))
            return Response(raw[len(prefix):])
        with mock.patch.object(b.urllib.request, "build_opener") as opener:
            opener.return_value.open.side_effect = replace
            with self.assertRaises(b.BootstrapError): b.download(self.bootstrap().assets["package"], target)
        self.assertEqual(retained.read_bytes(), prefix)
        self.assertEqual(target.read_bytes(), b"z" * len(prefix))

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
            opener.return_value.open.reset_mock()
            b.download(asset,self.root/'download')
            opener.return_value.open.assert_not_called()
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
