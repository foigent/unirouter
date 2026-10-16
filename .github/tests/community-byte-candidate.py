#!/usr/bin/env python3
"""One manual, unsigned byte candidate; native Docker + native draft Release assets.

This is an internal validation producer, not an installer or release publisher.
Existing image/source observers and their validators are imported byte-for-byte.
No retry, resume, replacement, publication, registry push or signing operation.
"""
import base64
import contextlib
import gzip
import hashlib
import io
import json
import os
import platform
import re
import shutil
import stat
import sys
import tarfile
import time
import types
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path, PurePosixPath

MIB = 1024 * 1024
ASSET_LIMIT = 1024 * MIB
METADATA_LIMIT = 32 * MIB
REPO = 'foigent/unirouter'
REPO_ID = 1281228065
ACTOR_ID = 252312344
PRODUCT = '3818ba9627d1bc44eb836036b8ae8904a7175e65'
TREE = '14832e89a5a4cece7ed2efd51b98ce984dabba39'
WORKFLOW = '.github/workflows/community-byte-candidate.yml'
API = 'https://api.github.com'
PREFIX = '/repos/' + REPO
ROOT = Path(__file__).resolve().parents[2]
INPUTS = ROOT / '.github/release-inputs'
BOUND = {
    'community-image-candidate.py': 'b379b8c3fc767f4837d18fffe098e9432f4ddf5767152c80f77c8e6fb73c99c3',
    'community-source-evidence.py': 'f5cb3ea9875e37306aab73553f7f03ad408e01b418814c89a20589410d7a00ed',
    'frontend-membership-observer.mjs': 'bc34e95b3a76c8a8f9a5c66f59845d63f53d237938e96ded84ab30039ee2d130',
    'component-notices.tar.gz': '1c82db604fe54e6a076723374e80cbf066b55d46b6880b6b630237324fb3913a',
    'community-public-keys.json': '96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d',
}
ASSETS = ('backend-image.tar.gz', 'frontend-image.tar.gz',
          'uniroute-upgrade-linux-amd64', 'candidate-evidence.tar.gz', 'SHA256SUMS')
MARKER = 'private-channel-check.txt'
EXCEPTIONS = frozenset(('Stop', 'ValueError', 'KeyError', 'TypeError', 'FileNotFoundError',
    'FileExistsError', 'PermissionError', 'TimeoutError', 'ConnectionError', 'URLError',
    'HTTPError', 'OSError', 'IncompleteRead', 'RemoteDisconnected', 'CommandTimeout',
    'TimeoutExpired', 'BrokenPipeError', 'JSONDecodeError', 'UnicodeDecodeError', 'SSLError', 'OtherException'))
MODULE_ROLES = {'community-byte-candidate.py': 'byte_producer',
    'community-image-candidate.py': 'accepted_image_guard',
    'community-source-evidence.py': 'accepted_source_observer'}
COMMAND_PHASES = frozenset((
    'producer checkout identity', 'local Docker environment', 'source commit', 'source tree',
    'export tracked source', 'one retained backend build', 'one retained frontend build',
    'actual final frontend membership', 'source evidence exact stopped copy',
    'one offline Linux verifier build', 'backend stopped byte inventory', 'frontend stopped byte inventory',
    'backend native image save', 'frontend native image save', 'cleanup ownership',
    'stop owned tool process', 'remove owned container and anonymous volumes', 'other_owned_command',
    *[role+' '+suffix for role in ('backend-builder', 'frontend-builder', 'backend-runtime', 'frontend-runtime')
      for suffix in ('pull', 'image inspect')],
    *[role+' image inspect' for role in ('backend', 'frontend', 'verifier-tool')],
    *[role+' '+suffix for role in ('byte-go-version', 'byte-node-version', 'backend-byte-inventory',
      'frontend-byte-inventory', 'actual-backend-buildinfo', 'verifier-output', 'verifier-buildinfo')
      for suffix in ('create', 'container identity', 'version/readback')]))


class Stop(Exception):
    """Constant, non-sensitive reason code only; never an HTTP response/URL."""


def need(ok, code):
    if not ok:
        raise Stop(code)


def sha(body):
    return hashlib.sha256(body).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=True, indent=2,
                       allow_nan=False) + '\n').encode()


def load(body):
    def pairs(items):
        value = {}
        for key, item in items:
            need(key not in value, 'duplicate_json_key')
            value[key] = item
        return value
    return json.loads(body, object_pairs_hook=pairs,
                      parse_constant=lambda _: need(False, 'json_constant'))


def regular(path, cap):
    st = path.lstat()
    need(stat.S_ISREG(st.st_mode) and st.st_nlink == 1 and st.st_size <= cap,
         'regular_file_limit')
    with path.open('rb') as stream:
        need(stamp(os.fstat(stream.fileno())) == stamp(st), 'file_changed')
        body = stream.read(cap + 1)
    need(len(body) == st.st_size and stamp(path.lstat()) == stamp(st), 'file_changed')
    return body


def write_new(path, body):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as out:
        out.write(body)


def file_fact(path):
    st = path.lstat()
    need(stat.S_ISREG(st.st_mode) and st.st_nlink == 1, 'asset_not_regular')
    h = hashlib.sha256()
    with path.open('rb') as inp:
        need(stamp(os.fstat(inp.fileno())) == stamp(st), 'file_changed')
        for chunk in iter(lambda: inp.read(65536), b''):
            h.update(chunk)
    need(stamp(path.lstat()) == stamp(st), 'file_changed')
    return {'bytes': st.st_size, 'sha256': h.hexdigest()}


def stamp(st):
    return (st.st_dev, st.st_ino, st.st_mode, st.st_nlink, st.st_size,
            st.st_mtime_ns, st.st_ctime_ns)


def safe_name(name):
    need(isinstance(name, str) and len(name) <= 1024 and name
         and not PurePosixPath(name).is_absolute() and '\\' not in name
         and all(p not in ('', '.', '..') for p in name.split('/'))
         and all(ord(c) >= 32 for c in name), 'unsafe_path')
    return name


def module(name, path):
    raw = regular(path, 128 * 1024)
    need(sha(raw) == BOUND[path.name], 'observer_identity')
    result = types.ModuleType(name)
    result.__file__ = str(path.resolve())
    exec(compile(raw, str(path), 'exec'), result.__dict__)
    return result


def failure_diagnostic(error, guard=None):
    """Closed structural location facts. Never exception text, filenames or URLs."""
    frames = []; trace = error.__traceback__
    while trace is not None:
        role = MODULE_ROLES.get(Path(trace.tb_frame.f_code.co_filename).name)
        if role is not None:
            frames.append({'module_role': role, 'line': trace.tb_lineno})
        trace = trace.tb_next
    name = type(error).__name__
    number = getattr(error, 'errno', None)
    command = None
    if guard is not None and guard.receipt.get('commands'):
        rows = guard.receipt['commands']; row = rows[-1]; phase = row.get('phase')
        code = row.get('exit_code')
        command = {'index': len(rows), 'phase': phase if phase in COMMAND_PHASES else 'other_owned_command',
                   'exit_code': code if type(code) is int else None}
    return {'exception_class': name if name in EXCEPTIONS else 'OtherException',
        'errno': number if type(number) is int and -4096 <= number <= 4096 else None,
        'locations': frames[-4:], 'last_owned_command': command}


def validate_diagnostic(value):
    need(set(value) == {'exception_class', 'errno', 'locations', 'last_owned_command'}
         and value['exception_class'] in EXCEPTIONS, 'diagnostic_schema')
    need(value['errno'] is None or (type(value['errno']) is int and -4096 <= value['errno'] <= 4096),
         'diagnostic_errno')
    need(type(value['locations']) is list and len(value['locations']) <= 4, 'diagnostic_locations')
    for row in value['locations']:
        need(set(row) == {'module_role', 'line'} and row['module_role'] in MODULE_ROLES.values()
             and type(row['line']) is int and 0 < row['line'] < 100000, 'diagnostic_location')
    command = value['last_owned_command']
    if command is not None:
        need(set(command) == {'index', 'phase', 'exit_code'} and command['phase'] in COMMAND_PHASES
             and type(command['index']) is int and 0 < command['index'] < 10000
             and (command['exit_code'] is None or type(command['exit_code']) is int), 'diagnostic_command')
    return value


def bound_inputs(root=ROOT):
    tests = root / '.github/tests'
    inputs = root / '.github/release-inputs'
    for name, digest in BOUND.items():
        path = (inputs if name.endswith(('.gz', '.json')) else tests) / name
        need(sha(regular(path, 2 * MIB)) == digest, 'input_identity')
    ring = regular(inputs / 'community-public-keys.json', 1024)
    keys = load(ring)
    need(list(keys) == ['ur-community-upgrade-20261011-a'], 'ring_key_scope')
    key = base64.b64decode(next(iter(keys.values())), validate=True)
    need(len(key) == 32 and sha(key) ==
         '765f38f8a4a4f38bf7e94aa3236c28f74dccac56431ab3a1d035a880f6086116', 'ring_key')
    raw = regular(inputs / 'component-notices.tar.gz', MIB)
    files = {}; total = 0
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as archive:
        for member in archive:
            name = safe_name(member.name)
            need(member.isfile() and name not in files and len(files) < 256,
                 'notice_archive_member')
            total += member.size
            need(0 <= member.size <= 2*MIB and total <= 8*MIB, 'notice_archive_limit')
            files[name] = archive.extractfile(member).read(member.size+1)
            need(len(files[name]) == member.size, 'notice_archive_truncated')
    index = load(files['index.json'])
    need(index['product_source']['commit'] == PRODUCT
         and index['product_source']['tree'] == TREE, 'notice_source')
    for row in index['components']:
        for material in row['materials']:
            body = files[material['file']]
            need((len(body), sha(body)) == (material['bytes'], material['sha256']),
                 'notice_body_binding')
    return base64.urlsafe_b64encode(ring).decode().rstrip('='), index


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class NativeHTTP:
    """Native environment proxy support; no redirect or retry; URLs never logged."""
    def __init__(self, guard=lambda: None):
        self.guard = guard
        # Standard environment proxy support remains; no shared configuration writes.
        self.opener = urllib.request.build_opener(NoRedirect())

    def request(self, method, url, headers, data, receive):
        self.guard()
        request = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            response = self.opener.open(request, timeout=25)
        except urllib.error.HTTPError as response:
            with response:
                return receive(response.code, response.headers, response)
        with response:
            return receive(response.status, response.headers, response)


class GitHub:
    def __init__(self, token, transport=None, guard=lambda: None):
        need(bool(token), 'app_token_missing')
        self._token = token
        self.transport = transport or NativeHTTP(guard)
        self.guard = guard
        self.observations = []

    def request(self, method, path, body=None, anonymous=False, upload=None):
        self.guard()
        need(method in ('GET', 'POST') and path.startswith('/'), 'http_contract')
        headers = {'Accept': 'application/vnd.github+json',
                   'X-GitHub-Api-Version': '2026-03-10', 'User-Agent': 'ur-byte-candidate'}
        if not anonymous:
            headers['Authorization'] = 'Bearer ' + self._token
        data = encoded(body) if body is not None else None
        url = API + path
        if data is not None:
            headers['Content-Type'] = 'application/json'
        if upload is not None:
            url, data = upload
            need(re.fullmatch(r'https://uploads\.github\.com/repos/foigent/unirouter/'
                 r'releases/[0-9]+/assets\?name=[A-Za-z0-9._-]+', url), 'upload_target')
            headers['Content-Type'] = 'application/octet-stream'
            headers['Content-Length'] = str(os.fstat(data.fileno()).st_size)
            original = data
            guard = self.guard
            class BoundedUpload:
                def read(self, size=65536):
                    guard()
                    return original.read(min(size if size >= 0 else 65536, 65536))
            data = BoundedUpload()

        def receive(status, response_headers, stream):
            raw = stream.read(2*MIB + 1)
            need(len(raw) <= 2*MIB, 'api_response_limit')
            self.observations.append({'method': method, 'path': path.split('?')[0],
                                      'anonymous': anonymous, 'status': status,
                                      'bytes': len(raw), 'sha256': sha(raw)})
            # Error bodies/headers/exceptions are never projected into evidence.
            return status, load(raw) if status in (200, 201) else None
        return self.transport.request(method, url, headers, data, receive)

    def download(self, url, destination=None, maximum=ASSET_LIMIT,
                 authenticated=True, require_absent=False):
        self.guard()
        if authenticated:
            need(re.fullmatch(re.escape(API+PREFIX) + r'/releases/assets/[0-9]+', url),
                 'download_api_target')
        else:
            need(url.startswith(API+PREFIX+'/releases/assets/')
                 or re.fullmatch(r'https://github\.com/foigent/unirouter/releases/download/'
                                 r'[A-Za-z0-9._-]+/[A-Za-z0-9._-]+', url),
                 'anonymous_download_target')
        headers = {'Accept': 'application/octet-stream', 'User-Agent': 'ur-byte-candidate',
                   'X-GitHub-Api-Version': '2026-03-10'}
        if authenticated:
            headers['Authorization'] = 'Bearer '+self._token

        def receive(status, rh, stream):
            if status == 302:
                need(not require_absent, 'anonymous_redirect_exposed')
                location = rh.get('Location', '')
                parsed = urllib.parse.urlsplit(location)
                need(parsed.scheme == 'https' and parsed.hostname in
                     ('release-assets.githubusercontent.com', 'objects.githubusercontent.com')
                     and parsed.port in (None, 443) and parsed.username is None
                     and parsed.password is None and not parsed.fragment,
                     'signed_download_host')
                # Signed URLs stay in memory. Authorization is deliberately dropped.
                return self.transport.request('GET', location,
                    {'User-Agent': 'ur-byte-candidate'}, None, consume)
            return consume(status, rh, stream)

        def consume(status, rh, stream):
            if require_absent:
                need(status == 404, 'anonymous_access_not_denied')
                return None
            need(status == 200 and destination is not None, 'download_status')
            length = rh.get('Content-Length')
            if length is not None:
                need(length.isdecimal() and int(length) <= maximum, 'download_limit')
            fd = os.open(destination, os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600)
            total = 0; h = hashlib.sha256()
            with os.fdopen(fd, 'wb') as out:
                while True:
                    self.guard(); chunk = stream.read(min(65536, maximum-total+1))
                    if not chunk:
                        break
                    total += len(chunk); need(total <= maximum, 'download_limit')
                    h.update(chunk); out.write(chunk)
            need(length is None or total == int(length), 'download_truncated')
            return {'bytes': total, 'sha256': h.hexdigest()}
        return self.transport.request('GET', url, headers, None, receive)


class Draft:
    """One candidate, exclusive local intents; uncertain writes are never repeated."""
    def __init__(self, api, work, producer, run_id):
        self.api = api; self.work = work; self.producer = producer
        need(re.fullmatch('[0-9]{1,24}', run_id), 'run_id')
        self.tag = 'ur-byte-candidate-'+run_id+'-attempt1'
        self.release = None; self.bot = None; self.expected = {}
        self.verified = set()
        self.last_diagnostic = None
        self.effects = []; self.blocked = False; self.marker_verified = False
        self.tag_before = None; self.tag_after = None

    def get(self, path, anonymous=False):
        status, value = self.api.request('GET', path, anonymous=anonymous)
        need(status == 200, 'api_read_status')
        return value

    def repository(self):
        repo = self.get(PREFIX)
        need(repo['id'] == REPO_ID and repo['full_name'] == REPO
             and repo['private'] is False and repo['default_branch'] == 'main', 'repository')
        head = self.get(PREFIX+'/git/ref/heads/main')['object']
        need(head['type'] == 'commit' and head['sha'] == self.producer, 'default_head_drift')

    def ref(self):
        status, value = self.api.request('GET', PREFIX+'/git/ref/tags/'+self.tag)
        need(status in (200, 404), 'tag_read_status')
        if status == 404:
            return {'present': False}
        obj = value['object']; target = obj['sha']
        if obj['type'] == 'tag':
            target_obj = self.get(PREFIX+'/git/tags/'+target)['object']
            need(target_obj['type'] == 'commit', 'tag_target_type')
            target = target_obj['sha']
        else:
            need(obj['type'] == 'commit', 'tag_target_type')
        need(target == self.producer, 'tag_target_drift')
        return {'present': True, 'object_type': obj['type'], 'object_sha': obj['sha'],
                'commit': target}

    def drafts(self):
        result = []; total = 0
        for page in range(1, 1001):
            rows = self.get(PREFIX+'/releases?per_page=100&page='+str(page))
            need(isinstance(rows, list), 'release_listing')
            total += len(encoded(rows)); need(total <= 16*MIB, 'release_listing_limit')
            result.extend(row for row in rows if row.get('tag_name') == self.tag)
            if len(rows) < 100:
                return result
        raise Stop('release_listing_limit')

    def validate(self, row):
        need(type(row.get('id')) is int and row['id'] > 0 and row['draft'] is True
             and row['published_at'] is None and row['tag_name'] == self.tag
             and row['target_commitish'] == self.producer
             and row['author']['id'] == self.bot
             and row['url'] == API+PREFIX+'/releases/'+str(row['id']), 'draft_identity')
        upload = 'https://uploads.github.com'+PREFIX+'/releases/'+str(row['id'])+'/assets{?name,label}'
        need(row['upload_url'] == upload, 'native_upload_host')
        return row

    def fresh(self):
        need(not self.blocked and self.release is not None, 'draft_writes_blocked')
        self.repository()
        row = self.validate(self.get(PREFIX+'/releases/'+str(self.release['id'])))
        self.tag_after = self.ref()
        assets = row['assets']
        need(isinstance(assets, list) and len(assets) == len(self.expected)
             and len({a['name'] for a in assets}) == len(assets), 'asset_set')
        for asset in assets:
            need(asset['name'] in self.expected
                 and asset['id'] == self.expected[asset['name']]['id'], 'foreign_asset')
            self.asset_identity(asset, asset['name'], self.expected[asset['name']]['fact'])
        return row

    def mutate(self, label, callback):
        need(not self.blocked, 'draft_writes_blocked')
        # A preexisting intent is an uncertain prior attempt, never permission to retry.
        try:
            write_new(self.work/('intent-'+label+'.json'), encoded({'action': label, 'tag': self.tag}))
        except FileExistsError:
            self.blocked = True; raise Stop('existing_write_intent') from None
        try:
            return callback()
        except Exception as error:
            self.blocked = True
            self.last_diagnostic = failure_diagnostic(error)
            reason = str(error) if isinstance(error, Stop) else type(error).__name__
            try:
                matches = self.drafts()
                self.effects.append({'uncertain_action': label,
                    'matching_draft_ids': [r.get('id') for r in matches],
                    'matching_asset_names': [[a.get('name') for a in r.get('assets', [])] for r in matches],
                    'tag_observation': self.ref(), 'writes_stopped': True, 'reason': reason})
            except Exception:
                self.effects.append({'uncertain_action': label, 'inspection_incomplete': True,
                                     'writes_stopped': True, 'reason': reason})
            raise Stop('uncertain_or_rejected_write') from None

    def create(self):
        self.repository()
        bot = self.get('/users/github-actions%5Bbot%5D')
        need(bot['login'] == 'github-actions[bot]' and bot['type'] == 'Bot'
             and type(bot['id']) is int, 'app_bot_identity')
        self.bot = bot['id']
        before = self.ref()
        self.tag_before = before
        need(not before['present'] and not self.drafts(), 'candidate_already_exists')
        def post():
            status, row = self.api.request('POST', PREFIX+'/releases', {
                'tag_name': self.tag, 'target_commitish': self.producer,
                'name': 'Unsigned byte candidate '+self.tag,
                'body': 'Internal byte retention candidate. Not signed, published, installable, or license cleared.',
                'draft': True, 'prerelease': True, 'make_latest': 'false',
                'generate_release_notes': False})
            need(status == 201, 'draft_create_status')
            self.release = self.validate(row)
            self.effects.append({'create_readback': self.ref(), 'tag_before': before,
                                 'draft_id': row['id']})
            self.fresh()
            return row
        return self.mutate('create', post)

    def asset_identity(self, row, name, fact):
        need(type(row['id']) is int and row['id'] > 0 and row['name'] == name
             and row['state'] == 'uploaded' and row['size'] == fact['bytes']
             and row['uploader']['id'] == self.bot
             and row['url'] == API+PREFIX+'/releases/assets/'+str(row['id'])
             and re.fullmatch(r'https://github\.com/foigent/unirouter/releases/download/'
                              r'[A-Za-z0-9._-]+/'+re.escape(name), row['browser_download_url']), 'asset_identity')
        if name in self.expected:
            expected = self.expected[name]
            need(row['id'] == expected['id'] and row['browser_download_url'] ==
                 expected['row']['browser_download_url'], 'asset_readback_identity')
        digest = row.get('digest')
        need(digest in (None, 'sha256:'+fact['sha256']), 'asset_server_digest')

    def privacy(self, asset):
        control = self.get(PREFIX, anonymous=True)
        need(control['id'] == REPO_ID and control['private'] is False, 'anonymous_control')
        status, _ = self.api.request('GET', PREFIX+'/releases/'+str(self.release['id']), anonymous=True)
        need(status == 404, 'anonymous_release_not_denied')
        for url in (asset['url'], asset['browser_download_url']):
            self.api.download(url, authenticated=False, require_absent=True)

    def upload(self, path):
        try:
            return self._upload(path)
        except Exception as error:
            self.blocked = True
            if self.last_diagnostic is None:
                self.last_diagnostic = failure_diagnostic(error)
            raise

    def _upload(self, path):
        name = path.name
        need(name in ASSETS+(MARKER,) and (name == MARKER or self.marker_verified), 'marker_required')
        self.fresh()
        need(name not in self.expected, 'asset_name_collision')
        fact = file_fact(path)
        need(sum(x['fact']['bytes'] for x in self.expected.values()) + fact['bytes'] <= ASSET_LIMIT,
             'aggregate_asset_limit')
        # Recheck audience immediately before each binary/metadata upload.
        if name != MARKER:
            self.privacy(self.expected[MARKER]['row'])
        upload = self.release['upload_url'].removesuffix('{?name,label}')+'?name='+urllib.parse.quote(name, safe='')
        def post():
            with path.open('rb') as stream:
                need(os.fstat(stream.fileno()).st_size == fact['bytes'], 'file_changed')
                status, row = self.api.request('POST', PREFIX+'/releases/'+str(self.release['id'])+'/assets',
                                                upload=(upload, stream))
            need(status == 201, 'asset_upload_status')
            self.asset_identity(row, name, fact)
            self.expected[name] = {'id': row['id'], 'fact': fact, 'row': row}
            actual = self.get(PREFIX+'/releases/assets/'+str(row['id']))
            self.asset_identity(actual, name, fact)
            self.fresh(); self.privacy(actual)
            # Readback copy is task-owned and removed only after exact digest match.
            copy = self.work/('readback-'+name)
            read = self.api.download(actual['url'], copy, maximum=fact['bytes'])
            need(read == fact and file_fact(path) == fact, 'asset_readback_mismatch')
            copy.unlink()
            self.verified.add(name)
            self.effects.append({'asset_id': row['id'], 'name': name, **fact,
                                 'anonymous_denied': True, 'readback_matched': True})
            if name == MARKER:
                self.marker_verified = True
        self.mutate('upload-'+name, post)


def runtime_identity(env, event):
    producer = env.get('EXPECTED_WORKFLOW_COMMIT', '')
    need(re.fullmatch('[0-9a-f]{40}', producer)
         and producer == env.get('GITHUB_SHA') == env.get('GITHUB_WORKFLOW_SHA'), 'producer_sha')
    need(env.get('GITHUB_REPOSITORY') == REPO and env.get('GITHUB_REPOSITORY_ID') == str(REPO_ID)
         and env.get('GITHUB_EVENT_NAME') == 'workflow_dispatch'
         and env.get('GITHUB_REF') == 'refs/heads/main'
         and env.get('GITHUB_RUN_ATTEMPT') == '1'
         and env.get('GITHUB_ACTOR_ID') == str(ACTOR_ID)
         and env.get('GITHUB_WORKFLOW_REF') == REPO+'/'+WORKFLOW+'@refs/heads/main', 'workflow_identity')
    need(event['repository']['id'] == REPO_ID
         and event['repository']['full_name'] == REPO
         and event['sender']['id'] == ACTOR_ID, 'event_identity')
    need(env.get('RUNNER_ENVIRONMENT') == 'github-hosted'
         and env.get('RUNNER_OS') == 'Linux' and env.get('RUNNER_ARCH') == 'X64', 'runner_identity')
    need(not any(env.get(k) for k in ('DOCKER_HOST', 'DOCKER_CONTEXT', 'DOCKER_TLS_VERIFY',
         'DOCKER_CERT_PATH', 'BUILDKIT_HOST', 'BUILDX_BUILDER')), 'remote_docker_context')
    need(re.fullmatch('[0-9a-f]{64}', env.get('ADMISSION_SHA256', '')), 'external_admission_binding')
    return producer


def initialize_guard(guard, work, env):
    need(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'native_machine')
    guard.work = work; guard.run_id = env['GITHUB_RUN_ID']
    clock = env.get('UR_BYTE_JOB_MONOTONIC', '')
    need(re.fullmatch(r'[0-9]+(?:\.[0-9]+)?', clock), 'job_clock')
    start = float(clock); need(0 < start <= time.monotonic(), 'job_clock')
    guard.deadline = start + 38*60; guard.cleanup_deadline = start + 40*60
    guard.time_guard()
    config = work/'docker-config'; config.mkdir(mode=0o700)
    write_new(config/'config.json', b'{"auths":{}}\n')
    guard.child_env = {'PATH': env['PATH'], 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
        'TZ': 'UTC', 'DOCKER_CONFIG': str(config), 'DOCKER_BUILDKIT': '1', 'TMPDIR': str(work),
        'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_ATTR_NOSYSTEM': '1'}
    need(Path(env['GITHUB_WORKSPACE']).resolve()/'producer' == ROOT, 'workspace')
    need(guard.argv_run('producer checkout identity', ['git', '-C', str(ROOT), 'rev-parse', 'HEAD'])
         == env['EXPECTED_WORKFLOW_COMMIT'], 'producer_checkout')
    guard.SOURCE_CHECKOUT = ROOT/'.byte-source-3818'
    info = load(guard.docker('local Docker environment', 'info', '--format',
        '{"OSType":{{json .OSType}},"Architecture":{{json .Architecture}},'
        '"SecurityOptions":{{json .SecurityOptions}},"DockerRootDir":{{json .DockerRootDir}}}'))
    need(info['OSType'] == 'linux' and info['Architecture'] in ('amd64', 'x86_64')
         and not any('rootless' in x for x in info['SecurityOptions']), 'docker_environment')
    guard.docker_root = Path(info['DockerRootDir'])
    need(guard.docker_root.is_absolute() and guard.docker_root.is_dir(), 'docker_root')
    guard.disk_guard(initial=True)


def derive_recipes(guard, source):
    """Only temporary source export is modified. Actual product Git identity stays fixed."""
    back, front = guard.recipes(source)
    root = source/'frontend'; instrument = root/'.ur-source-evidence'
    instrument.mkdir(mode=0o700)
    write_new(instrument/'membership-observer.mjs', regular(Path(__file__).with_name(
        'frontend-membership-observer.mjs'), 128*1024))
    write_new(instrument/'original-package.json', regular(root/'package.json', MIB))
    write_new(instrument/'original-package-lock.json', regular(root/'package-lock.json', MIB))
    write_new(instrument/'vite.config.ts', b"import { mergeConfig } from 'vite';\n"
        b"import original from '../vite.config';\n"
        b"import { membershipObserver } from './membership-observer.mjs';\n"
        b"export default mergeConfig(original, { plugins: [membershipObserver()] });\n")
    for target in (source/'.ur-byte-inputs', root/'.ur-byte-inputs'):
        target.mkdir(mode=0o700)
        write_new(target/'component-notices.tar.gz', regular(INPUTS/'component-notices.tar.gz', MIB))
        for name in ('LICENSE', 'NOTICE'):
            write_new(target/name, regular(source/name, MIB))
    btext = back.read_text()
    ftext = front.read_text()
    need(btext.count('COPY migrations/ ./migrations/') == 1
         and ftext.count('RUN npm run build') == 1
         and ftext.count('COPY nginx.conf /etc/nginx/conf.d/default.conf') == 1, 'recipe_shape')
    btext = btext.replace('COPY migrations/ ./migrations/',
        'COPY migrations/ ./migrations/\nCOPY .ur-byte-inputs/ /usr/share/licenses/unirouter/')
    command = ('RUN npm run check:toolchain && ./node_modules/.bin/tsc && '
        'npm run check:api-contracts && npm run check:react-compat && '
        './node_modules/.bin/vite build --config .ur-source-evidence/vite.config.ts && '
        'node scripts/check-bundle-budget.mjs')
    ftext = ftext.replace('RUN npm run build', command).replace(
        'COPY nginx.conf /etc/nginx/conf.d/default.conf',
        'COPY nginx.conf /etc/nginx/conf.d/default.conf\n'
        'COPY .ur-byte-inputs/ /usr/share/licenses/unirouter/\n'
        'COPY --from=builder /app/.ur-source-evidence/output /usr/share/licenses/unirouter/output')
    # Native recipes() owns these exact new temporary files; not repository files.
    back.write_text(btext); front.write_text(ftext)
    return back, front, {'backend': file_fact(back), 'frontend': file_fact(front),
        'changes': ['existing fixed FROM substitutions', 'accepted frontend observer and same production/1.12.0 arguments',
                    'retained notices/LICENSE/NOTICE in non-served runtime directory',
                    'actual frontend observer output retained in same final image'],
        'not_claimed_byte_equal_to_old_ephemeral_images': True}


def inventory(guard, source_helper, role, image, data):
    """Bounded stopped rootfs walk. Never extract/follow links or start app services."""
    cid = guard.create(role+'-byte-inventory', image)
    rows = []; dbs = {}; notices = []; count = total = 0
    binary = guard.work/'actual-backend-binary' if role == 'backend' else None
    seen = set()
    def receive(stream):
        nonlocal count, total
        reader = source_helper.BoundedReader(stream, 600*MIB, guard.time_guard)
        with tarfile.open(fileobj=reader, mode='r|') as archive:
            for member in archive:
                guard.time_guard(); name = safe_name(member.name.rstrip('/') if member.isdir() else member.name)
                need(name not in seen, 'rootfs_duplicate'); seen.add(name)
                count += 1; need(count <= 50000, 'rootfs_entries')
                row = {'path': name, 'mode': member.mode, 'type': 'other'}
                if member.isdir():
                    row['type'] = 'directory'
                elif member.issym() or member.islnk():
                    row.update(type='symlink' if member.issym() else 'hardlink', target=member.linkname)
                elif member.isfile():
                    need(member.size >= 0, 'rootfs_size'); total += member.size
                    need(total <= 512*MIB, 'rootfs_bytes')
                    basename = PurePosixPath(name).name
                    candidate = bool(re.fullmatch(r'(?:LICEN[CS]E|COPYING|COPYRIGHT|NOTICE|AUTHORS)'
                        r'(?:[._-][A-Za-z0-9._-]+)?', basename, flags=re.I))
                    candidate = candidate and not basename.lower().endswith((
                        '.js', '.mjs', '.cjs', '.jsx', '.ts', '.tsx', '.css', '.scss', '.map',
                        '.json', '.sql', '.go', '.py', '.rb', '.php', '.c', '.h', '.cpp',
                        '.sh', '.html', '.wasm', '.gz', '.bz2', '.zip', '.tar'))
                    database = name in ('lib/apk/db/installed', 'var/lib/dpkg/status')
                    keep = database or (candidate and member.size <= MIB)
                    if database:
                        need(member.size <= 16*MIB, 'database_limit')
                    buf = bytearray(); h = hashlib.sha256(); out = None
                    if binary is not None and name == 'app/uniroute':
                        out = binary.open('xb'); os.chmod(binary, 0o644)
                    try:
                        with archive.extractfile(member) as inp:
                            for chunk in iter(lambda: inp.read(65536), b''):
                                guard.time_guard(); h.update(chunk)
                                if keep: buf.extend(chunk)
                                if out: out.write(chunk)
                    finally:
                        if out: out.close()
                    row.update(type='regular', bytes=member.size, sha256=h.hexdigest())
                    if database:
                        dbs[name] = bytes(buf)
                        write_new(data/role/'databases'/name, bytes(buf))
                    if candidate:
                        notice = {'path': name, 'bytes': member.size, 'sha256': h.hexdigest(), 'retained_text': None}
                        if keep and b'\x00' not in buf:
                            try: bytes(buf).decode('utf-8')
                            except UnicodeDecodeError: pass
                            else:
                                rel = role+'/image-notices/'+h.hexdigest()+'.txt'
                                target = data/rel
                                if not target.exists(): write_new(target, bytes(buf))
                                notice['retained_text'] = rel
                        notices.append(notice)
                rows.append(row)
    guard.stream_command(role+' stopped byte inventory', ['docker', '--config', str(guard.work/'docker-config'),
        '--host', 'unix:///var/run/docker.sock', 'export', cid], receive)
    report = {'image_id': image, 'entries': rows, 'regular_bytes': total,
        'apk': guard.apk_packages(dbs['lib/apk/db/installed']) if 'lib/apk/db/installed' in dbs else [],
        'dpkg_unparsed': 'var/lib/dpkg/status' in dbs, 'notice_filename_candidates': notices,
        'limits_of_claim': ['stopped Docker export excludes mounted volumes', 'links are recorded and never followed',
            'notice filename/text candidates are not a licensing clearance', 'blank license declarations remain blank']}
    raw = encoded(report); need(len(raw) <= 8*MIB, 'inventory_metadata_limit')
    write_new(data/(role+'-rootfs.json'), raw)
    return cid, binary, report


def saved_image(guard, role, image, assets, remaining):
    native = guard.work/(role+'-native-image.tar')
    guard.docker(role+' native image save', 'save', '--output', str(native), image)
    # Native Docker archive is retained as bytes, not reconstructed OCI metadata.
    original = file_fact(native)
    need(original['bytes'] <= 2*ASSET_LIMIT, 'native_save_limit')
    destination = assets/(role+'-image.tar.gz')
    fd = os.open(destination, os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600)
    with native.open('rb') as inp, os.fdopen(fd, 'wb') as out:
        with gzip.GzipFile(filename='', mode='wb', fileobj=out, mtime=0, compresslevel=6) as zipped:
            for chunk in iter(lambda: inp.read(65536), b''):
                guard.time_guard(); guard.disk_guard(); zipped.write(chunk)
                need(out.tell() <= remaining, 'aggregate_asset_limit')
        need(out.tell() <= remaining, 'aggregate_asset_limit')
    need(file_fact(native) == original, 'native_save_changed')
    return {'image_id': image, 'native_docker_save': original, 'compressed': file_fact(destination)}


def verifier(guard, source_helper, source, go, assets, data):
    recipe = guard.work/'verifier.Dockerfile'
    write_new(recipe, ('FROM '+guard.FIXED['images']['backend-builder']['pull_reference']+'\n'
        'WORKDIR /src\nCOPY go.mod go.sum ./\n'
        'COPY cmd/uniroute-upgrade/ cmd/uniroute-upgrade/\n'
        'COPY internal/signing/ internal/signing/\n'
        'COPY internal/upgradebundle/ internal/upgradebundle/\n'
        'RUN GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off '
        'CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false '
        '-mod=readonly -o /out/uniroute-upgrade ./cmd/uniroute-upgrade\n').encode())
    guard.docker('one offline Linux verifier build', 'build', '--network=none', '--platform', 'linux/amd64',
        '--file', str(recipe), '--iidfile', str(guard.work/'verifier.id'), str(source))
    image = regular(guard.work/'verifier.id', 128).decode().strip()
    need(re.fullmatch('sha256:[0-9a-f]{64}', image), 'verifier_image')
    guard.inspect_image('verifier-tool', image)
    cid = guard.create('verifier-output', image)
    target = assets/'uniroute-upgrade-linux-amd64'
    source_helper.copy_exact(guard, cid, 'out/uniroute-upgrade', target, 16*MIB)
    os.chmod(target, 0o644)  # non-root Go inspector must be able to read the bind mount
    with target.open('rb') as inp: header = inp.read(20)
    need(header[:6] == b'\x7fELF\x02\x01' and header[18:20] == b'\x3e\x00', 'verifier_ELF')
    info = guard.tool('verifier-buildinfo', go,
        'mkdir -p /tmp/telemetry; printf "off\\n" > /tmp/telemetry/mode; go version -m /inspect/uniroute', target)
    for item in ('go1.25.12', 'CGO_ENABLED=0', 'GOOS=linux', 'GOARCH=amd64'):
        need(item in info, 'verifier_buildinfo')
    write_new(data/'verifier-buildinfo.txt', info.encode())
    write_new(data/'verifier-recipe.Dockerfile', regular(recipe, 65536))
    return file_fact(target)


def component_binding(notice_index, report, membership, buildinfo):
    """Join actual outputs to retained materials without promoting old observations."""
    rows = notice_index['components']; go_rows = []; go_bindings = []
    for line in buildinfo.splitlines():
        columns = line.strip().split('\t')
        if columns[0] in ('mod', 'dep', '=>'):
            go_rows.append(columns)
            matches = [r['id'] for r in rows if r.get('kind') == 'go-module'
                and len(columns) >= 3 and r.get('actual_build_module', {}).get('path') == columns[1]
                and r.get('actual_build_module', {}).get('version') == columns[2]
                and r.get('actual_build_module', {}).get('sum') == (columns[3] if len(columns) > 3 else None)]
            go_bindings.append({'actual_row': columns, 'retained_material_candidate_ids': matches,
                                'replacement_rows_preserved_not_assumed_equivalent': columns[0] == '=>'})
    npm = []
    for package in membership['packages']:
        component = (package['name']+'@'+package['version']) if package['name'] and package['version'] else None
        matches = [r['id'] for r in rows if r['component'] == component
                   and (r.get('source_observation') or {}).get('location') == package['location']]
        npm.append({'location': package['location'], 'name': package['name'], 'version': package['version'],
            'observed_notices': package['notices'], 'retained_material_candidate_ids': matches,
            'not_a_license_clearance': True})
    return {'source_commit': PRODUCT, 'source_tree': TREE, 'actual_go_module_rows': go_rows,
        'actual_go_notice_bindings': go_bindings,
        'actual_frontend_packages': npm,
        'actual_final_APK': {role: value['apk'] for role, value in report.items()},
        'notice_input_sha256': BOUND['component-notices.tar.gz'],
        'open_obligations': ['actual final OS component license/source disposition',
            'Go module replacements and corresponding source fulfillment review',
            'virtual/CSS frontend disposition remains subject to accepted observer findings',
            'four referenced upstream dependency images are not rebuilt or rehosted',
            'no signed bundle, formal registry reference, installer or production acceptance']}


def metadata_archive(data, output, guard=lambda: None):
    facts = {}; total = 0
    for path in sorted(data.rglob('*')):
        need(not path.is_symlink(), 'metadata_link')
        if path.is_dir(): continue
        name = safe_name(path.relative_to(data).as_posix()); fact = file_fact(path)
        total += fact['bytes']; need(total <= 28*MIB and len(facts) < 8192, 'metadata_limit')
        facts[name] = fact
    write_new(data/'closed-file-manifest.json', encoded({'files': facts, 'bytes': total,
        'scope': 'metadata and notice texts only; excludes manifest itself and raw command logs'}))
    fd = os.open(output, os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as out, gzip.GzipFile(filename='', mode='wb', fileobj=out, mtime=0) as zipped:
        with tarfile.open(fileobj=zipped, mode='w|', format=tarfile.USTAR_FORMAT) as archive:
            for path in sorted(data.rglob('*')):
                guard()
                if path.is_dir(): continue
                name = safe_name(path.relative_to(data).as_posix()); info = tarfile.TarInfo(name)
                info.size = path.stat().st_size; info.mode = 0o600
                with path.open('rb') as inp: archive.addfile(info, inp)
                need(out.tell() <= 30*MIB, 'metadata_archive_limit')
    need(output.stat().st_size <= 30*MIB < METADATA_LIMIT, 'metadata_archive_limit')
    return file_fact(output)


def build(guard, helper, ring, index, assets, data, retain):
    source = guard.source_export()
    need(guard.FIXED['source']['commit'] == PRODUCT and guard.FIXED['source']['tree'] == TREE, 'product_identity')
    back, front, derivation = derive_recipes(guard, source)
    write_new(data/'recipe-derivation.json', encoded(derivation))
    for role, path in (('backend', back), ('frontend', front)):
        write_new(data/(role+'.Dockerfile'), regular(path, 65536))
    for name in ('LICENSE', 'NOTICE'):
        write_new(data/('product-'+name+'.txt'), regular(source/name, MIB))
    write_new(data/'component-notices.tar.gz', regular(INPUTS/'component-notices.tar.gz', MIB))
    write_new(data/'public-keys.json', regular(INPUTS/'community-public-keys.json', 1024))
    write_new(data/'source-and-fixed-inputs.json', encoded({
        'source_export': guard.receipt['source_export'], 'fixed_inputs': guard.FIXED,
        'producer_commit': os.environ['EXPECTED_WORKFLOW_COMMIT'],
        'observer_bindings': BOUND, 'admission_sha256': os.environ['ADMISSION_SHA256'],
        'upstream_companions': 'references only; not pulled/rebuilt/rehosted by this workflow'}))
    bases = {role: guard.pull(role) for role in
             ('backend-builder', 'frontend-builder', 'backend-runtime', 'frontend-runtime')}
    go = bases['backend-builder']
    go_version = guard.tool('byte-go-version', go,
        'mkdir -p /tmp/telemetry; printf "off\\n" > /tmp/telemetry/mode; go version')
    node_version = guard.tool('byte-node-version', bases['frontend-builder'],
                              'node --version && npm --version').splitlines()
    need(go_version == 'go version go1.25.12 linux/amd64'
         and node_version == ['v24.19.0', '11.17.0'], 'actual_build_tools')
    images = {}; inventories = {}; membership = None; buildinfo = None
    # Exactly one backend and one complete production frontend build.
    for role, recipe, context in (('backend', back, source), ('frontend', front, source/'frontend')):
        iidfile = guard.work/(role+'.id')
        command = ['build', '--platform', 'linux/amd64', '--file', str(recipe),
                   '--tag', 'ur-byte-'+role+':'+guard.run_id, '--iidfile', str(iidfile)]
        args = (['RUNTIME_VERSION=1.12.0', 'RUNTIME_COMMIT='+PRODUCT, 'RUNTIME_TREE='+TREE,
                 'UNIROUTER_RELEASE_TRUST_MODE=release', 'UNIROUTER_UPGRADE_TRUST_BUNDLE_B64='+ring,
                 'GOPROXY=https://proxy.golang.org,direct'] if role == 'backend' else
                ['VITE_DEPLOY_ENV=production', 'VITE_APP_VERSION=1.12.0'])
        for argument in args: command += ['--build-arg', argument]
        guard.docker('one retained '+role+' build', *command, str(context))
        image = regular(iidfile, 128).decode().strip()
        need(re.fullmatch('sha256:[0-9a-f]{64}', image), 'app_image_id')
        guard.inspect_image(role, image)
        layers = guard.FIXED['images'][role+'-runtime']['rootfs_diff_ids']
        need(guard.receipt['images'][role]['rootfs_diff_ids'][:len(layers)] == layers, 'app_base_layers')
        cid, binary, report = inventory(guard, helper, role, image, data)
        inventories[role] = report
        # Check the notice archive actually included in each final image, not just build input.
        actual = guard.work/(role+'-included-notices.tar.gz')
        helper.copy_exact(guard, cid, 'usr/share/licenses/unirouter/component-notices.tar.gz', actual, MIB)
        need(file_fact(actual) == file_fact(INPUTS/'component-notices.tar.gz'), 'image_notice_binding')
        if role == 'backend':
            need(binary is not None and binary.is_file(), 'actual_backend_missing')
            buildinfo = guard.tool('actual-backend-buildinfo', go,
                'mkdir -p /tmp/telemetry; printf "off\\n" > /tmp/telemetry/mode; go version -m /inspect/uniroute', binary)
            for value in ('go1.25.12', 'CGO_ENABLED=0', 'GOOS=linux', 'GOARCH=amd64',
                          'main.buildVersion=1.12.0', 'main.buildCommit='+PRODUCT, 'main.buildTree='+TREE,
                          'github.com/uniroute/unirouter/internal/releasetrust.buildMode=release',
                          'github.com/uniroute/unirouter/internal/releasetrust.embeddedUpgradeKeyRingBase64='+ring):
                need(value in buildinfo, 'backend_release_buildinfo')
            write_new(data/'backend-buildinfo.txt', buildinfo.encode())
            write_new(data/'backend-binary.json', encoded(file_fact(binary)))
        else:
            destination = data/'frontend'; destination.mkdir(mode=0o700)
            guard.stream_command('actual final frontend membership', ['docker', '--config', str(guard.work/'docker-config'),
                '--host', 'unix:///var/run/docker.sock', 'cp', cid+':/usr/share/licenses/unirouter/output', '-'],
                lambda stream: helper.receive_frontend(stream, destination, guard.time_guard))
            files = {'frontend/'+p.relative_to(destination).as_posix(): regular(p, 16*MIB)
                     for p in destination.rglob('*') if p.is_file()}
            helper.validate_evidence(files)
            membership = load(files['frontend/frontend-membership.json'])
            dist = {r['path'].removeprefix('usr/share/nginx/html/'): r for r in report['entries']
                    if r['type'] == 'regular' and r['path'].startswith('usr/share/nginx/html/')}
            for row in membership['output_files']:
                actual_output = dist.get(row['file'])
                need(actual_output is not None and (actual_output['bytes'], actual_output['sha256'])
                     == (row['bytes'], row['sha256']), 'final_dist_membership')
            # Extra nginx stock files are recorded; they are not attributed to npm output.
        remaining = ASSET_LIMIT - sum(p.stat().st_size for p in assets.iterdir()) - 32*MIB
        images[role] = saved_image(guard, role, image, assets, remaining)
        write_new(data/(role+'-native-save.json'), encoded(images[role]))
        retain(assets/(role+'-image.tar.gz'))
    verifier(guard, helper, source, go, assets, data)
    retain(assets/'uniroute-upgrade-linux-amd64')
    write_new(data/'actual-component-bindings.json', encoded(component_binding(index, inventories, membership, buildinfo)))
    write_new(data/'actual-image-inspection.json', encoded(guard.receipt['images']))
    # Command argv/log contents are intentionally excluded; identity/status/exit facts suffice here.
    write_new(data/'command-outcomes.json', encoded([
        {k: row.get(k) for k in ('phase', 'status', 'exit_code', 'log_bytes', 'log_sha256')}
        for row in guard.receipt['commands']]))


def terminal_path(env):
    run = env.get('GITHUB_RUN_ID', '')
    need(re.fullmatch('[0-9]{1,24}', run), 'run_id')
    return Path(env['RUNNER_TEMP'])/('ur-byte-terminal-'+run+'-attempt1.json')


def safe_terminal(result):
    """Closed public summary: no paths, HTTP bodies, signed URLs, argv or log excerpts."""
    need(result['status'] in ('retained_unsigned_candidate_pending_clearance', 'stopped_partial_candidate'), 'summary_status')
    need(re.fullmatch('[0-9]{1,24}', result['run_id']), 'summary_run')
    need(result['producer'] is None or re.fullmatch('[0-9a-f]{40}', result['producer']), 'summary_producer')
    need(result['error'] is None or re.fullmatch('[a-zA-Z0-9_]{1,80}', result['error']), 'summary_error')
    assets = []
    for row in result['assets']:
        need(row['name'] in ASSETS+(MARKER,) and type(row['id']) is int
             and type(row['bytes']) is int and 0 <= row['bytes'] <= ASSET_LIMIT
             and re.fullmatch('[0-9a-f]{64}', row['sha256']), 'summary_asset')
        need(type(row['readback_matched']) is bool, 'summary_asset_verification')
        assets.append({k: row[k] for k in ('name', 'id', 'bytes', 'sha256', 'readback_matched')})
    value = {'kind': 'unsigned_byte_candidate_terminal', 'status': result['status'],
        'run_id': result['run_id'], 'producer': result['producer'], 'product_source': PRODUCT,
        'error': result['error'], 'draft_id': result['draft_id'], 'assets': assets,
        'owned_cleanup_complete': result['owned_cleanup_complete'],
        'not_signed_published_installed_or_license_cleared': True}
    remote = result.get('remote_outcome', {})
    need(type(remote.get('writes_blocked', False)) is bool, 'summary_remote')
    ids = remote.get('matching_draft_ids', [])
    need(type(ids) is list and len(ids) <= 20 and all(type(i) is int for i in ids), 'summary_remote')
    tags = {}
    for phase in ('before', 'after'):
        row = remote.get('tag_observations', {}).get(phase, remote.get('tag_'+phase))
        if row is None: tags[phase] = None; continue
        need(type(row.get('present')) is bool, 'summary_tag')
        if row['present']:
            need(re.fullmatch('[0-9a-f]{40}', row['commit']), 'summary_tag')
        tags[phase] = {'present': row['present'], 'commit': row.get('commit')}
    value['remote_outcome'] = {'writes_blocked': remote.get('writes_blocked', False),
                               'matching_draft_ids': ids, 'tag_observations': tags}
    diagnostic = result.get('diagnostic')
    value['diagnostic'] = validate_diagnostic(diagnostic) if diagnostic is not None else None
    need(value['draft_id'] is None or type(value['draft_id']) is int, 'summary_draft')
    need(type(value['owned_cleanup_complete']) is bool, 'summary_cleanup')
    raw = encoded(value); need(len(raw) <= 64*1024, 'summary_limit')
    return raw


def summary(env):
    try:
        raw = safe_terminal(load(regular(terminal_path(env), 64*1024)))
    except Exception:
        raw = encoded({'kind': 'unsigned_byte_candidate_terminal', 'status': 'terminal_unavailable',
                       'not_evidence_of_absent_remote_effects': True})
    # This path has no token, HTTP client, Docker, subprocess or mutation recovery.
    print(raw.decode(), end='')
    destination = env.get('GITHUB_STEP_SUMMARY')
    if destination:
        with open(destination, 'a') as out:
            out.write('```json\n'+raw.decode()+'```\n')


def main():
    if sys.argv[1:] == ['--summary']:
        summary(os.environ); return 0
    need(not sys.argv[1:], 'argument_contract')
    env = dict(os.environ)
    result = {'status': 'stopped_partial_candidate', 'run_id': env.get('GITHUB_RUN_ID', ''),
              'producer': None, 'error': None, 'draft_id': None, 'assets': [], 'owned_cleanup_complete': False}
    guard = draft = work = None
    cleanup_attempted = False
    try:
        producer = runtime_identity(env, load(regular(Path(env['GITHUB_EVENT_PATH']), 2*MIB)))
        result['producer'] = producer
        ring, index = bound_inputs()
        base = Path(env['RUNNER_TEMP']).resolve(); need(base.is_dir(), 'runner_temp')
        work = base/('ur-byte-candidate-'+env['GITHUB_RUN_ID']+'-attempt1'); work.mkdir(mode=0o700)
        assets = work/'assets'; assets.mkdir(mode=0o700)
        data = work/'data'; data.mkdir(mode=0o700)
        guard = module('accepted_byte_guards', Path(__file__).with_name('community-image-candidate.py'))
        helper = module('accepted_source_observer', Path(__file__).with_name('community-source-evidence.py'))
        # Token is retained only by the REST client; never inherited by build/tool commands.
        token = env.pop('GITHUB_TOKEN', '')
        os.environ.pop('GITHUB_TOKEN', None)
        initialize_guard(guard, work, env)
        api = GitHub(token, guard=guard.time_guard); token = None
        draft = Draft(api, work, producer, env['GITHUB_RUN_ID'])
        draft.create()
        result['draft_id'] = draft.release['id']
        marker = work/MARKER; write_new(marker, b'UniRouter draft audience check; contains no build or credential data.\n')
        draft.upload(marker)
        # Prove the retention channel before spending the app build time.
        with contextlib.redirect_stdout(io.StringIO()):
            build(guard, helper, ring, index, assets, data, draft.upload)
        guard.receipt['status'] = 'built_candidate_with_unresolved_obligations'
        cleanup_attempted = True
        with contextlib.redirect_stdout(io.StringIO()): guard.cleanup()
        result['owned_cleanup_complete'] = all(r['removed_with_anonymous_volumes'] for r in guard.receipt['cleanup'])
        need(result['owned_cleanup_complete'], 'owned_cleanup_incomplete')
        write_new(data/'owned-cleanup.json', encoded(guard.receipt['cleanup']))
        write_new(data/'draft-pre-upload-observations.json', encoded({'effects': draft.effects,
            'api_observations': api.observations, 'tag': draft.tag,
            'limits_of_claim': 'draft access checked at observations; repository writers retain control'}))
        metadata_archive(data, assets/'candidate-evidence.tar.gz', guard.time_guard)
        checksums = ''.join(file_fact(assets/name)['sha256']+'  '+name+'\n' for name in ASSETS[:-1])
        write_new(assets/'SHA256SUMS', checksums.encode())
        need(sum(p.stat().st_size for p in assets.iterdir()) + marker.stat().st_size <= ASSET_LIMIT,
             'aggregate_asset_limit')
        for name in ASSETS:
            if name in draft.expected: continue
            draft.upload(assets/name)
        draft.fresh()
        need(set(draft.expected) == set(ASSETS+(MARKER,)), 'final_asset_set')
        result['status'] = 'retained_unsigned_candidate_pending_clearance'
    except Exception as error:
        result['error'] = str(error) if isinstance(error, Stop) else type(error).__name__
        result['diagnostic'] = (draft.last_diagnostic if draft is not None and draft.last_diagnostic
                                else failure_diagnostic(error, guard))
    finally:
        if guard is not None and work is not None and guard.work is not None:
            try:
                if not cleanup_attempted:
                    cleanup_attempted = True
                    with contextlib.redirect_stdout(io.StringIO()): guard.cleanup()
                result['owned_cleanup_complete'] = (len(guard.receipt['cleanup']) == len(guard.owned)
                    and all(r['removed_with_anonymous_volumes'] for r in guard.receipt['cleanup']))
            except Exception:
                result['owned_cleanup_complete'] = False
        if draft is not None:
            # A product/build failure can still retain bounded partial metadata in the
            # already proven channel. An uncertain/auth/audience failure forbids this.
            if (work is not None and draft.marker_verified and not draft.blocked
                    and 'candidate-evidence.tar.gz' not in draft.expected
                    and not (work/'assets/candidate-evidence.tar.gz').exists()):
                try:
                    write_new(work/'data/partial-terminal.json', encoded({
                        'status': result['status'], 'error': result['error'],
                        'diagnostic': result.get('diagnostic'),
                        'cleanup_complete': result['owned_cleanup_complete'],
                        'remote_effects': draft.effects,
                        'command_outcomes': [{k: row.get(k) for k in
                            ('phase', 'status', 'exit_code', 'log_bytes', 'log_sha256')}
                            for row in guard.receipt['commands']]}))
                    metadata_archive(work/'data', work/'assets/candidate-evidence.tar.gz', guard.time_guard)
                    draft.upload(work/'assets/candidate-evidence.tar.gz')
                except Exception:
                    # Preserve the original failure; never retry the failed retention.
                    pass
            result['draft_id'] = draft.release['id'] if draft.release else None
            result['assets'] = [{**row['fact'], 'name': name, 'id': row['id'], 'readback_matched': name in draft.verified}
                                for name, row in sorted(draft.expected.items())]
            result['remote_outcome'] = {'writes_blocked': draft.blocked,
                'matching_draft_ids': sorted({i for row in draft.effects for i in row.get('matching_draft_ids', [])
                                             if type(i) is int}),
                'tag_before': draft.tag_before, 'tag_after': draft.tag_after}
            if work is not None:
                try:
                    write_new(work/'remote-write-observations.json', encoded({'effects': draft.effects,
                        'api': draft.api.observations, 'writes_blocked': draft.blocked}))
                except Exception: pass
        if not result['owned_cleanup_complete']:
            result['status'] = 'stopped_partial_candidate'
        try:
            write_new(terminal_path(env), safe_terminal(result))
        except Exception:
            # Hard cancellation/early preflight can leave no complete terminal result.
            print('{"status":"terminal_unavailable","not_evidence_of_absent_remote_effects":true}')
    return 0 if result['status'] == 'retained_unsigned_candidate_pending_clearance' else 1


if __name__ == '__main__':
    sys.exit(main())
